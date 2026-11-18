"""Read package identity once from the CLI-validated YAML manifest."""
import json,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parent.parent
_manifest=json.loads(subprocess.check_output(['node','--input-type=module','-e',
 "import {readFileSync} from 'node:fs';import {parse} from 'yaml';console.log(JSON.stringify(parse(readFileSync('dsm-package.yaml','utf8'))));"],cwd=ROOT,text=True))
PACKAGE_ID=_manifest['package']
PACKAGE_VERSION=f"{_manifest['version']}-{_manifest['build']:04d}"
TAG='v'+PACKAGE_VERSION
