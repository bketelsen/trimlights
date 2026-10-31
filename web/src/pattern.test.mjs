import test from 'node:test';
import assert from 'node:assert/strict';
import vectors from './pattern-vectors.json' with {type:'json'};
import nameVectors from './name-vectors.json' with {type:'json'};
import {renderPattern,newDesign,validateDesign,validName,exportDesign,parsePatternFile,notPatternFile} from './pattern.ts';
for(const vector of vectors)test(vector.name,()=>assert.deepEqual(renderPattern(vector.design,vector.count,vector.tick_ms),vector.expected));
for(const vector of nameVectors)test(`name: ${vector.name}`,()=>{assert.equal(validName(vector.value),vector.valid);assert.equal(validateDesign({...newDesign(),name:vector.value})===null,vector.valid);});
// JSON vectors cannot carry a lone surrogate to Go (its decoder stores U+FFFD), so these cases stay here.
test('names reject lone surrogates and keep valid pairs',()=>{for(const name of ['\uD800','\uDC00','Porch \uD83C','\uDF83 Porch','\uDF83\uD83C'])assert.equal(validName(name),false,JSON.stringify(name));assert.equal(validName('Porch \uD83C\uDF83'),true);});
test('bounds and native effects cannot produce misleading frames',()=>{
 for(const [count,tick] of [[0,0],[2049,0],[5,-1],[5,86400001],[1,0.5]])assert.throws(()=>renderPattern(newDesign(),count,tick));
 const native={...newDesign(),effect:'native',brightness:100,speed:0,palette:[],native:{id:1,name:'Native',parameters_hex:'00'.repeat(156),snapshot_at:new Date().toISOString()}};
 assert.equal(validateDesign(native),null);assert.throws(()=>renderPattern(native,10,0));
 for(const run of [{color:'#ff0000',count:0},{color:'red',count:1},{color:'#00ff00',count:1.5}])assert.notEqual(validateDesign({...newDesign(),palette:[run]}),null);
});
test('export round trip is a portable design',()=>{const d=newDesign();assert.deepEqual(JSON.parse(exportDesign(d)),d);});
test('pattern files that are not designs get one clear message',()=>{
 for(const text of ['','{','not json','<html></html>','null','5','"pattern"','[]','{}','{"schema":2,"name":"Porch"}'])assert.throws(()=>parsePatternFile(text),{message:notPatternFile},JSON.stringify(text));
 // A design with a bad field keeps the message for that field.
 assert.throws(()=>parsePatternFile(JSON.stringify({...newDesign(),brightness:101})),{message:/Brightness and speed/});
 assert.deepEqual(parsePatternFile(exportDesign(newDesign())),newDesign());
});
