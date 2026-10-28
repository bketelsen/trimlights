import test from 'node:test';
import assert from 'node:assert/strict';
import vectors from './pattern-vectors.json' with {type:'json'};
import {renderPattern,newDesign,validateDesign,exportDesign} from './pattern.ts';
for(const vector of vectors)test(vector.name,()=>assert.deepEqual(renderPattern(vector.design,vector.count,vector.tick_ms),vector.expected));
test('bounds and native effects cannot produce misleading frames',()=>{
 for(const [count,tick] of [[0,0],[2049,0],[5,-1],[5,86400001],[1,0.5]])assert.throws(()=>renderPattern(newDesign(),count,tick));
 const native={...newDesign(),effect:'native',brightness:100,speed:0,palette:[],native:{id:1,name:'Native',parameters_hex:'00'.repeat(156),snapshot_at:new Date().toISOString()}};
 assert.equal(validateDesign(native),null);assert.throws(()=>renderPattern(native,10,0));
 for(const run of [{color:'#ff0000',count:0},{color:'red',count:1},{color:'#00ff00',count:1.5}])assert.notEqual(validateDesign({...newDesign(),palette:[run]}),null);
});
test('export round trip is a portable design',()=>{const d=newDesign();assert.deepEqual(JSON.parse(exportDesign(d)),d);});
