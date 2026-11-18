"""Exercise public CLI schema and packaging policy on the application manifest."""
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parent.parent
LOCALES = ('en', 'zh-TW', 'zh-CN', 'ja', 'ko', 'de', 'fr', 'es', 'it')


def project_lock(path):
    """pnpm 11+ writes the pnpm toolchain lock as a first YAML document; the project lock is the last."""
    return list(yaml.safe_load_all(path.read_text()))[-1]


class SDKMigrationTests(unittest.TestCase):
    def test_acceptance_artifact_identity(self):
        from package_meta import PACKAGE_ID, PACKAGE_VERSION, TAG
        self.assertEqual(PACKAGE_ID, 'SynoPkgGopeed')
        self.assertRegex(PACKAGE_VERSION, r'^\d+\.\d+\.\d+-\d{4}$')
        self.assertEqual(TAG, 'v' + PACKAGE_VERSION)

    def test_sdk_comes_from_the_public_registries(self):
        # Published packages retain exact lockfile pins; CLI and UI follow patch releases.
        for directory, package, version in [(ROOT, '@synopackageland/sdk', '0.53.49'), (ROOT, '@synopackageland/app', '0.53.49'), (ROOT, '@synopackageland/cli', '0.54.6'), (ROOT, '@synopackageland/templates', '0.42.1'), (ROOT/'frontend', '@synopackageland/sdk', '0.53.49'), (ROOT/'frontend', '@synopackageland/ui', '0.25.29')]:
            manifest = json.loads((directory/'package.json').read_text())
            lock = project_lock(directory/'pnpm-lock.yaml')
            declared = dict(manifest.get('dependencies', {}), **manifest.get('devDependencies', {}))
            self.assertEqual(declared[package], '~'+version if package in ('@synopackageland/cli', '@synopackageland/ui') else version)
            importer = lock['importers']['.']
            entry = dict(importer.get('dependencies', {}), **importer.get('devDependencies', {}))[package]
            self.assertEqual(entry['version'].split('(')[0], version)
            # pnpm resolves registry packages by name@version from the default
            # registry, pinned by SRI integrity (no tarball URL override).
            resolution = lock['packages'][f'{package}@{version}']['resolution']
            self.assertEqual(set(resolution), {'integrity'})
        gomod = (ROOT/'backend/go.mod').read_text()
        self.assertIn('github.com/synopackageland/syno-pkg-land-sdk v0.53.49', gomod)
        # The released SDK module comes straight from the public module proxy,
        # pinned by go.sum: no replacement, local checkout or source patch.
        self.assertEqual([line for line in gomod.splitlines() if line.startswith('replace ')], [])
        self.assertNotIn('sdkSource', json.loads((ROOT/'toolchain.json').read_text()))
        self.assertRegex((ROOT/'backend/go.sum').read_text(), r'github.com/synopackageland/syno-pkg-land-sdk v0\.53\.49 h1:[A-Za-z0-9+/]{43}=')
        self.assertFalse((ROOT/'frontend/src/sdk').exists())
        self.assertFalse((ROOT/'docs/delegate-sdk-snapshot.json').exists())

    def test_notices_come_from_the_templates_package(self):
        self.assertFalse((ROOT/'frontend/third-party-notices.mjs').exists())
        prepare = (ROOT/'scripts/prepare.py').read_text()
        self.assertIn("from '@synopackageland/templates/notices'", prepare)

    def test_schema_two_preserves_daemon_and_install_selected_delegate(self):
        # Store text comes only from the catalog, which lives outside this
        # repository; check runs against a placeholder entry for this version.
        from package_meta import PACKAGE_ID, PACKAGE_VERSION
        text = lambda value: {locale: value for locale in LOCALES}
        catalog = {'schema': 4, 'packages': [{'pins': [{'package': PACKAGE_ID}], 'metadata': {
            'description': text('Test description'),
            'changelog': {'releases': {PACKAGE_VERSION: text(['Test note'])}},
        }}]}
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)/'catalog.yaml'
            path.write_text(yaml.safe_dump(catalog, allow_unicode=True))
            env = dict(os.environ, SYNOPKGLAND_CATALOG=str(path))
            result = subprocess.run([str(ROOT/'node_modules/.bin/synopkgland'), 'check', '--json'], cwd=ROOT, capture_output=True, text=True, env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(json.loads(result.stdout)['ok'])
        manifest = subprocess.check_output(['node', '--input-type=module', '-e', "import{readFileSync}from'node:fs';import{parse}from'yaml';console.log(JSON.stringify(parse(readFileSync('dsm-package.yaml','utf8'))))"], cwd=ROOT, text=True)
        data = json.loads(manifest)
        self.assertEqual(data['schema'], 2)
        self.assertEqual(data['capabilities'], {'files':'session','delegate':{'scope':'install-selected','operations':['files.moveFromStaging'],'destinations':['save'],'lifetime':'until-revoked'}})
        self.assertEqual(data['backend']['kind'], 'go')
        self.assertEqual(data['daemon']['exec'], 'bin/engine')
        self.assertEqual(set(data['daemon']['targets']), {'x86_64', 'aarch64'})
