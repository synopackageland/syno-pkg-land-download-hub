import hashlib
import io
import json
import tarfile
import tempfile
import unittest
from pathlib import Path
from app_policy import verify_spk, unique_file


def archive(files):
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode='w') as tar:
        for name, data in files.items():
            member = tarfile.TarInfo(name)
            member.mode = 0o755 if name.endswith('.cgi') else 0o644
            member.size = len(data)
            tar.addfile(member, io.BytesIO(data))
    return raw.getvalue()


class PolicyTests(unittest.TestCase):
    def fixture(self, config, checksum=None, anchor='valid'):
        executor = b'unchanged executor'
        payload = archive({'ui/config': json.dumps(config).encode(), 'ui/cgi/api.cgi': b'unchanged executable', 'executor/executor': executor})
        info = 'checksum="' + (checksum or hashlib.md5(payload).hexdigest()) + '"\n'
        if anchor is not None:
            digest = hashlib.sha256(executor).hexdigest() if anchor in ('valid', 'duplicate') else anchor
            info += f'synopkgland_executor_sha256="{digest}"\n'
            if anchor == 'duplicate':
                info += f'synopkgland_executor_sha256="{digest}"\n'
        info = info.encode()
        return archive({'package.tgz': payload, 'INFO': info, 'scripts/postinst': b'unchanged lifecycle'})

    def test_launcher_policy_checksum_and_other_payloads(self):
        config = {'window.js': {'app': {'type': 'app', 'allUsers': True, 'grantPrivilege':'all', 'advanceGrantPrivilege':True, 'appWindow': 'SYNO.SDS.SynoPkgGopeed.MainWindow'}, 'lib': {'type': 'lib'}}}
        with tempfile.TemporaryDirectory() as folder:
            p = Path(folder)/'test.spk'; original=self.fixture(config); p.write_bytes(original)
            verify_spk(p, 'SynoPkgGopeed')
            self.assertEqual(p.read_bytes(),original)
            with tarfile.open(p) as outer:
                payload = unique_file(outer, 'package.tgz')
                self.assertIn(hashlib.md5(payload).hexdigest().encode(), unique_file(outer, 'INFO'))
                self.assertEqual(unique_file(outer, 'scripts/postinst'), b'unchanged lifecycle')
            with tarfile.open(fileobj=io.BytesIO(payload)) as inner:
                self.assertIs(json.loads(unique_file(inner, 'ui/config'))['window.js']['app']['allUsers'], True)
                self.assertEqual(unique_file(inner, 'ui/cgi/api.cgi'), b'unchanged executable')
                self.assertEqual(inner.getmember('ui/cgi/api.cgi').mode, 0o755)

    def test_registry_drift_and_bad_checksum_fail_without_replacing_spk(self):
        for raw in (self.fixture({}), self.fixture({}, '0'*32)):
            with self.subTest(), tempfile.TemporaryDirectory() as folder:
                p = Path(folder)/'test.spk'; p.write_bytes(raw)
                with self.assertRaises(ValueError): verify_spk(p, 'SynoPkgGopeed')
                self.assertEqual(p.read_bytes(), raw)

    def test_unrestricted_launcher_is_refused(self):
        config = {'window.js': {'app': {'type':'app','allUsers':True,'appWindow':'SYNO.SDS.SynoPkgGopeed.MainWindow'}}}
        with tempfile.TemporaryDirectory() as folder:
            p=Path(folder)/'test.spk';p.write_bytes(self.fixture(config))
            with self.assertRaises(ValueError):verify_spk(p,'SynoPkgGopeed')

    def test_missing_or_mismatched_executor_info_anchor_is_refused(self):
        config = {'window.js': {'app': {'type': 'app', 'allUsers': True, 'grantPrivilege':'all', 'advanceGrantPrivilege':True, 'appWindow': 'SYNO.SDS.SynoPkgGopeed.MainWindow'}}}
        for anchor in (None, '0'*64, 'duplicate'):
            with self.subTest(anchor=anchor), tempfile.TemporaryDirectory() as folder:
                p = Path(folder)/'test.spk'; original=self.fixture(config, anchor=anchor); p.write_bytes(original)
                with self.assertRaisesRegex(ValueError, 'executor.*anchor'):
                    verify_spk(p, 'SynoPkgGopeed')
                self.assertEqual(p.read_bytes(), original)
