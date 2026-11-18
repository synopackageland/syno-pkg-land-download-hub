import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import YAML from 'yaml';
import {readRegistry,registryTools} from '../node_modules/@synopackageland/cli/dist/ctl/registry.js';
const root=new URL('../',import.meta.url);
test('search page and wait contracts retain the display hash without exposing download links',async()=>{
 const {ajv}=await import('../node_modules/@synopackageland/cli/dist/ctl/descriptor.js');
 const registry=readRegistry(new URL('operations.json',root).pathname);
 for(const method of ['search.jobs.pages','search.jobs.wait']){
  const validate=ajv.compile(registry.operations[method].resultSchema);
  const result={items:[{sourceId:'owned-source',page:1,results:[{resultId:'owned-result',sourceId:'owned-source',title:'Fixture',hash:'a'.repeat(40)}],morePossible:false}],offset:0,total:1};
  assert.equal(validate(result),true,`${method}: ${JSON.stringify(validate.errors)}`);
  const leaked=structuredClone(result) as any;leaked.items[0].results[0].link='https://example.org/private-download';
  assert.equal(validate(leaked),false,`${method} must keep links server-side`);
 }
});
test('one v2 registry owns the real methods, schemas and safe AI policy',()=>{
 const manifest=YAML.parse(readFileSync(new URL('dsm-package.yaml',root),'utf8'));
 assert.equal(manifest.ctl.registry,'operations.json');
 assert.equal(manifest.ctl.commands,undefined);
 const registry=readRegistry(new URL('operations.json',root).pathname);
 for(const [name,op] of Object.entries(registry.operations)){
  assert.ok(Object.keys(op.resultSchema).length,name);
  if(op.ai!=='hidden' && op.effect!=='read'){
   assert.equal(op.ai,'draft',name);
   assert.equal(registry.operations[op.preview].ai,'preview',name);
   assert.deepEqual(registry.operations[op.preview].paramsSchema,op.paramsSchema,name);
  }
 }
 const tools=registryTools(registry);
 assert.ok(tools.some(op=>op.name==='tasks.delete' && op.execution==='preview-and-human-confirmation'));
 assert.ok(!tools.some(op=>op.name==='gopeed.request'));
 assert.equal(registry.operations['search.jobs.start'].flow.type,'job');
 assert.equal(registry.operations['search.sources.test.start'].flow.type,'job');
 assert.equal(registry.operations['downloads.add'].flow.type,'invoke');
 assert.equal(registry.operations['downloads.add'].resultSchema.properties.completion.const,'not_checked');
});

test('every successful real handler response conforms to its registered schema',async(t)=>{
 const {mkdtempSync,rmSync}=await import('node:fs');
 const {tmpdir}=await import('node:os');
 const {join}=await import('node:path');
 const {spawnSync}=await import('node:child_process');
 const {ajv}=await import('../node_modules/@synopackageland/cli/dist/ctl/descriptor.js');
 const dir=mkdtempSync(join(tmpdir(),'registry-results-'));
 try{
  const file=join(dir,'results.ndjson');
  // Catalog icons are a distinct successful producer shape and belong in this gate.
  const run=spawnSync('go',['test','./internal/gopeed','-run','Test(Ctl|Registry|Search|Browser|Settings|Plugin|Official|MediaTool|Protocol|Tool|Destination|Catalog)','-count=1'],{cwd:new URL('../backend/',import.meta.url),env:{...process.env,REGISTRY_RESULTS:file},encoding:'utf8',timeout:180000});
  assert.equal(run.status,0,run.stdout+run.stderr);
  const registry=readRegistry(new URL('operations.json',root).pathname);
  const validators=new Map(Object.entries(registry.operations).map(([name,op])=>[name,ajv.compile(op.resultSchema)]));
  const seen=new Set();let count=0;
  for(const line of readFileSync(file,'utf8').trim().split('\n')){
   const {method,data}=JSON.parse(line);const validate=validators.get(method);
   if(!validate)continue;
   assert.equal(validate(data),true,`${method}: ${JSON.stringify(validate.errors)}`);
   seen.add(method);count++;
  }
  t.diagnostic(`${count} real handler responses; ${seen.size} registered methods exercised`);
  assert.ok(count>100,`only ${count} handler outputs checked`);
  const missing=Object.entries(registry.operations).filter(([name,op])=>op.ai!=='hidden'&&!seen.has(name)).map(([name])=>name);
  assert.deepEqual(missing,[],'every exposed operation needs successful real-handler evidence');
 }finally{rmSync(dir,{recursive:true,force:true})}
});
