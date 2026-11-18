import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import YAML from 'yaml';
import {operationInput} from '../node_modules/@synopackageland/cli/dist/ctl/input.js';
const m=YAML.parse(readFileSync(new URL('../dsm-package.yaml',import.meta.url),'utf8'));
const commands=Object.fromEntries(Object.entries(JSON.parse(readFileSync(new URL('../operations.json',import.meta.url),'utf8')).operations).filter(([,op])=>op.ai!=='hidden'));
test('download hub aliases and bounded task commands use existing routes',()=>{
 assert.ok(m.ctl.aliases.includes('download-hub'));
 for(const key of ['tasks.get','tasks.delete','tasks.removal','gopeed.destination','search.catalog','gopeed.session','gopeed.plugins.list','gopeed.plugins.switch','gopeed.plugins.switch','gopeed.plugins.delete','gopeed.plugins.settings','gopeed.settings.get','gopeed.settings.set','binaries.status','media.tools.status']) assert.ok(commands[key],key);
 assert.equal(commands['tasks.delete'].effect,'destructive');
 assert.equal(commands['gopeed.plugins.delete'].effect,'destructive');
 for(const key of ['downloads.add','downloads.add_torrent']) assert.equal(commands[key].flow.type,'invoke','return admission immediately');
});
test('task schemas reject traversal, foreign identity and unknown properties',()=>{
 for(const key of ['tasks.get','tasks.delete','tasks.removal']){
  assert.deepEqual(operationInput(commands[key],['--id','mine']),{id:'mine'});
  for(const args of [['--id','../other'],['--id','mine','--owner','normal2'],[]])assert.throws(()=>operationInput(commands[key],args));
 }
});
test('destination and engine preference parameters stay narrow',()=>{
 assert.deepEqual(operationInput(commands['gopeed.destination'],['--path','/home/Downloads']),{path:'/home/Downloads',key:'save'});
 assert.throws(()=>operationInput(commands['gopeed.destination'],['--path','/home','--key','other']));
 const good={maxRunning:2,connections:4,seedRatio:0,seedMinutes:0,btPort:16889,ed2kPort:4662,ed2kUdpPort:4672};
 assert.deepEqual(operationInput(commands['gopeed.settings.set'],['--input','-'],JSON.stringify(good)),good);
 assert.deepEqual(operationInput(commands['gopeed.settings.set'],['--input','-'],JSON.stringify({...good,trackers:['udp://tracker.example.org/announce']})).trackers,['udp://tracker.example.org/announce']);
for(const patch of [{btPort:80},{maxRunning:17},{seedRatio:-1},{seedMinutes:10081},{root:true},{trackers:Array(65).fill('udp://t.example.org/a')},{trackers:[1]}])assert.throws(()=>operationInput(commands['gopeed.settings.set'],['--input','-'],JSON.stringify({...good,...patch})));
});
test('synchronous long search APIs are not advertised as nonblocking ctl commands',()=>{
 const blocked=new Set(['search.stream','search.batch','search.query','search.sources.test','search.sources.unlock','search.sources.clearance.test','search.definitions.update','binaries.update','binaries.rollback','gopeed.plugins.install','gopeed.plugins.update','media.tools.update','media.tools.rollback']);
 for(const [key,op]of Object.entries(commands))assert.ok(!blocked.has(op.flow.method),key);
});
test('search and probes use bounded existing SDK job flows and original-id reads',()=>{
 for(const key of ['search.jobs.start','search.sources.test.start','search.jobs.status','search.jobs.result','search.jobs.pages','search.jobs.list','search.jobs.cancel']){
  assert.ok(commands[key],key);assert.equal(commands[key].flow.type,key==='search.jobs.start'||key==='search.sources.test.start'?'job':'invoke',key)
 }
 assert.equal(commands['search.jobs.start'].flow.start,'search.jobs.start');
 assert.throws(()=>operationInput(commands['search.jobs.start'],['--query','ubuntu','--page','1','--source-ids','[]']));
});
