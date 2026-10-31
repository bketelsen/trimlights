import test from 'node:test';
import assert from 'node:assert/strict';
import {latestRequests} from './latest.ts';

// Answers can arrive in any order; only the newest request may apply its answer.
test('a slow older request cannot overwrite a newer answer',async()=>{
 const requests=latestRequests();
 let status='initial';
 const answer=(value,ms)=>new Promise(resolve=>setTimeout(()=>resolve(value),ms));
 const load=async(value,ms)=>{const current=requests.begin();const result=await answer(value,ms);if(current())status=result;};
 await Promise.all([load('slow poll',30),load('fresh refresh',5)]);
 assert.equal(status,'fresh refresh');
 await load('next poll',1);
 assert.equal(status,'next poll');
});
test('each check stays true until a newer request begins',()=>{
 const requests=latestRequests(),other=latestRequests();
 const first=requests.begin();
 assert.equal(first(),true);
 other.begin();
 assert.equal(first(),true,'separate guards are independent');
 const second=requests.begin();
 assert.equal(first(),false);
 assert.equal(second(),true);
});
