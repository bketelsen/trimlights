import test from 'node:test';
import assert from 'node:assert/strict';
import {appendNativeRules} from './calendarDraft.ts';
const original={schema:1,timezone:'UTC',location:{latitude:45,longitude:10},rules:[{id:'existing',name:'Kept'}],override:{id:'override',action:{kind:'off'},starts_at:'2026-10-28T20:00:00Z',expires_at:'2026-10-28T21:00:00Z'}};
const incoming=[{id:'native-timer-1',name:'Timer 1',enabled:true},{id:'native-timer-2',name:'Timer 2',enabled:false}];
test('append keeps unsaved rules, location and original override expiry',()=>{
 const current=structuredClone(original);const additions=structuredClone(incoming);
 const result=appendNativeRules(current,structuredClone(current),additions);
 assert.deepEqual(result,{...original,rules:[...original.rules,...incoming]});
 assert.deepEqual(current,original);
 additions[0].name='Changed after adding';assert.equal(result.rules[1].name,'Timer 1');
});
test('a delayed proposal cannot overwrite newer draft changes',()=>{
 for(const change of [{timezone:'Europe/London'},{location:{latitude:0,longitude:0}},{override:undefined},{rules:[{id:'newer'}]}]){
  const current={...structuredClone(original),...change};const before=structuredClone(current);
  assert.throws(()=>appendNativeRules(current,original,incoming),/draft changed/);
  assert.deepEqual(current,before);
 }
});
test('repeat submission and duplicate IDs preserve original rules',()=>{
 const first=appendNativeRules(original,original,incoming);
 assert.throws(()=>appendNativeRules(first,first,incoming),/already/);
 assert.throws(()=>appendNativeRules(original,original,[incoming[0],incoming[0]]),/already/);
 assert.throws(()=>appendNativeRules(original,original,[{id:'override'}]),/already/);
 const full={...original,rules:Array.from({length:99},(_,i)=>({id:`rule-${i}`}))};
 assert.throws(()=>appendNativeRules(full,full,incoming),/100-rule/);
 assert.equal(appendNativeRules(full,full,[incoming[0]]).rules.length,100);
});
