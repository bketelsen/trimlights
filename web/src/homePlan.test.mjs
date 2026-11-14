import test from 'node:test';
import assert from 'node:assert/strict';
import { favoriteShortcuts, planSummary } from './homePlan.ts';

const hex = (/** @type {string} */ byte) => byte.repeat(156);
const saved = (/** @type {string} */ id, /** @type {object} */ design, archived = false) =>
  /** @type {import('./pattern.ts').SavedPattern} */ ({
    id,
    revision: 1,
    actor: 'test',
    updated: '2026-11-13T00:00:00Z',
    archived,
    design: {
      schema: 1,
      name: id,
      favorite: true,
      effect: 'native',
      brightness: 100,
      speed: 0,
      reverse: false,
      palette: [],
      ...design,
    },
  });
const native = (/** @type {number} */ id, /** @type {string} */ parameters) => ({
  native: {
    id,
    name: `slot ${id}`,
    parameters_hex: parameters,
    snapshot_at: '2026-11-01T00:00:00Z',
  },
});
const snapshot = {
  patterns: [
    { id: 1, name: 'One', uninterpreted_parameters_hex: hex('0a') },
    { id: 2, name: 'Two', uninterpreted_parameters_hex: hex('0b') },
    { id: 3, name: 'Three' },
  ],
};

test('favoriteShortcuts offers a favorite whose slot still holds the imported parameters', () => {
  const library = [saved('Porch', native(1, hex('0A'))), saved('Party', native(2, hex('0b')))];
  assert.deepEqual(favoriteShortcuts(library, snapshot), [
    { slot: 1, name: 'Porch', saved: 'One' },
    { slot: 2, name: 'Party', saved: 'Two' },
  ]);
});

test('favoriteShortcuts leaves out what it cannot match to a saved slot', () => {
  const library = [
    saved('Not a favorite', { favorite: false, ...native(1, hex('0a')) }),
    saved('Archived', native(1, hex('0a')), true),
    saved('Designed in the app', { effect: 'static', palette: [{ color: '#ffffff', count: 1 }] }),
    saved('Slot changed', native(2, hex('0c'))),
    saved('Slot gone', native(9, hex('0a'))),
    saved('No parameters in the snapshot', native(3, hex('0a'))),
  ];
  assert.deepEqual(favoriteShortcuts(library, snapshot), []);
});

test('favoriteShortcuts needs a snapshot and lists a slot once', () => {
  const library = [saved('First', native(1, hex('0a'))), saved('Copy', native(1, hex('0a')))];
  assert.deepEqual(favoriteShortcuts(library, undefined), []);
  assert.deepEqual(favoriteShortcuts(library, snapshot), [
    { slot: 1, name: 'First', saved: 'One' },
  ]);
});

const candidate = (/** @type {string} */ name) => ({
  id: name,
  name,
  kind: 'weekly',
  priority: 0,
  action: { kind: 'off' },
});
const segment = (/** @type {object} */ values) => ({
  from: '2026-11-13T22:00:00Z',
  until: '2026-11-14T03:00:00Z',
  action: { kind: 'off' },
  conflict: false,
  reason: '',
  winners: [],
  shadowed: [],
  ...values,
});
const preview = (/** @type {object[]} */ segments) =>
  /** @type {import('./Schedule.tsx').Preview} */ (
    /** @type {unknown} */ ({
      execution_enabled: false,
      timezone: 'America/New_York',
      segments,
      issues: [],
    })
  );

test('planSummary is null when no routine plans anything', () => {
  assert.equal(planSummary(preview([]), []), null);
  assert.equal(planSummary(preview([segment({})]), []), null);
});

