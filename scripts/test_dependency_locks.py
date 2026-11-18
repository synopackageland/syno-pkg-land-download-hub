"""Every fetched npm artifact must come from the default registry with an SRI pin."""
import base64
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parent.parent


class DependencyLocksTests(unittest.TestCase):
    def test_all_npm_artifacts_have_official_registry_and_integrity(self):
        for name in ('pnpm-lock.yaml', 'frontend/pnpm-lock.yaml'):
            # pnpm 11+ writes the pnpm toolchain lock as its own YAML document.
            for lock in yaml.safe_load_all((ROOT / name).read_text()):
                # No per-lockfile registry override: every package resolves from
                # registry.npmjs.org by name and version.
                self.assertNotIn('registry', lock.get('settings') or {})
                for key, package in lock['packages'].items():
                    with self.subTest(lock=name, package=key):
                        # Only integrity: no tarball URL, git or directory source.
                        self.assertEqual(set(package['resolution']), {'integrity'})
                        algorithm, encoded = package['resolution']['integrity'].split('-', 1)
                        self.assertEqual(algorithm, 'sha512')
                        self.assertEqual(len(base64.b64decode(encoded, validate=True)), 64)


if __name__ == '__main__':
    unittest.main()
