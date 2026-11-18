#!/usr/bin/env python3
"""Loopback-only UI fixture. No DSM/NAS calls; never included in SPK."""
from http.server import ThreadingHTTPServer,SimpleHTTPRequestHandler
from pathlib import Path
ROOT=Path(__file__).resolve().parent.parent
HTML='''<!doctype html><meta charset="utf-8"><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/webman/3rdparty/SynoPkgGopeed/app/app.css"><div id="app"></div><script src="/webman/3rdparty/SynoPkgGopeed/app/app.js" data-package="SynoPkgGopeed"></script><script>
window.calls=[];window.selected='';window.expired=false;
const config={downloadDir:'/home',maxRunning:2,proxy:{},protocolConfig:{http:{},bt:{}},extra:{bt:{trackerSubscribeUrls:[],subscribeTrackers:[],autoUpdateTrackers:false,customTrackers:[]},notifyWhenNewVersion:false,locale:'zh_TW',githubMirror:{enabled:false}}};
const ctx={locale:'zh-TW',reopen:()=>()=>{},call:async(method,p={})=>{
 calls.push({method,params:p});if(expired)throw {code:'session_required'};
 if(method==='gopeed.pending')return [];
 if(method==='gopeed.folder'){window.selected=p.folder;return {folder:p.folder}}
 if(method==='gopeed.request'){
  let data=null;if(p.path==='/api/v1/config')data=config;
  else if(p.path==='/api/v1/tasks'||p.path==='/api/v1/extensions')data=[];
  else throw {code:'unsupported'};
  if(p.method!=='GET')throw {code:'unsupported'};
  return {code:0,data};
 }
 throw {code:'unsupported'};
}};
window.mounted=DsmPackageApps.SynoPkgGopeed.mount(document.getElementById('app'),ctx);
</script>'''
class Handler(SimpleHTTPRequestHandler):
 def do_GET(self):
  if self.path=='/':
   b=HTML.encode();self.send_response(200);self.send_header('Content-Type','text/html; charset=utf-8');self.send_header('Content-Length',str(len(b)));self.end_headers();self.wfile.write(b);return
  prefix='/webman/3rdparty/SynoPkgGopeed/app/'
  if not self.path.startswith(prefix):self.send_error(404);return
  self.path='/ui/'+self.path[len(prefix):];super().do_GET()
 def log_message(self,*args):pass
if __name__=='__main__':
 import os
 os.chdir(ROOT)
 ThreadingHTTPServer(('127.0.0.1',18761),Handler).serve_forever()