test('planSummary names the current step and the next one', () => {
  const library = [saved('Porch', native(1, hex('0a')))];
  const summary = planSummary(
    preview([
      segment({}),
      segment({
        from: '2026-11-14T03:00:00Z',
        until: '2026-11-14T05:00:00Z',
        action: { kind: 'pattern', pattern_id: 'Porch', revision: 1 },
        winners: [candidate('Evenings'), candidate('Weekend')],
      }),
      segment({ from: '2026-11-14T05:00:00Z' }),
    ]),
    library,
  );
  assert.deepEqual(summary, {
    now: {
      label: 'Lights off',
      detail: 'Between routines',
      from: '2026-11-13T22:00:00Z',
      until: '2026-11-14T03:00:00Z',
    },
    next: {
      label: 'Porch',
      detail: 'Evenings + Weekend',
      from: '2026-11-14T03:00:00Z',
      until: '2026-11-14T05:00:00Z',
    },
  });
});

test('planSummary reports a conflict, an unknown pattern and a week without change', () => {
  const summary = planSummary(
    preview([
      segment({
        conflict: true,
        action: { kind: 'pattern', pattern_id: 'gone', revision: 1 },
        winners: [candidate('A'), candidate('B')],
      }),
    ]),
    [],
  );
  assert.equal(summary?.now.label, 'Overlapping routines need a choice');
  assert.equal(summary?.next, undefined);
  const unknown = planSummary(
    preview([
      segment({
        action: { kind: 'pattern', pattern_id: 'gone', revision: 1 },
        winners: [candidate('A')],
      }),
    ]),
    [],
  );
  assert.equal(unknown?.now.label, 'Saved pattern');
});

test('planSummary skips a step that changes the routine but not the lights', () => {
  const library = [saved('Porch', native(1, hex('0a'))), saved('Party', native(2, hex('0b')))];
  const porch = { kind: 'pattern', pattern_id: 'Porch', revision: 1 };
  const summary = planSummary(
    preview([
      segment({ until: '2026-11-14T01:00:00Z', action: porch, winners: [candidate('Early')] }),
      // A second routine takes over with the same pattern: the lights do not change.
      segment({
        from: '2026-11-14T01:00:00Z',
        until: '2026-11-14T03:00:00Z',
        action: porch,
        winners: [candidate('Late')],
      }),
      segment({
        from: '2026-11-14T03:00:00Z',
        until: '2026-11-14T05:00:00Z',
        action: { kind: 'pattern', pattern_id: 'Party', revision: 1 },
        winners: [candidate('Night')],
      }),
    ]),
    library,
  );
  assert.equal(summary?.now.label, 'Porch');
  assert.equal(summary?.now.detail, 'Early');
  assert.equal(summary?.now.until, '2026-11-14T03:00:00Z');
  assert.equal(summary?.next?.label, 'Party');
  assert.equal(summary?.next?.from, '2026-11-14T03:00:00Z');
});

test('planSummary has no next step when only the routine changes', () => {
  const porch = { kind: 'pattern', pattern_id: 'Porch', revision: 1 };
  const summary = planSummary(
    preview([
      segment({ action: porch, winners: [candidate('Early')] }),
      segment({ from: '2026-11-14T03:00:00Z', action: porch, winners: [candidate('Late')] }),
    ]),
    [],
  );
  assert.equal(summary?.next, undefined);
});

test('planSummary treats a conflict and another revision as a change', () => {
  const porch = { kind: 'pattern', pattern_id: 'Porch', revision: 1 };
  const conflict = planSummary(
    preview([
      segment({}),
      segment({ from: '2026-11-14T03:00:00Z', conflict: true, winners: [candidate('A')] }),
    ]),
    [],
  );
  assert.equal(conflict?.next?.label, 'Overlapping routines need a choice');
  const revision = planSummary(
    preview([
      segment({ action: porch, winners: [candidate('A')] }),
      segment({
        from: '2026-11-14T03:00:00Z',
        action: { ...porch, revision: 2 },
        winners: [candidate('A')],
      }),
    ]),
    [],
  );
  assert.equal(revision?.next?.from, '2026-11-14T03:00:00Z');
});

test('favoriteShortcuts keeps the name saved on the lights beside the Library name', () => {
  const unnamed = { patterns: [{ id: 4, name: '', uninterpreted_parameters_hex: hex('0d') }] };
  assert.deepEqual(favoriteShortcuts([saved('Candy cane', native(4, hex('0d')))], unnamed), [
    { slot: 4, name: 'Candy cane', saved: 'Pattern 4' },
  ]);
});
