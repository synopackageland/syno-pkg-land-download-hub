import {test} from 'node:test';
import assert from 'node:assert/strict';
import {prepareJSONProcess} from './prepared-json.ts';
test('input readiness is reached before the target is admitted, and JSON is sent once',async()=>{
 const command=await prepareJSONProcess(input=>['--input-type=module','-e',"import fs from 'node:fs';const value=JSON.parse(fs.readFileSync(process.argv[1],'utf8'));console.log(JSON.stringify({ok:true,data:value}));",input]);
 try{
  const reply=await command.send({id:'created-after-reader-ready'});
  assert.deepEqual(reply,{ok:true,data:{id:'created-after-reader-ready'}});
  await assert.rejects(command.send({id:'second'}),/already been sent/);
 }finally{await command.close()}
});
test('a prepared command can be disposed before sending any mutation input',async()=>{
 const command=await prepareJSONProcess(input=>['--input-type=module','-e',"import fs from 'node:fs';fs.readFileSync(process.argv[1],'utf8');console.log('{}');",input]);
 await command.close();
 await assert.rejects(command.send({id:'never-admitted'}),/already been sent/);
});
test('an early CLI refusal fails readiness before any job can be admitted',async()=>{
 await assert.rejects(prepareJSONProcess(()=>['-e',"console.log(JSON.stringify({ok:false,error:{code:'refused'}}))"]),/exited before reading/);
});
