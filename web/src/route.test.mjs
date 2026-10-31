import test from 'node:test';
import assert from 'node:assert/strict';
import {pageFromHash,pageHash,pageTitle,pages} from './route.ts';

test('every page round-trips through its URL hash',()=>{for(const page of pages)assert.equal(pageFromHash(pageHash(page)),page);});
test('an empty or unknown hash opens Home',()=>{for(const hash of ['','#','#unknown','#Home','#/house'])assert.equal(pageFromHash(hash),'home');});
test('each page has its own document title',()=>{
 const titles=pages.map(pageTitle);
 assert.equal(new Set(titles).size,pages.length);
 assert.equal(pageTitle('house'),'Trimlights · House');
 assert.equal(pageTitle(null),'Trimlights');
});
