#!/usr/bin/env python3
"""Download verified official binaries; build the independent Syno Package UI."""
import hashlib,json,os,shutil,subprocess,tarfile,urllib.request,zipfile
from pathlib import Path
from go_notices import write_go_notices
from search_notices import write_search_notices
ROOT=Path(__file__).resolve().parent.parent
LOCK=json.loads((ROOT/'third-party/gopeed/lock.json').read_text())
CACHE=ROOT/'.cache';SOURCE=ROOT/'third-party/gopeed/source'
def run(args,cwd=ROOT,env=None):subprocess.run(args,cwd=cwd,env=env,check=True)
def fetch(key,item,suffix):
 p=CACHE/('gopeed-'+key+suffix);CACHE.mkdir(exist_ok=True)
 if not p.exists():
  tmp=p.with_suffix('.partial')
  with urllib.request.urlopen(item['url'],timeout=30) as src,tmp.open('wb') as dst:
   total=0
   while block:=src.read(1024*1024):
    total+=len(block)
    if total>256*1024*1024:raise RuntimeError('Upstream archive limit')
    dst.write(block)
  tmp.replace(p)
 if hashlib.sha256(p.read_bytes()).hexdigest()!=item['sha256']:raise RuntimeError('Upstream checksum mismatch: '+key)
 return p

def prepare():
 archive=fetch('source',LOCK['source'],'.tar.gz')
 if SOURCE.exists():shutil.rmtree(SOURCE)
 tmp=CACHE/'source-extract'
 if tmp.exists():shutil.rmtree(tmp)
 tmp.mkdir()
 with tarfile.open(archive) as t:t.extractall(tmp,filter='data')
 roots=list(tmp.iterdir())
 if len(roots)!=1 or not roots[0].is_dir():raise RuntimeError('Invalid source root')
 shutil.move(roots[0],SOURCE)
 for arch,item in LOCK['assets'].items():
  archive=fetch(arch,item,'.zip');bundle=ROOT/'build'/('daemon-'+arch)
  if bundle.exists():shutil.rmtree(bundle)
  (bundle/'bin').mkdir(parents=True)
  with zipfile.ZipFile(archive) as z:
   names=[n for n in z.namelist() if n.endswith('/gopeed')]
   if len(names)!=1:raise RuntimeError('Unexpected binary archive')
   info=z.getinfo(names[0])
   if info.file_size>128*1024*1024:raise RuntimeError('Binary limit')
   with z.open(names[0]) as src,(bundle/'bin/gopeed').open('wb') as dst:shutil.copyfileobj(src,dst)
  (bundle/'bin/gopeed').chmod(0o755)
  env=dict(os.environ,CGO_ENABLED='0',GOOS='linux',GOARCH={'x86_64':'amd64','aarch64':'arm64'}[arch])
  run(['go','build','-trimpath','-buildvcs=false','-ldflags=-s -w','-o',str(bundle/'bin/engine'),'./cmd/engine'],ROOT/'backend',env)
  run(['go','build','-trimpath','-buildvcs=false','-ldflags=-s -w','-o',str(bundle/'bin/search-helper'),'.'],ROOT/'search-helper',dict(env,GOTOOLCHAIN='go1.27.3'))
  (bundle/'bin/upstream.json').write_text(json.dumps(dict(item,version=LOCK['version'],binarySHA256=hashlib.sha256((bundle/'bin/gopeed').read_bytes()).hexdigest()),indent=2)+'\n')
 legacy=ROOT/'ui/gopeed'
 if legacy.exists():shutil.rmtree(legacy)
 # Notices shipped in SPK and corresponding source offered beside release.
 # The templates' writer replaces only its own files, so start from an empty
 # ui/LICENSES and copy the other licenses beside them.
 licenses=ROOT/'ui/LICENSES'
 if licenses.exists():shutil.rmtree(licenses)
 run(['node','--input-type=module','-e',"import {writeThirdPartyNotices} from '@synopackageland/templates/notices'; writeThirdPartyNotices('frontend','ui/LICENSES',{ui:true});"])
 shutil.copy(SOURCE/'LICENSE',licenses/'Gopeed-GPL-3.0.txt')
 shutil.copy(SOURCE/'LICENSE',ROOT/'LICENSE')
 write_go_notices([ROOT/'build'/('daemon-'+arch)/'bin/gopeed' for arch in LOCK['assets']],licenses)
 write_search_notices(ROOT/'build/daemon-x86_64/bin/search-helper',licenses)
 for name,label in [('@synopackageland/ui','ui'),('vue','vue')]:
  package=ROOT/'frontend/node_modules'/name
  shutil.copy(package/'LICENSE',licenses/f'{label}-MIT.txt')
  # The templates' writer already emits UI-THIRD_PARTY.md; DSM refuses names
  # that differ only by case, so skip a copy it already wrote.
  third=licenses/f'{label}-THIRD_PARTY.md'
  if (package/'LICENSES/THIRD_PARTY.md').exists() and not any(p.name.lower()==third.name.lower() for p in licenses.iterdir()):shutil.copy(package/'LICENSES/THIRD_PARTY.md',third)
 # The SDK's Go packages and Vue components are both compiled into the SPK.
 shutil.copy(ROOT/'frontend/node_modules/@synopackageland/sdk/LICENSE',licenses/'sdk-MIT.txt')
 # Retire classic output and old lazy chunks while preserving notices/assets.
 for name in ('app.js','app.module.js'):
  (ROOT/'ui'/name).unlink(missing_ok=True)
 shutil.rmtree(ROOT/'ui/chunks',ignore_errors=True)
 run(['pnpm','--dir','frontend','run','build'])
if __name__=='__main__':prepare()
