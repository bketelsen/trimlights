import test from 'node:test';
import assert from 'node:assert/strict';
import { mayDiscard, pageFromHash, pageHash, pageTitle, pages, routeStep } from './route.ts';

test('every page round-trips through its URL hash', () => {
  for (const page of pages) assert.equal(pageFromHash(pageHash(page)), page);
});
test('an empty or unknown hash opens Home', () => {
  for (const hash of ['', '#', '#unknown', '#Home', '#/house'])
    assert.equal(pageFromHash(hash), 'home');
});
test('each page has its own document title', () => {
  const titles = pages.map(pageTitle);
  assert.equal(new Set(titles).size, pages.length);
  assert.equal(pageTitle('house'), 'Trimlights · House');
  assert.equal(pageTitle(null), 'Trimlights');
});

// A small browser: history entries (pages) and a cursor. pushState drops the
// forward entries. A traversal or a typed hash fires popstate and then hashchange;
// each event reads the current entry, as location.hash does.
/** @typedef {import('./route.ts').Page} Page */
/** @typedef {{entries:Page[],cursor:number,shown:Page,answers:boolean[],asks:number,push:(next:Page)=>void,event:()=>void,fire:()=>void,traverse:(delta:number)=>void,type:(next:Page)=>void,click:(next:Page)=>void,refuse:()=>void}} Browser */
/** @param {Page} page */
function browser(page) {
  // The methods are added below.
  const b = /** @type {Browser} */ (
    /** @type {unknown} */ ({ entries: [page], cursor: 0, shown: page, answers: [], asks: 0 })
  );
  b.push = (next) => {
    b.entries.splice(b.cursor + 1, Infinity, next);
    b.cursor++;
  };
  b.event = () => {
    const step = routeStep(b.shown, b.entries[b.cursor], () => {
      b.asks++;
      return b.answers.shift() ?? true;
    });
    if (step.show) b.shown = step.show;
    if (step.keep) b.push(step.keep);
  };
  b.fire = () => {
    b.event();
    b.event();
  };
  b.traverse = (delta) => {
    b.cursor += delta;
    assert.ok(b.cursor >= 0 && b.cursor < b.entries.length);
    b.fire();
  };
  b.type = (next) => {
    b.push(next);
    b.fire();
  };
  b.click = (next) => {
    if (next === b.shown) return;
    b.push(next);
    b.shown = next;
  };
  b.refuse = () => {
    b.answers.push(false);
  };
  return b;
}
const at = (
  /** @type {Browser} */ b,
  /** @type {Page[]} */ entries,
  /** @type {number} */ cursor,
  /** @type {Page} */ shown,
) => {
  assert.deepEqual(b.entries, entries);
  assert.equal(b.cursor, cursor);
  assert.equal(b.shown, shown);
  assert.equal(b.entries[b.cursor], b.shown);
};

test('the same page needs no question and no change', () => {
  assert.deepEqual(
    routeStep('house', 'house', () => assert.fail('no question')),
    {},
  );
  assert.deepEqual(
    routeStep('house', 'home', () => true),
    { show: 'home' },
  );
  assert.deepEqual(
    routeStep('house', 'home', () => false),
    { keep: 'house' },
  );
});
test('a refused Back keeps every earlier entry reachable', () => {
  const b = browser('home');
  b.click('patterns');
  b.click('house');
  b.refuse();
  b.traverse(-1);
  assert.equal(b.asks, 1);
  at(b, ['home', 'patterns', 'house'], 2, 'house');
  // After the save, Back reaches Library and then Home.
  b.traverse(-1);
  at(b, ['home', 'patterns', 'house'], 1, 'patterns');
  b.traverse(-1);
  at(b, ['home', 'patterns', 'house'], 0, 'home');
  assert.equal(b.asks, 3);
});
test('the next navigation after a refusal continues from the kept entry', () => {
  const b = browser('home');
  b.click('patterns');
  b.click('house');
  b.refuse();
  b.traverse(-1);
  b.click('settings');
  at(b, ['home', 'patterns', 'house', 'settings'], 3, 'settings');
  b.traverse(-1);
  at(b, ['home', 'patterns', 'house', 'settings'], 2, 'house');
  b.traverse(-1);
  at(b, ['home', 'patterns', 'house', 'settings'], 1, 'patterns');
});
test('repeated refused Back never loses an entry', () => {
  const b = browser('home');
  b.click('patterns');
  b.click('house');
  b.refuse();
  b.traverse(-1);
  b.refuse();
  b.traverse(-1);
  at(b, ['home', 'patterns', 'house'], 2, 'house');
  assert.equal(b.asks, 2);
  b.traverse(-1);
  b.traverse(-1);
  at(b, ['home', 'patterns', 'house'], 0, 'home');
});
test('a refused typed hash keeps the page on screen', () => {
  const b = browser('home');
  b.click('house');
  b.refuse();
  b.type('patterns');
  assert.equal(b.asks, 1);
  at(b, ['home', 'house', 'patterns', 'house'], 3, 'house');
  b.traverse(-1);
  at(b, ['home', 'house', 'patterns', 'house'], 2, 'patterns');
});
test('a refused Forward keeps the page on screen', () => {
  const b = browser('home');
  b.click('patterns');
  b.click('house');
  b.traverse(-1);
  at(b, ['home', 'patterns', 'house'], 1, 'patterns');
  b.refuse();
  b.traverse(1);
  assert.equal(b.asks, 2);
  at(b, ['home', 'patterns', 'house', 'patterns'], 3, 'patterns');
  b.traverse(-1);
  at(b, ['home', 'patterns', 'house', 'patterns'], 2, 'house');
});
test('Reconnect asks only over unsaved edits and obeys the answer', () => {
  /** @type {boolean[]} */
  const asked = [];
  const ask = (/** @type {boolean} */ answer) => () => {
    asked.push(answer);
    return answer;
  };
  assert.equal(mayDiscard(false, ask(false)), true);
  assert.deepEqual(asked, []);
  assert.equal(mayDiscard(true, ask(false)), false);
  assert.equal(mayDiscard(true, ask(true)), true);
  assert.deepEqual(asked, [false, true]);
});
