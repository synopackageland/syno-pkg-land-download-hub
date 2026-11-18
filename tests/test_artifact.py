"""Lock the built SPKs' icon and translated DSM title; run after `pnpm run build`."""
import io
import json
import os
import platform
import re
import struct
import subprocess
import sys
import tarfile
import tempfile
import unittest
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT/'scripts'))
from app_policy import read_spk, unique_file  # noqa: E402
from go_notices import binary_modules  # noqa: E402
from package_meta import PACKAGE_ID, PACKAGE_VERSION  # noqa: E402

ICON = ROOT/'assets/icon'
ARCHES = ('x86_64', 'aarch64')


def pixels(data):
    """Decode a non-interlaced 8-bit PNG to (width, height, RGBA rows)."""
    assert data[:8] == b'\x89PNG\r\n\x1a\n'
    pos, chunks = 8, {}
    while pos < len(data):
        size, kind = struct.unpack('>I4s', data[pos:pos+8])
        chunks.setdefault(kind, []).append(data[pos+8:pos+8+size])
        pos += 12+size
    width, height, depth, colour, _, _, interlace = struct.unpack('>IIBBBBB', chunks[b'IHDR'][0])
    channels = {2: 3, 6: 4}[colour]
    assert depth == 8 and interlace == 0
    raw, stride, rows, prev = zlib.decompress(b''.join(chunks[b'IDAT'])), width*channels, [], bytearray(width*channels)
    for y in range(height):
        kind, line = raw[y*(stride+1)], bytearray(raw[y*(stride+1)+1:(y+1)*(stride+1)])
        for x in range(stride):
            a = line[x-channels] if x >= channels else 0
            b, c = prev[x], prev[x-channels] if x >= channels else 0
            p = a+b-c
            predictor = a if abs(p-a) <= abs(p-b) and abs(p-a) <= abs(p-c) else b if abs(p-b) <= abs(p-c) else c
            line[x] = (line[x]+[0, a, b, (a+b)//2, predictor][kind]) & 0xff
        rows.append(bytes(line) if channels == 4 else b''.join(bytes(line[i:i+3])+b'\xff' for i in range(0, stride, 3)))
        prev = line
    return width, height, rows


class ArtifactTests(unittest.TestCase):
    def spk(self, arch):
        return ROOT/'dist'/f'{PACKAGE_ID}-{PACKAGE_VERSION}-{arch}.spk'

    def payload(self, arch):
        with tarfile.open(fileobj=io.BytesIO(read_spk(self.spk(arch))[0]), mode='r:*') as inner:
            return {m.name: inner.extractfile(m).read() for m in inner.getmembers() if m.isfile()}

    def app_root(self, payload):
        entries = [name for name in payload if re.fullmatch(r'ui/app/[a-f0-9]{64}/app\.module\.js', name)]
        self.assertEqual(len(entries), 1)
        return entries[0].removesuffix('app.module.js')

    def test_es_module_and_lazy_pages_share_the_versioned_snapshot(self):
        for arch in ARCHES:
            with self.subTest(arch=arch):
                payload = self.payload(arch)
                root = self.app_root(payload)
                self.assertIn(root+'app.css', payload)
                for page in ('SearchPage', 'SourceSettings'):
                    # The SDK build adds a precompressed .js.gz next to each chunk; count the chunk once.
                    self.assertEqual(len([name for name in payload if name.startswith(root+'chunks/'+page+'-') and name.endswith('.js')]), 1)
                self.assertNotIn('ui/app/app.js', payload)
                window = next(data for name, data in payload.items() if re.fullmatch(r'ui/window-[^/]+\.js', name))
                self.assertIn(b'app.module.js', window)

    @unittest.skipUnless(platform.system() == 'Linux' and platform.machine() == 'x86_64',
                         'the packaged CGI requires Linux x86_64')
    def test_packaged_cgi_accepts_its_bound_policy_and_refuses_no_session(self):
        payload = self.payload('x86_64')
        with tempfile.TemporaryDirectory() as folder:
            # The shipped CGI intentionally refuses root. A root build host
            # must exercise this request as an unprivileged account too.
            identity = {}
            if os.geteuid() == 0:
                import pwd
                user = pwd.getpwnam('nobody')
                Path(folder).chmod(0o755)
                identity = dict(user=user.pw_uid, group=user.pw_gid, extra_groups=[])
            binary = Path(folder)/'api.cgi'
            binary.write_bytes(payload['ui/cgi/api.cgi'])
            binary.chmod(0o755)
            body = b'{"method":"gopeed.session","params":{}}'
            result = subprocess.run([str(binary)], input=body, capture_output=True, cwd=folder, **identity, env=dict(
                os.environ, GATEWAY_INTERFACE='CGI/1.1', REQUEST_METHOD='POST',
                CONTENT_TYPE='application/json', CONTENT_LENGTH=str(len(body)),
                HTTP_SEC_FETCH_SITE='same-origin', HTTP_X_SYNO_TOKEN='fixture-token',
                REMOTE_ADDR='192.0.2.1', SERVER_NAME='app.example', SERVER_PORT='80',
                SERVER_PROTOCOL='HTTP/1.1', SCRIPT_NAME='/cgi/api.cgi', REQUEST_URI='/cgi/api.cgi',
            ))
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            response = json.loads(result.stdout.split(b'\r\n\r\n', 1)[1])
            self.assertFalse(response['success'])
            self.assertEqual(response['error']['code'], 'session_required')

    def assertSamePixels(self, actual, size):
        # The CLI re-encodes PNGs, so compare decoded pixels, not bytes.
        self.assertEqual(pixels(actual), pixels((ICON/f'icon_{size}.png').read_bytes()))

    def test_package_center_icons_are_the_gopeed_logo(self):
        for arch in ARCHES:
            with self.subTest(arch=arch), tarfile.open(self.spk(arch)) as outer:
                self.assertSamePixels(unique_file(outer, 'PACKAGE_ICON.PNG'), 64)
                self.assertSamePixels(unique_file(outer, 'PACKAGE_ICON_256.PNG'), 256)

    def test_dsm_window_waits_for_the_shared_request_deadline(self):
        # The shared SDK request budget reaches the window's ctx.requestTimeout.
        for arch in ARCHES:
            with self.subTest(arch=arch):
                windows = [data for name, data in self.payload(arch).items() if re.fullmatch(r'ui/window-[^/]+\.js', name)]
                self.assertEqual(len(windows), 1)
                # Public JS is minified; keep checking the 300-second value.
                self.assertRegex(windows[0], rb'\brequestTimeout\s*:\s*(?:300000|3e5)\b')

    def test_dsm_window_offers_the_application_portal_through_the_executor(self):
        # Below DSM 7.2 the executor registers the entry; the window gives ctx.portal.
        for arch in ARCHES:
            with self.subTest(arch=arch):
                payload = self.payload(arch)
                windows = [data for name, data in payload.items() if re.fullmatch(r'ui/window-[^/]+\.js', name)]
                self.assertRegex(
                    windows[0],
                    rb'\bdefaultAlias\s*:\s*"download-hub"\s*,\s*entry\s*:\s*(?:true|!0)\b',
                )
                self.assertNotIn('ui/portal.html', payload)

    def test_mobile_entries_attach_executor_approval_with_shipped_tasks_and_pins(self):
        for arch in ARCHES:
            with self.subTest(arch=arch):
                payload = self.payload(arch)
                executor = json.loads((ROOT/'dist'/f'{arch}-inspection.json').read_text())['data']['executor']
                for name in ('ui/app/entry.js', 'ui/app/entry-context.js'):
                    script = payload[name]
                    self.assertRegex(script, rb'\.executor\s*=\s*[\w$]+\.createExecutorAccess\(')
                    self.assertRegex(script, rb'\bconfirmPassword\s*:\s*[\w$]+\.createPasswordConfirm\(')
                    for key in ('executorDigest', 'policyDigest'):
                        self.assertIn(executor[key].encode(), script)
                    for task in executor['tasks']:
                        self.assertIn(task['name'].encode(), script)

    def test_dsm_window_icons_are_the_gopeed_logo(self):
        for arch in ARCHES:
            with self.subTest(arch=arch):
                payload = self.payload(arch)
                # The CLI names the icons after their content; ui/config says which.
                icons = {entry['icon'] for script in json.loads(payload['ui/config']).values() for entry in script.values()}
                self.assertEqual(len(icons), 1, icons)
                pattern = re.escape('ui/'+icons.pop()).replace(re.escape('{0}'), r'(\d+)')
                images = {int(m.group(1)): data for name, data in payload.items() if (m := re.fullmatch(pattern, name))}
                self.assertTrue(images)
                for size, data in images.items():
                    self.assertSamePixels(data, size)

    def test_inspect_reports_the_custom_icon(self):
        for arch in ARCHES:
            with self.subTest(arch=arch):
                inspection = json.loads((ROOT/'dist'/f'{arch}-inspection.json').read_text())
                self.assertEqual(inspection['data']['icon']['source'], 'custom')

    def test_inspect_finds_no_dsm_format_errors(self):
        for arch in ARCHES:
            with self.subTest(arch=arch):
                inspection = json.loads((ROOT/'dist'/f'{arch}-inspection.json').read_text())
                self.assertTrue(inspection['data']['synopkgland'])
                self.assertEqual(inspection['data']['dsm']['errors'], [])

    def test_localized_entries_load_their_texts(self):
        for arch in ARCHES:
            with self.subTest(arch=arch):
                config = json.loads(self.payload(arch)['ui/config'])
                localized = [entry for script in config.values() for entry in script.values()
                             if any(str(entry.get(key, '')).startswith('synopkgland:') for key in ('title', 'desc'))]
                self.assertTrue(localized)
                for entry in localized:
                    self.assertEqual(entry.get('texts'), 'texts')

    def test_traditional_chinese_title_and_description(self):
        for arch in ARCHES:
            with self.subTest(arch=arch):
                payload = self.payload(arch)
                cht, enu = payload['ui/texts/cht/strings'].decode(), payload['ui/texts/enu/strings'].decode()
                self.assertIn('app_title="下載中心"', cht)
                self.assertIn('Powered by Gopeed"', cht)
                self.assertIn('app_title="Download Hub"', enu)
                self.assertIn('Powered by Gopeed"', enu)


    def test_ui_ships_gopeed_and_frontend_licenses_beside_the_notices(self):
        # ui/licenses once collided with ui/LICENSES on case-insensitive disks.
        for arch in ARCHES:
            with self.subTest(arch=arch):
                payload = self.payload(arch)
                root = self.app_root(payload)
                for name in ('Gopeed-GPL-3.0.txt', 'ui-MIT.txt', 'vue-MIT.txt', 'sdk-MIT.txt', 'THIRD_PARTY_NOTICES.txt'):
                    self.assertIn(root+'LICENSES/'+name, set(payload))
                self.assertIn(b'GNU GENERAL PUBLIC LICENSE', payload[root+'LICENSES/Gopeed-GPL-3.0.txt'])
                self.assertEqual([n for n in payload if n.startswith(root+'licenses/')], [])
    def test_go_module_notices_list_exactly_the_modules_of_the_shipped_gopeed_binary(self):
        for arch in ARCHES:
            with self.subTest(arch=arch), tempfile.TemporaryDirectory() as folder:
                payload = self.payload(arch)
                root = self.app_root(payload)
                gopeed = Path(folder)/'gopeed'; gopeed.write_bytes(payload['daemon/payload/bin/gopeed'])
                inventory = json.loads(payload[root+'LICENSES/gopeed-go-modules.json'])
                self.assertEqual([(m['path'], m['version']) for m in inventory], binary_modules(gopeed))
                text = payload[root+'LICENSES/gopeed-go-modules.txt'].decode()
                for m in inventory:
                    self.assertIn(f"{m['path']} {m['version']}\n", text)
                self.assertIn('github.com/monkeyWie/goed2k', {m['path'] for m in inventory})

if __name__ == '__main__':
    unittest.main()
