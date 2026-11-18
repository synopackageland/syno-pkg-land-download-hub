import {test} from 'node:test';import assert from 'node:assert/strict';import {readFileSync} from 'node:fs';import YAML from 'yaml';
import {operationInput,requestPlan} from '../node_modules/@synopackageland/cli/dist/ctl/input.js';
const manifest=YAML.parse(readFileSync(new URL('../dsm-package.yaml',import.meta.url),'utf8'));const commands=Object.fromEntries(Object.entries(JSON.parse(readFileSync(new URL('../operations.json',import.meta.url),'utf8')).operations).filter(([,op])=>op.ai!=='hidden'));
const id='a'.repeat(32);
test('installed ctl parser accepts credential argv and masks plans',()=>{
 for(const [key,args]of [
  ['search.sources.field',['--id',id,'--field','passkey','--value','synthetic-secret']],
  ['search.solver.set',['--url','http://solver.example:8191']],
 ]as const){const op=commands[key];const params=operationInput(op,args);const plan=requestPlan({payload:{package:{id:'SynoPkgGopeed',version:'0.2.2'},transport:{path:'/fixed'},commands}},key,params);assert.ok(!JSON.stringify(plan).includes('synthetic-secret'));assert.ok(!JSON.stringify(plan).includes('browser-agent'));assert.equal(params.id??id,id)}
});
test('ctl has explicit CRUD and solver operations without engine proxy',()=>{
 for(const suffix of ['save','patch','field','enabled','delete','config'])assert.ok(commands['search.sources.'+suffix]);
 assert.ok(!Object.keys(commands).some(k=>k.startsWith('search.cookie.')));
 const input=operationInput(commands['search.sources.patch'],['--id',id,'--name','Renamed']);assert.deepEqual(input,{id,name:'Renamed'});
 assert.equal(commands['search.sources.config'].flow.method,'search.sources.config');
 assert.equal(commands['search.sources.delete'].effect,'destructive');
});
