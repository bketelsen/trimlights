import test from 'node:test';
import assert from 'node:assert/strict';
import { appendNativeRules, repeatChange } from './calendarDraft.ts';
/** @typedef {import('./Schedule.tsx').Plan} Plan */
/** @typedef {import('./Schedule.tsx').Rule} Rule */
// Fixtures are deliberately minimal; the casts keep them minimal under checkJs.
const original = /** @type {Plan} */ ({
  schema: 1,
  timezone: 'UTC',
  location: { latitude: 45, longitude: 10 },
  rules: [{ id: 'existing', name: 'Kept' }],
  override: {
    id: 'override',
    action: { kind: 'off' },
    starts_at: '2026-10-28T20:00:00Z',
    expires_at: '2026-10-28T21:00:00Z',
  },
});
const incoming = /** @type {Rule[]} */ ([
  { id: 'native-timer-1', name: 'Timer 1', enabled: true },
  { id: 'native-timer-2', name: 'Timer 2', enabled: false },
]);
test('append keeps unsaved rules, location and original override expiry', () => {
  const current = structuredClone(original);
  const additions = structuredClone(incoming);
  const result = appendNativeRules(current, structuredClone(current), additions);
  assert.deepEqual(result, { ...original, rules: [...original.rules, ...incoming] });
  assert.deepEqual(current, original);
  additions[0].name = 'Changed after adding';
  assert.equal(result.rules[1].name, 'Timer 1');
});
test('a delayed proposal cannot overwrite newer draft changes', () => {
  for (const change of /** @type {Partial<Plan>[]} */ ([
    { timezone: 'Europe/London' },
    { location: { latitude: 0, longitude: 0 } },
    { override: undefined },
    { rules: [{ id: 'newer' }] },
  ])) {
    const current = { ...structuredClone(original), ...change };
    const before = structuredClone(current);
    assert.throws(() => appendNativeRules(current, original, incoming), /draft changed/);
    assert.deepEqual(current, before);
  }
});
test('repeat submission and duplicate IDs preserve original rules', () => {
  const first = appendNativeRules(original, original, incoming);
  assert.throws(() => appendNativeRules(first, first, incoming), /already/);
  assert.throws(() => appendNativeRules(original, original, [incoming[0], incoming[0]]), /already/);
  assert.throws(
    () => appendNativeRules(original, original, /** @type {Rule[]} */ ([{ id: 'override' }])),
    /already/,
  );
  const full = {
    ...original,
    rules: /** @type {Rule[]} */ (Array.from({ length: 99 }, (_, i) => ({ id: `rule-${i}` }))),
  };
  assert.throws(() => appendNativeRules(full, full, incoming), /100-rule/);
  assert.equal(appendNativeRules(full, full, [incoming[0]]).rules.length, 100);
});
test('changing Repeat keeps the selected weekdays', () => {
  const weekly = { id: 'r', kind: 'weekly', weekdays: [1, 3, 5] };
  const seasonal = repeatChange(weekly, 'seasonal', '2026-11-01');
  assert.deepEqual(seasonal, {
    kind: 'seasonal',
    from: '2026-11-01',
    through: '2026-11-01',
    weekdays: [1, 3, 5],
  });
  assert.deepEqual(
    repeatChange({ ...weekly, ...seasonal, through: '2026-12-31' }, 'weekly', '2026-11-02'),
    { kind: 'weekly', from: undefined, through: undefined, weekdays: [1, 3, 5] },
  );
  // An empty selection stays empty, so the form still asks for a day instead of choosing all.
  assert.deepEqual(
    repeatChange({ ...weekly, weekdays: [] }, 'seasonal', '2026-11-01').weekdays,
    [],
  );
  assert.notEqual(repeatChange(weekly, 'seasonal', '2026-11-01').weekdays, weekly.weekdays);
});
test('a special date has no weekdays and gives the earlier selection back', () => {
  const weekly = { id: 'r', kind: 'weekly', from: '2026-11-05', weekdays: [0, 6] };
  const special = repeatChange(weekly, 'exception', '2026-11-01');
  assert.deepEqual(special, {
    kind: 'exception',
    from: '2026-11-05',
    through: '2026-11-05',
    weekdays: [],
  });
  assert.deepEqual(
    repeatChange({ ...weekly, ...special }, 'weekly', '2026-11-01', [0, 6]).weekdays,
    [0, 6],
  );
  // With nothing remembered (for example a loaded special date), every day is selected.
  assert.deepEqual(
    repeatChange({ ...weekly, ...special }, 'seasonal', '2026-11-01').weekdays,
    [0, 1, 2, 3, 4, 5, 6],
  );
  assert.deepEqual(
    repeatChange({ ...weekly, ...special }, 'seasonal', '2026-11-01', []).weekdays,
    [0, 1, 2, 3, 4, 5, 6],
  );
});
