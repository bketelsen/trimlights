import test from 'node:test';
import assert from 'node:assert/strict';
import {pageFromHash,pageHash,pageTitle,pages,routeIndex,routePush,routeSettle,routeState,routeStep} from './route.ts';

test('every page round-trips through its URL hash',()=>{for(const page of pages)assert.equal(pageFromHash(pageHash(page)),page);});
test('an empty or unknown hash opens Home',()=>{for(const hash of ['','#','#unknown','#Home','#/house'])assert.equal(pageFromHash(hash),'home');});
test('each page has its own document title',()=>{
 const titles=pages.map(pageTitle);
 assert.equal(new Set(titles).size,pages.length);
 assert.equal(pageTitle('house'),'Trimlights · House');
 assert.equal(pageTitle(null),'Trimlights');
});

const ask=answers=>{const calls=[];return {calls,canLeave:()=>{calls.push(true);return answers.shift()??true;}};};
const apply=(model,events,canLeave)=>{const steps=[];for(const [page,index] of events){const step=routeStep(model,page,index,canLeave);steps.push(step);model=step.model;}return {model,steps};};

test('route state round-trips and rejects foreign state',()=>{
 assert.equal(routeIndex(routeState(3)),3);
 for(const state of [null,undefined,'x',{},{trimlightsRoute:'2'},{trimlightsRoute:1.5}])assert.equal(routeIndex(state),undefined);
});
test('a refused Back returns to the same entry and keeps the earlier entries',()=>{
 // Home(0) -> Library(1) -> House(2), with unsaved edits on House.
 let model={page:'home',index:0};
 model=routePush(model,'patterns');model=routePush(model,'house');
 assert.deepEqual(model,{page:'house',index:2});
 const refuse=ask([false]);
 // Back fires popstate and hashchange; the app goes forward 1 entry; then the landing events arrive.
 const back=apply(model,[['patterns',1],['patterns',1],['house',2],['house',2]],refuse.canLeave);
 assert.equal(refuse.calls.length,1);
 assert.deepEqual(back.steps.map(s=>s.go),[1,undefined,undefined,undefined]);
 assert.ok(back.steps.every(s=>!s.show&&s.stamp===undefined&&!s.replace));
 assert.deepEqual(back.model,{page:'house',index:2});
 // After the save, Back reaches Library and then Home: no entry was lost.
 const accept=ask([]);
 const again=apply(back.model,[['patterns',1],['patterns',1],['home',0],['home',0]],accept.canLeave);
 assert.deepEqual(again.steps.map(s=>s.show),['patterns',undefined,'home',undefined]);
 assert.deepEqual(again.model,{page:'home',index:0});
});
test('the next navigation after a refusal continues from the kept entry',()=>{
 const refuse=ask([false]);
 const {model}=apply({page:'house',index:1},[['home',0],['house',1]],refuse.canLeave);
 assert.deepEqual(routePush(model,'settings'),{page:'settings',index:2});
 const forward=routeStep({page:'house',index:1},'settings',2,()=>false);
 assert.equal(forward.go,-1);
});
test('repeated Back without edits shows each page once',()=>{
 const accept=ask([]);
 const {model,steps}=apply({page:'settings',index:3},[['house',2],['house',2],['patterns',1],['home',0]],accept.canLeave);
 assert.deepEqual(steps.map(s=>s.show),['house',undefined,'patterns','home']);
 assert.deepEqual(model,{page:'home',index:0});
 assert.equal(accept.calls.length,3);
});
test('a typed hash is stamped as a new entry, or left with Back when refused',()=>{
 const accepted=apply({page:'home',index:0},[['house',undefined],['house',1]],()=>true);
 assert.deepEqual(accepted.steps[0],{model:{page:'house',index:1},show:'house',stamp:1});
 assert.deepEqual(accepted.model,{page:'house',index:1});
 const refuse=ask([false]);
 const refused=apply({page:'house',index:2},[['patterns',undefined],['patterns',undefined],['house',2]],refuse.canLeave);
 assert.equal(refuse.calls.length,1);
 assert.equal(refused.steps[0].go,-1);
 assert.deepEqual(refused.model,{page:'house',index:2});
 const same=routeStep({page:'home',index:0},'home',undefined,()=>assert.fail('no question for the same page'));
 assert.deepEqual(same,{model:{page:'home',index:1},stamp:1});
});
test('a changed page on the same entry is replaced back when refused',()=>{
 assert.deepEqual(routeStep({page:'house',index:2},'home',2,()=>false),{model:{page:'house',index:2},replace:true});
 assert.deepEqual(routeStep({page:'house',index:2},'home',2,()=>true),{model:{page:'home',index:2},show:'home'});
});

