import test from 'node:test';
import assert from 'node:assert/strict';
import {clockTime,repeatLabel} from './uiText.ts';

// The browser's locale decides the format, so compare with the same formatter.
const local=(/** @type {number} */hour,/** @type {number} */minute)=>new Date(2000,0,1,hour,minute).toLocaleTimeString(undefined,{hour:'numeric',minute:'2-digit'});

test('clockTime formats controller HH:MM times in the local format',()=>{
 for(const [value,hour,minute] of /** @type {[string,number,number][]} */([['00:00',0,0],['07:05',7,5],['12:00',12,0],['19:30',19,30],['23:59',23,59]])){
  const label=clockTime(value);
  assert.equal(label,local(hour,minute),value);
  assert.ok(label.length>0,value);
 }
 // Morning and evening times with the same digits stay different, and minutes count.
 assert.notEqual(clockTime('07:30'),clockTime('19:30'));
 assert.notEqual(clockTime('07:05'),clockTime('07:06'));
});

test('clockTime returns any other text unchanged',()=>{
 for(const value of ['','7:05','07:5','0705','07:05:00',' 07:05','07:05 ','07:05\n','ab:cd','--:--','unknown']){
  assert.equal(clockTime(value),value,JSON.stringify(value));
 }
});

test('repeatLabel names the controller repetition in household words',()=>{
 assert.equal(repeatLabel('every day'),'Every day');
 assert.equal(repeatLabel('every day (observed)'),'Every day');
 assert.equal(repeatLabel('today only'),'One day only');
 assert.equal(repeatLabel('today only (observed)'),'One day only');
});

test('repeatLabel calls every other value a custom repeat',()=>{
 for(const value of ['','unknown','Every day','TODAY ONLY',' every day','weekdays','day only','everyday']){
  assert.equal(repeatLabel(value),'Custom repeat',JSON.stringify(value));
 }
});
