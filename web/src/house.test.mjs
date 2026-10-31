import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {canvasTouchAction,positions,rotateView,sourceRun} from './house.ts';
for(const v of JSON.parse(readFileSync(new URL('./house-vectors.json',import.meta.url),'utf8'))){test(v.name,()=>assert.deepEqual(positions(v.mapping,v.count,v.width,v.height),v.expected));}
test('four quarter turns preserve bulb order and coordinates',()=>{const view={photo_id:'a',turns:0,mappings:[{run_id:'a',points:[{x:0,y:0},{x:1,y:1}],reverse:true,adjustments:[{index:1,point:{x:.5,y:.25}}]}]};let turned=view;for(let i=0;i<4;i++)turned=rotateView(turned);assert.deepEqual(turned,view);});
test('mirrored output shares source addresses',()=>{const source={id:'front',start:12,count:5};const mirror={id:'side',start:1,count:5,mirror_of:'front'};assert.equal(sourceRun(mirror,[source,mirror]),source);});
test('only drawing, dragging or a zoomed photo blocks page scrolling',()=>{
 assert.equal(canvasTouchAction('pan',1),'pan-y');
 assert.equal(canvasTouchAction('pan',1.5),'none');
 for(const mode of ['draw','points','bulbs'])assert.equal(canvasTouchAction(mode,1),'none');
});
