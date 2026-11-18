#!/usr/bin/env python3
"""License notices for the Go modules compiled into the official Gopeed binaries, as `go version -m` lists them."""
import json,subprocess
from pathlib import Path
LICENSE_PREFIXES=('LICENSE','LICENCE','COPYING','COPYRIGHT','NOTICE','PATENTS','UNLICENSE')
HEADER='Go modules compiled into the official Gopeed engine binary (bin/gopeed), with their license files.\nThe list is read from the binary with `go version -m`; gopeed-go-modules.json holds the same list.\n'
def modules(text):
 # A `=>` line replaces the dep above it; a local replacement has no version and no license to fetch.
 found={}
 last=None
 for line in text.splitlines():
  f=line.strip().split('\t')
  if f[0]=='dep':last=f[1];found[last]=(f[1],f[2])
  elif f[0]=='=>':
   if len(f)<3 or not f[2]:raise RuntimeError('Local replacement without a version: '+f[1])
   found[last]=(f[1],f[2])
 return sorted(found.values())
def binary_modules(*binaries,env=None):
 return sorted({m for b in binaries for m in modules(subprocess.check_output(['go','version','-m',str(b)],env=env,text=True))})
def module_dir(path,version,env=None):
 result=subprocess.run(['go','mod','download','-json',f'{path}@{version}'],env=env,capture_output=True,text=True)
 folder=json.loads(result.stdout or '{}').get('Dir') if not result.returncode else None
 if not folder:raise RuntimeError(f'go mod download failed: {path}@{version}: '+(result.stdout+result.stderr).strip()[-500:])
 return Path(folder)
def notices(mods,locate):
 text=[HEADER];inventory=[]
 for path,version in mods:
  folder=locate(path,version)
  files=sorted(p for p in folder.iterdir() if p.is_file() and p.name.upper().startswith(LICENSE_PREFIXES))
  if not files:raise RuntimeError(f'No license file in {path}@{version}')
  inventory.append({'path':path,'version':version,'files':[p.name for p in files]})
  text.append(f'\n{"="*72}\n{path} {version}\n{"="*72}\n')
  for p in files:text.append(f'\n--- {p.name} ---\n\n'+p.read_text(errors='replace').rstrip('\n')+'\n')
 return ''.join(text),inventory
def write_go_notices(binaries,out,env=None):
 text,inventory=notices(binary_modules(*binaries,env=env),lambda path,version:module_dir(path,version,env))
 out.mkdir(parents=True,exist_ok=True)
 (out/'gopeed-go-modules.txt').write_text(text)
 (out/'gopeed-go-modules.json').write_text(json.dumps(inventory,indent=2)+'\n')
