#!/usr/bin/env python3
"""Build an isolated copy of the actual App for destructive tool fixtures.

Only the copy's package identity and private engine/protocol ports differ.
Production handlers, frontend and real media-tool workers are unchanged.
Build the ordinary App first so its verified upstream payload/UI are available.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / '.cache' / 'owned-package'
PACKAGE = 'DownloadHubFixture'


def build():
    if OUT.exists():
        shutil.rmtree(OUT)
    OUT.mkdir(parents=True)
    for name in ['backend', 'ui', 'assets', 'i18n', 'generated']:
        shutil.copytree(ROOT / name, OUT / name)
    for name in ['operations.json', 'package.json', 'pnpm-lock.yaml', 'pnpm-workspace.yaml']:
        shutil.copy2(ROOT / name, OUT / name)
    shutil.copytree(ROOT / 'frontend', OUT / 'frontend', ignore=shutil.ignore_patterns('node_modules'))
    (OUT / 'frontend/node_modules').symlink_to(ROOT / 'frontend/node_modules', target_is_directory=True)
    fixture = ROOT / 'tests' / 'fixtures' / 'managed-extension'
    shutil.copy2(fixture / 'install.go', OUT / 'backend/cmd/app/fixture_extension.go')
    shutil.copy2(fixture / 'manifest.json', OUT / 'backend/cmd/app/fixture-extension.json')
    main = OUT / 'backend/cmd/app/main.go'
    main.write_text(main.read_text().replace(
        'routes := append(append(service.Routes(), files...), saves.Routes()...)',
        'routes := append(append(service.Routes(), files...), saves.Routes()...)\n\troutes = append(routes, fixtureExtensionRoutes(data)...)'))
    registry = json.loads((OUT / 'operations.json').read_text())
    registry['operations']['fixture.extension.install'] = {
        'summary': 'Seed the fixed inert extension in this test-owned package',
        'cli': ['fixture', 'extension', 'install'], 'effect': 'write', 'permission': 'administrators',
        'ai': 'hidden', 'paramsSchema': {'type': 'object', 'properties': {}, 'additionalProperties': False},
        'resultSchema': {'type': 'object', 'properties': {'id': {'type': 'string'}}, 'required': ['id'], 'additionalProperties': False},
        'flow': {'type': 'invoke', 'method': 'fixture.extension.install'}, 'examples': [{}],
    }
    (OUT / 'operations.json').write_text(json.dumps(registry, indent=2) + '\n')
    (OUT / 'node_modules').symlink_to(ROOT / 'node_modules', target_is_directory=True)
    # Use the installed manifest parser, preserving all capability/entry rules.
    source = """
import fs from 'node:fs'; import YAML from 'yaml';
const m=YAML.parse(fs.readFileSync('dsm-package.yaml','utf8'));
m.package='DownloadHubFixture';m.displayname='Download Hub test fixture';m.build=1;
m.arches=['x86_64'];m.daemon.targets={x86_64:m.daemon.targets.x86_64};delete m.portal;
m.ui.build={cwd:'frontend',command:['npm','run','build']};m.ctl.aliases=['download-hub-fixture'];
for(const locale of Object.values(m.i18n.info))locale.displayname='Download Hub test fixture';
fs.writeFileSync('.cache/owned-package/dsm-package.yaml',YAML.stringify(m));
"""
    subprocess.run(['node', '--input-type=module', '-e', source], cwd=ROOT, check=True)
    for relative in ['backend/cmd/app/main.go', 'backend/cmd/engine/main.go',
                     'backend/internal/gopeed/service.go', 'backend/internal/gopeed/bt_settings.go']:
        path = OUT / relative
        text = path.read_text().replace('"SynoPkgGopeed"', f'"{PACKAGE}"')
        for before, after in [('19893', '26943'), ('16889', '26944'), ('4662', '26945'), ('4672', '26946')]:
            text = text.replace(before, after)
        path.write_text(text)
    payload = OUT / 'build' / 'daemon-x86_64'
    shutil.copytree(ROOT / 'build' / 'daemon-x86_64', payload)
    env = dict(os.environ, CGO_ENABLED='0', GOOS='linux', GOARCH='amd64')
    subprocess.run(['go', 'build', '-trimpath', '-buildvcs=false', '-ldflags=-s -w',
                    '-o', str(payload / 'bin' / 'engine'), './cmd/engine'],
                   cwd=OUT / 'backend', env=env, check=True)
    cli = ROOT / 'node_modules' / '.bin' / 'synopkgland'
    for args in [['ctl', 'generate'], ['check', '--json'], ['build', '--json']]:
        subprocess.run([str(cli), *args], cwd=OUT, check=True)
    spk = next((OUT / 'dist').glob('*.spk'))
    subprocess.run([str(cli), 'inspect', str(spk), '--json'], cwd=OUT, check=True)
    print(json.dumps({'package': PACKAGE, 'spk': str(spk)}))


if __name__ == '__main__':
    if len(sys.argv) != 1:
        raise SystemExit('This fixture takes no identity, path or port overrides')
    build()
