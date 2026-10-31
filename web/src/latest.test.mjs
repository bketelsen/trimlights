import test from 'node:test';
import assert from 'node:assert/strict';
import { latestRequests } from './latest.ts';

const interval = 5000;
const stale = 60000;

// simulate runs the status poll of main.tsx on a fake clock. Every interval the poll
// starts a request unless one is busy; each request answers after latency(n) ms
// (Infinity: never). It returns the answers that applied, in order.
/** @param {(n: number) => number} latency @param {number} until */
function simulate(latency, until) {
  let clock = 0;
  const requests = latestRequests(() => clock);
  /** @type {{n: number, at: number, settle: () => boolean}[]} */
  const answers = [];
  /** @type {number[]} */
  const applied = [];
  let started = 0;
  for (let tick = 0; tick <= until; tick += 100) {
    clock = tick;
    for (const answer of answers.filter((a) => a.at <= clock)) {
      answers.splice(answers.indexOf(answer), 1);
      if (answer.settle()) applied.push(answer.n);
    }
    if (tick % interval === 0 && !requests.busy(stale)) {
      const n = started++;
      answers.push({ n, at: clock + latency(n), settle: requests.begin() });
    }
  }
  return { applied, started };
}

test('answers slower than the poll interval still apply', () => {
  // Every answer takes 7 s; the poll interval is 5 s.
  const { applied, started } = simulate(() => 7000, 60000);
  assert.ok(applied.length >= 5, `applied ${applied.length}`);
  assert.deepEqual(
    applied,
    applied.map((_, i) => i),
  );
  // The poll waits for each answer: no request starts while another runs.
  assert.equal(started, applied.length + 1);
});

test('a hung request stops blocking the poll after the stale limit', () => {
  const { applied } = simulate((n) => (n === 0 ? Infinity : 1000), 90000);
  assert.ok(applied.length > 0);
  assert.ok(!applied.includes(0));
});

test('a slow older answer cannot overwrite a newer answer', () => {
  const requests = latestRequests();
  const poll = requests.begin();
  const refresh = requests.begin();
  assert.equal(refresh(), true);
  assert.equal(poll(), false, 'the older poll answers after the refresh');
  const next = requests.begin();
  assert.equal(next(), true);
});

test('an older answer applies while a newer request still runs', () => {
  const requests = latestRequests();
  const first = requests.begin();
  const second = requests.begin();
  assert.equal(first(), true);
  assert.equal(second(), true);
});

test('busy counts running requests only, and guards are independent', () => {
  let clock = 0;
  const requests = latestRequests(() => clock);
  const other = latestRequests(() => clock);
  assert.equal(requests.busy(stale), false);
  const settle = requests.begin();
  assert.equal(requests.busy(stale), true);
  assert.equal(other.busy(stale), false);
  clock = stale;
  assert.equal(requests.busy(stale), false, 'a request older than the stale limit');
  clock = 0;
  settle();
  assert.equal(requests.busy(stale), false);
});
