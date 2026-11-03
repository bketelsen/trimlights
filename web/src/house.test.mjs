import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
  afterPhotoDeletion,
  canvasTouchAction,
  missingPhotoViews,
  positions,
  rotateView,
  sourceRun,
  usesPhoto,
  withAdjustment,
  withoutMissingPhotos,
  withoutPhotoViews,
  withoutPhotos,
} from './house.ts';
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
test('views of deleted photos are found and left out without changing the layout', () => {
  const kept = { photo_id: 'kept', turns: 0, mappings: [] };
  const gone = { photo_id: 'gone', turns: 1, mappings: [] };
  const layout = { schema: 1, runs: [], views: [kept, gone] };
  const photo = { id: 'kept', name: 'House photo', width: 80, height: 40, created: '' };
  assert.equal(usesPhoto(layout, 'gone'), true);
  assert.equal(usesPhoto(layout, 'other'), false);
  assert.deepEqual(missingPhotoViews(layout, [photo]), ['gone']);
  const next = withoutMissingPhotos(layout, [photo]);
  assert.deepEqual(next.layout.views, [kept]);
  assert.equal(next.removed, 1);
  // The input keeps both views.
  assert.equal(layout.views.length, 2);
});
test('a photo list without the deleted photos keeps its order and its input', () => {
  const photos = [
    { id: 'a', name: 'House photo', width: 80, height: 40, created: '' },
    { id: 'b', name: 'House photo', width: 80, height: 40, created: '' },
    { id: 'c', name: 'House photo', width: 80, height: 40, created: '' },
  ];
  assert.deepEqual(withoutPhotos(photos, new Set(['b'])), [photos[0], photos[2]]);
  assert.deepEqual(withoutPhotos(photos, new Set()), photos);
  assert.notEqual(withoutPhotos(photos, new Set()), photos);
  assert.equal(photos.length, 3);
});
test('removing the views of one photo keeps the runs and its input', () => {
  const run = { id: 'front', name: 'Front', output: 'output-1', start: 1, count: 5 };
  const views = [
    { photo_id: 'a', turns: 0, mappings: [] },
    { photo_id: 'b', turns: 0, mappings: [] },
  ];
  const layout = { schema: 1, runs: [run], views };
  const next = withoutPhotoViews(layout, 'a');
  assert.deepEqual(next.views, [views[1]]);
  assert.deepEqual(next.runs, [run]);
  // The input keeps both views.
  assert.equal(layout.views.length, 2);
});
test('afterPhotoDeletion keeps the newest selection and the input list', () => {
  const a = { id: 'a', name: 'House photo', width: 80, height: 40, created: '' };
  const b = { id: 'b', name: 'House photo', width: 80, height: 40, created: '' };
  const c = { id: 'c', name: 'House photo', width: 80, height: 40, created: '' };
  const photos = [a, b, c];
  // A poll dropped b while the delete waited, so deleting a selects c.
  assert.deepEqual(afterPhotoDeletion([a, c], 'a', 'a'), { photos: [c], selected: 'c' });
  // Deleting an unselected photo keeps the selection.
  assert.deepEqual(afterPhotoDeletion(photos, 'a', 'b'), { photos: [a, c], selected: 'a' });
  // A selection that is already gone moves to the first remaining photo.
  assert.deepEqual(afterPhotoDeletion(photos, 'gone', 'b'), { photos: [a, c], selected: 'a' });
  // The last photo leaves no selection.
  assert.deepEqual(afterPhotoDeletion([a], 'a', 'a'), { photos: [], selected: '' });
  // The input list is unchanged.
  assert.deepEqual(photos, [a, b, c]);
});