// A small browser: history entries, a cursor and queued (asynchronous) history.go calls.
// Each traversal fires popstate and hashchange, as browsers do for a hash change.
function browser(page){
 const b={entries:[{page,index:0}],cursor:0,queued:[],model:{page,index:0},shown:page,asks:0};
 b.event=leave=>{const entry=b.entries[b.cursor];const step=routeStep(b.model,entry.page,entry.index,()=>{b.asks++;return leave;});b.model=step.model;if(step.stamp!==undefined)entry.index=step.stamp;if(step.replace)b.entries[b.cursor]={page:step.model.page,index:step.model.index};if(step.go)b.queued.push(step.go);if(step.show)b.shown=step.show;};
 b.traverse=(delta,leave=true)=>{b.cursor+=delta;assert.ok(b.cursor>=0&&b.cursor<b.entries.length);b.event(leave);b.event(leave);};
 b.land=()=>{const asks=b.asks;b.traverse(b.queued.shift());assert.equal(b.asks,asks);};
 b.click=next=>{const pushed=routePush(b.model,next);if(!pushed||next===b.shown)return false;b.model=pushed;b.entries.splice(b.cursor+1,Infinity,{page:next,index:pushed.index});b.cursor++;b.shown=next;return true;};
 b.pages=()=>b.entries.map(e=>e.page);
 return b;
}
test('a navigation before a refused Back has returned does not push or lose entries',()=>{
 const b=browser('home');
 assert.ok(b.click('patterns')&&b.click('house'));
 b.traverse(-1,false);
 assert.equal(b.asks,1);
 assert.deepEqual(b.queued,[1]);
 assert.equal(b.cursor,1);
 // history.go(1) has not completed yet: the click must not push from the refused entry.
 assert.equal(b.click('settings'),false);
 assert.deepEqual(b.pages(),['home','patterns','house']);
 b.land();
 assert.equal(b.cursor,2);
 assert.deepEqual(b.model,{page:'house',index:2});
 assert.equal(b.shown,'house');
 // After the landing, navigation and Back work with correct positions.
 assert.ok(b.click('settings'));
 assert.deepEqual(b.pages(),['home','patterns','house','settings']);
 assert.deepEqual(b.entries.map(e=>e.index),[0,1,2,3]);
 assert.equal(b.cursor,3);
 b.traverse(-1);b.traverse(-1);
 assert.equal(b.shown,'patterns');
 assert.deepEqual(b.model,{page:'patterns',index:1});
});
test('a restoration that never lands is settled after the wait',()=>{
 const b=browser('home');
 b.click('house');
 b.traverse(-1,false);
 const pending=b.model.restoring;
 assert.ok(pending);
 b.queued=[]; // the history.go call never lands
 assert.equal(b.click('settings'),false);
 const settled=routeSettle(b.model,pending,b.entries[b.cursor].index);
 assert.deepEqual(settled,{model:{page:'house',index:0},replace:true});
 b.model=settled.model;b.entries[b.cursor]={page:'house',index:0};
 assert.ok(b.click('settings'));
 // The refused entry now holds the shown page, so the URL matches it again.
 assert.deepEqual(b.pages(),['house','settings']);
 assert.equal(b.cursor,1);
 // A settle after a landed restoration changes nothing.
 const landed={page:'house',index:2};
 assert.deepEqual(routeSettle(landed,{to:2,from:1},1),{model:landed});
});
