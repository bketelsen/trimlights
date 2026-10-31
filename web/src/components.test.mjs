// Smoke tests: render components to static HTML on the server, as React does
// before any effect runs. scripts/build-component-tests.mjs compiles them first.
import test from 'node:test';
import assert from 'node:assert/strict';
import {createElement} from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import native from '../.test-build/NativeControls.js';
import preview from '../.test-build/HousePreview.js';
import boundary from '../.test-build/ErrorBoundary.js';

const {NativeControls}=native,{HousePreview,HousePreviewBody}=preview,{ErrorFallback}=boundary;
const noop=()=>{};
const props={available:true,busy:false,csrf:'token',refreshVersion:0,onBusy:noop,onComplete:async()=>{},onError:noop};
const snapshot={queried_at:'2026-10-30T20:00:00Z',stable_sha256:'0'.repeat(64),patterns:[{id:1,name:'Warm white'},{id:2,name:''}],playback:{mode:'manual',matching_pattern_ids:[1]}};
const render=(component,values)=>renderToStaticMarkup(createElement(component,values));
const tags=(html,name)=>html.match(new RegExp(`<${name}\\b[^>]*>`,'g'))??[];
const text=html=>html.replace(/<[^>]+>/g,' ').replace(/\s+/g,' ');

test('NativeControls renders a labelled region with the snapshot patterns',()=>{
 const html=render(NativeControls,{...props,snapshot});
 assert.match(html,/<section[^>]*aria-labelledby="native-controls-title"/);
 assert.match(html,/<h2 id="native-controls-title">Your lights<\/h2>/);
 assert.match(text(html),/Playing a pattern/);
 assert.match(html,/class="now-playing">Warm white</);
 // An unnamed pattern gets a readable fallback name.
 assert.deepEqual(tags(html,'option').length,3);
 assert.match(html,/<option value="2">Pattern 2<\/option>/);
 for(const label of ['Play pattern','Turn off','Follow timers'])assert.match(html,new RegExp(`<button[^>]*>${label}</button>`));
 assert.doesNotMatch(text(html),/Refresh your lights to get started/);
 assert.doesNotMatch(html,/role="alert"/);
});

test('NativeControls keeps every control action disabled until the phone app is acknowledged',()=>{
 const html=render(NativeControls,{...props,snapshot});
 const buttons=tags(html,'button');
 assert.equal(buttons.length,3);
 for(const button of buttons)assert.match(button,/\bdisabled=""/);
 const checkbox=tags(html,'input').find(input=>/type="checkbox"/.test(input));
 assert.ok(checkbox);
 assert.doesNotMatch(checkbox,/checked/);
});

test('NativeControls before the first status shows a neutral checking state',()=>{
 const html=render(NativeControls,{...props,available:false,checking:true});
 assert.match(text(html),/Checking your lights…/);
 assert.match(text(html),/Checking…/);
 assert.doesNotMatch(text(html),/Refresh your lights to get started|Refresh to check/);
 assert.doesNotMatch(html,/role="alert"/);
});

test('NativeControls without a snapshot asks for a refresh and reports busy work',()=>{
 const idle=render(NativeControls,{...props,available:false});
 assert.match(text(idle),/Refresh to check/);
 assert.match(text(idle),/Refresh your lights to get started/);
 assert.equal(tags(idle,'option').length,1);
 const busy=render(NativeControls,{...props,available:false,busy:true});
 assert.match(busy,/role="status"[^>]*>Updating lights…</);
 assert.match(tags(busy,'select')[0],/disabled=""/);
});

test('HousePreview renders a closed disclosure with only its summary',()=>{
 const html=render(HousePreview,{patternID:'p1',revision:3});
 assert.match(html,/^<details class="house-preview"><summary>Preview on your house<\/summary><\/details>$/);
});

test('HousePreviewBody shows an error alone, without the loading text',()=>{
 const base={error:'',photos:[],photoID:'',onPhoto:noop};
 const failed=render(HousePreviewBody,{...base,error:'Could not load your house. Please try again.'});
 assert.equal(failed,'<p role="alert">Could not load your house. Please try again.</p>');
 assert.equal(render(HousePreviewBody,base),'<p>Loading your house…</p>');
 // A pinned pattern that is unavailable fails after the house loaded: the error still stands alone.
 const house={revision:1,layout:{schema:1,runs:[],views:[]}};
 assert.doesNotMatch(render(HousePreviewBody,{...base,house,error:'This saved pattern is unavailable'}),/Loading|Add a photo/);
 assert.match(render(HousePreviewBody,{...base,house}),/Add a photo and trace your roofline/);
});

test('ErrorFallback says that unsaved changes are lost and reloads only on request',()=>{
 const html=render(ErrorFallback,{onReload:noop});
 assert.match(html,/role="alert"/);
 assert.match(text(html),/Unsaved changes on this page could not be kept/);
 assert.match(html,/<button[^>]*>Reload the app<\/button>/);
});
