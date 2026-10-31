import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { canvasTouchAction, positions, rotateView, sourceRun, withAdjustment } from './house.ts';
for (const v of JSON.parse(
  readFileSync(new URL('./house-vectors.json', import.meta.url), 'utf8'),
)) {
  test(v.name, () =>
    assert.deepEqual(positions(v.mapping, v.count, v.width, v.height), v.expected),
  );
}
test('four quarter turns preserve bulb order and coordinates', () => {
  const view = {
    photo_id: 'a',
    turns: 0,
    mappings: [
      {
        run_id: 'a',
        points: [
          { x: 0, y: 0 },
          { x: 1, y: 1 },
        ],
        reverse: true,
        adjustments: [{ index: 1, point: { x: 0.5, y: 0.25 } }],
      },
    ],
  };
  let turned = view;
  for (let i = 0; i < 4; i++) turned = rotateView(turned);
  assert.deepEqual(turned, view);
});
test('mirrored output shares source addresses', () => {
  const source = /** @type {import('./house.ts').Run} */ ({ id: 'front', start: 12, count: 5 });
  const mirror = /** @type {import('./house.ts').Run} */ ({
    id: 'side',
    start: 1,
    count: 5,
    mirror_of: 'front',
  });
  assert.equal(sourceRun(mirror, [source, mirror]), source);
});
test('only drawing, dragging or a zoomed photo blocks page scrolling', () => {
  assert.equal(canvasTouchAction('pan', 1), 'pan-y');
  assert.equal(canvasTouchAction('pan', 1.5), 'none');
  for (const mode of /** @type {const} */ (['draw', 'points', 'bulbs']))
    assert.equal(canvasTouchAction(mode, 1), 'none');
});
test('a bulb adjustment replaces, clamps and resets one index', () => {
  const start = [
    { index: 2, point: { x: 0.1, y: 0.2 } },
    { index: 4, point: { x: 0.3, y: 0.4 } },
  ];
  assert.deepEqual(withAdjustment(start, 2, { x: 1.5, y: -1 }), [
    { index: 4, point: { x: 0.3, y: 0.4 } },
    { index: 2, point: { x: 1, y: 0 } },
  ]);
  assert.deepEqual(withAdjustment(start, 4), [{ index: 2, point: { x: 0.1, y: 0.2 } }]);
  assert.equal(start.length, 2);
  const mapping = {
    run_id: 'a',
    points: [
      { x: 0, y: 0 },
      { x: 1, y: 0 },
    ],
    reverse: false,
    adjustments: withAdjustment([], 1, { x: 0.5, y: 0.5 }),
  };
  assert.deepEqual(positions(mapping, 3, 100, 100)[1], { x: 0.5, y: 0.5 });
});
