import test from 'node:test';
import assert from 'node:assert/strict';
import {PreviewClock} from './previewClock.ts';
import {newDesign,renderPattern} from './pattern.ts';

test('first and repeated frame timestamps always produce valid renderer ticks',()=>{
 const clock=new PreviewClock(),design={...newDesign(),effect:'chase',speed:73};
 // A frame can have started before the React effect, or share a timestamp.
 for(const now of [12.5,12.5,12.3,28.9,45.6]){
  const tick=clock.tick(now,true);
  assert.ok(Number.isInteger(tick)&&tick>=0);
  assert.doesNotThrow(()=>renderPattern(design,48,tick));
 }
});
test('play changes colors, pause retains them, resume excludes time away',()=>{
 const clock=new PreviewClock(),design={...newDesign(),effect:'chase',speed:100};
 const first=renderPattern(design,48,clock.tick(100,true));
 const moving=renderPattern(design,48,clock.tick(200,true));
 assert.notDeepEqual(moving,first);
 clock.restart();
 assert.deepEqual(renderPattern(design,48,clock.tick(5000,false)),moving);
 clock.restart();
 assert.deepEqual(renderPattern(design,48,clock.tick(9000,true)),moving);
 assert.notDeepEqual(renderPattern(design,48,clock.tick(9100,true)),moving);
});
test('slow or suspended frames do not jump through unseen animation',()=>{
 const clock=new PreviewClock();
 assert.equal(clock.tick(0,true),0);
 assert.equal(clock.tick(60000,true),100);
 clock.restart();
 assert.equal(clock.tick(120000,true),100);
 assert.equal(clock.tick(120050,true),150);
});
