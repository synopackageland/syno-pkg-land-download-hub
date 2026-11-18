"""The SPK must carry the license of every Go module compiled into the official Gopeed binaries."""
import json
import os
import subprocess
import tempfile
import unittest
import zipfile
from pathlib import Path
from go_notices import binary_modules, modules, notices, write_go_notices

BUILD_INFO = '''gopeed: go1.24.13
\tpath\texample.com/app
\tmod\texample.com/app\tv0.0.0+dirty\t
\tdep\texample.com/b\tv1.2.0\th1:x=
\tdep\texample.com/a\tv1.0.0\th1:y=
\tdep\texample.com/old\tv0.1.0\th1:z=
\t=>\texample.com/new\tv0.2.0\th1:w=
\tbuild\tCGO_ENABLED=0
'''


def proxy(root):
    # A file:// module proxy so `go build` and `go mod download` work offline.
    folder = root/'proxy/example.com/dep/@v'; folder.mkdir(parents=True)
    mod = 'module example.com/dep\n\ngo 1.22\n'
    (folder/'list').write_text('v1.0.0\n')
    (folder/'v1.0.0.info').write_text('{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}')
    (folder/'v1.0.0.mod').write_text(mod)
    with zipfile.ZipFile(folder/'v1.0.0.zip', 'w') as z:
        z.writestr('example.com/dep@v1.0.0/go.mod', mod)
        z.writestr('example.com/dep@v1.0.0/dep.go', 'package dep\n\nfunc Name() string { return "dep" }\n')
        z.writestr('example.com/dep@v1.0.0/LICENSE', 'Dep License\n')
    return dict(os.environ, GOPROXY=(root/'proxy').as_uri(), GOSUMDB='off', GOFLAGS='-mod=mod -modcacherw',
                GOMODCACHE=str(root/'modcache'), GOTOOLCHAIN='local', CGO_ENABLED='0')


def binary(root, env):
    (root/'app').mkdir()
    (root/'app/go.mod').write_text('module example.com/app\n\ngo 1.22\n\nrequire example.com/dep v1.0.0\n')
    (root/'app/main.go').write_text('package main\n\nimport "example.com/dep"\n\nfunc main() { println(dep.Name()) }\n')
    subprocess.run(['go', 'build', '-o', str(root/'app/app'), '.'], cwd=root/'app', env=env, check=True, capture_output=True)
    return root/'app/app'


class GoNoticesTests(unittest.TestCase):
    def test_modules_are_the_sorted_deps_with_replacements_applied(self):
        self.assertEqual(modules(BUILD_INFO), [('example.com/a', 'v1.0.0'), ('example.com/b', 'v1.2.0'), ('example.com/new', 'v0.2.0')])

    def test_local_replacement_without_version_is_refused(self):
        with self.assertRaises(RuntimeError):
            modules('\tdep\texample.com/a\tv1.0.0\th1:y=\n\t=>\t../a\t\n')

    def test_notices_hold_every_license_file_of_every_module(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            for name, files in {'a': {'LICENSE': 'A MIT\n', 'go.mod': ''}, 'b': {'COPYING': 'B GPL\n', 'NOTICE.txt': 'B notice\n'}}.items():
                (root/name).mkdir()
                for file, text in files.items():
                    (root/name/file).write_text(text)
            text, inventory = notices([('example.com/a', 'v1.0.0'), ('example.com/b', 'v1.2.0')], lambda path, version: root/path.split('/')[-1])
            self.assertEqual(inventory, [{'path': 'example.com/a', 'version': 'v1.0.0', 'files': ['LICENSE']},
                                         {'path': 'example.com/b', 'version': 'v1.2.0', 'files': ['COPYING', 'NOTICE.txt']}])
            for expected in ('example.com/a v1.0.0', 'A MIT', 'example.com/b v1.2.0', 'B GPL', 'B notice'):
                self.assertIn(expected, text)
            self.assertNotIn('go.mod', text)

    def test_module_without_license_is_refused(self):
        with tempfile.TemporaryDirectory() as folder:
            with self.assertRaises(RuntimeError):
                notices([('example.com/a', 'v1.0.0')], lambda path, version: Path(folder))

    def test_written_inventory_matches_the_binary(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder); env = proxy(root); app = binary(root, env)
            self.assertEqual(binary_modules(app, env=env), [('example.com/dep', 'v1.0.0')])
            write_go_notices([app, app], root/'out', env=env)
            inventory = json.loads((root/'out/gopeed-go-modules.json').read_text())
            self.assertEqual(inventory, [{'path': 'example.com/dep', 'version': 'v1.0.0', 'files': ['LICENSE']}])
            self.assertIn('Dep License', (root/'out/gopeed-go-modules.txt').read_text())


if __name__ == '__main__':
    unittest.main()
