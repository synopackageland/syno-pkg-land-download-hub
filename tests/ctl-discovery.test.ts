import './catalog-fixture.ts';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtempSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {loadStandaloneManifest} from '../node_modules/@synopackageland/cli/dist/standalone/manifest.js';
import {buildApiDescription} from '../node_modules/@synopackageland/cli/dist/standalone/api-description.js';
import {inspectApiDescription} from '../node_modules/@synopackageland/cli/dist/standalone/api-description-inspect.js';
import {operationInput} from '../node_modules/@synopackageland/cli/dist/ctl/input.js';
import {CtlStore} from '../node_modules/@synopackageland/cli/dist/ctl/store.js';
import {runCtl} from '../node_modules/@synopackageland/cli/dist/ctl/cli.js';

test('compiled routes produce an inspectable descriptor, real ctl list and confirmed plans',async()=>{
 const loaded=loadStandaloneManifest(resolve('.'));assert.equal(loaded.ok,true,JSON.stringify(loaded));
 const built=await buildApiDescription(loaded.manifest,undefined,(command,args,options)=>{
  const r=spawnSync(command,args,{cwd:options.cwd,env:options.env,encoding:'utf8'});
  return {status:r.status,stdout:r.stdout,stderr:r.stderr};
 });
 const {description,registry}=built;
 const info={id:description.payload.package.id,version:description.payload.package.version};
 assert.deepEqual(inspectApiDescription(JSON.stringify(description),info,description.sha256,registry),[]);
 assert.match(inspectApiDescription(JSON.stringify(description),info,description.sha256,{...registry,'tasks.delete':'administrators'})[0],/BINDING_MISMATCH/);
 for(const [key,op] of Object.entries(description.payload.commands)){
  for(const example of op.examples??[])assert.doesNotThrow(()=>operationInput(op,['--input','-'],JSON.stringify(example)),key);
 }
 for(const key of ['gopeed.settings.get','gopeed.settings.set','binaries.status','media.tools.status','gopeed.plugins.list','gopeed.plugins.switch','gopeed.plugins.delete'])assert.equal(description.payload.commands[key].permission,'administrators',key);
 for(const key of ['tasks.get','tasks.delete','tasks.removal','gopeed.destination','search.catalog'])assert.equal(description.payload.commands[key].permission,'authenticated',key);
 const root=mkdtempSync(join(tmpdir(),'ctl-fixture-'));const oldExit=process.exitCode;
 const host={name:'fixture',origin:'https://fixture.invalid:5001',username:'alice',approved:{[info.id]:{version:info.version}}};
 const session={origin:host.origin,username:host.username,cookie:'synthetic-cookie',token:'synthetic-token'};
 const store=new CtlStore(root,{get:async()=>session,set:async()=>{},delete:async()=>{}});store.save(host);
 const business=[];const output=[];let searchOutcome='busy';
 const requestFor=()=>async(path,body)=>{
  if(path==='/webapi/entry.cgi')return {success:true,data:{total:1,packages:[{id:info.id,version:info.version}]}};
  if(body.method==='synopkgland.describe')return {success:true,data:description};
  business.push(body);
  const data=body.method==='search.jobs.start'?{outcome:searchOutcome,job:{id:'existing-job',key:'original-key',state:'running',operation:'search',cancelRequested:false},duplicate:false}
   :body.method==='search.jobs.status'?{id:'existing-job',key:'original-key',state:'succeeded',operation:'search',cancelRequested:false}
   :body.method==='search.jobs.result'?{kind:'search',pages:0}
   :body.method==='tasks.delete'?{deleting:true,fileKept:false}
   :body.method==='tasks.delete.preview'?{operation:'tasks.delete',effect:'destructive',confirmationRequired:true,checks:'execution_required'}
   :body.method==='gopeed.plugins.list'?[{identity:'example',name:null,title:null,description:null,version:null,author:null,disabled:null,settings:{token:'synthetic-setting-secret'},repository:null}]
   :{accepted:true,job_id:'ctl-example-0001',completion:'not_checked'};
  return {success:true,data};
 };
 const run=async(args)=>{output.length=0;process.exitCode=0;await runCtl([...args,'--host','fixture','--json'],{store,requestFor,out:s=>output.push(JSON.parse(s)),isTTY:false});return output[0]};
 try{
  const listing=await run(['list','--app',info.id]);assert.equal(listing.ok,true,JSON.stringify(listing));
  assert.ok(JSON.stringify(listing).includes('download-hub'));assert.ok(JSON.stringify(listing).includes('tasks.delete'));
  for(const [key,op] of Object.entries(description.payload.commands).filter(([,op])=>op.effect!=='read')){
   const input=JSON.stringify(op.examples[0]);
   // runCtl reads stdin only for --input -, so inject JSON with the service in
   // the dedicated unit tests; here use a local input file to exercise argv.
   const file=join(root,'input.json');writeFileSync(file,input);
   const args=['download-hub',...op.cli,'--input',file];
   const before=business.length;const dry=await run([...args,'--dry-run']);
   assert.equal(dry.ok,true,key+JSON.stringify(dry));assert.equal(business.length,before,key);
   const refusal=await run(args);assert.equal(refusal.ok,false,key);assert.equal(refusal.error.code,'CTL_CONFIRM_REQUIRED',key);assert.equal(business.length,before,key);
  }
  const previewBefore=business.length;
  const draftInput=join(root,'draft.json');writeFileSync(draftInput,JSON.stringify({id:'mine'}));
  const draft=await run(['tool','download-hub','tasks.delete','--input',draftInput]);
  assert.equal(draft.ok,true,JSON.stringify(draft));
  assert.equal(business.length,previewBefore+1);
  assert.equal(business.at(-1).method,'tasks.delete.preview','AI drafts never execute the write');
  const confirmed=await run(['download-hub','tasks','delete','--id','mine','--yes']);assert.equal(confirmed.ok,true,JSON.stringify(confirmed));assert.deepEqual(business.at(-1),{method:'tasks.delete',params:{id:'mine'}});
  const count=business.length;const admitted=await run(['gopeed','downloads','add','--key','ctl-example-0001','--url','https://example.com/file','--yes']);assert.equal(admitted.ok,true);assert.equal(business.length,count+1,'admission never synchronously polls');
  const searchInput=join(root,'search.json');writeFileSync(searchInput,JSON.stringify({key:'new-search',sourceIds:['a'.repeat(32)],query:'ubuntu',page:1}));
  const beforeSearch=business.length;const busy=await run(['gopeed','search','run','--input',searchInput,'--no-wait']);
  assert.equal(busy.ok,true,JSON.stringify(busy));assert.equal(busy.data.job.job.id,'existing-job');assert.equal(busy.data.job.outcome,'busy');assert.equal(business.length,beforeSearch+1,'start must not poll');
  searchOutcome='accepted';
  const completed=await run(['gopeed','search','run','--input',searchInput]);
  assert.equal(completed.ok,true,JSON.stringify(completed));assert.deepEqual(completed.data,{kind:'search',pages:0});
  assert.deepEqual(business.slice(-3).map(request=>request.method),['search.jobs.start','search.jobs.status','search.jobs.result']);
  // CLI 0.48.21 preserves responses; plan/log privacy is checked separately.
  const extensions=await run(['gopeed','extensions','list']);assert.equal(extensions.ok,true);
  assert.equal(extensions.data[0].settings.token,'synthetic-setting-secret');
 }finally{process.exitCode=oldExit;rmSync(root,{recursive:true,force:true})}
});
