#!/usr/bin/env python3
import subprocess
from package_meta import PACKAGE_ID, PACKAGE_VERSION
from pathlib import Path
from app_policy import verify_spk
ROOT=Path(__file__).resolve().parent.parent
cli=ROOT/'node_modules/.bin/synopkgland'
def run(args):subprocess.run(args,cwd=ROOT,check=True)
run([str(cli),'ctl','generate','--check','--json']);run([str(cli),'check','--json']);run([str(cli),'build','--json'])
for arch in ('x86_64','aarch64'):
 p=ROOT/'dist'/f'{PACKAGE_ID}-{PACKAGE_VERSION}-{arch}.spk'
 verify_spk(p, PACKAGE_ID)
 with (ROOT/'dist'/f'{arch}-inspection.json').open('w') as f:subprocess.run([str(cli),'inspect',str(p),'--json'],cwd=ROOT,stdout=f,check=True)
