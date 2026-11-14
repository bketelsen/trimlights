// Smoke tests: render components to static HTML on the server, as React does
// before any effect runs. scripts/build-component-tests.mjs compiles them first.
import test from 'node:test';
import assert from 'node:assert/strict';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import native from '../.test-build/NativeControls.js';
import preview from '../.test-build/HousePreview.js';
import boundary from '../.test-build/ErrorBoundary.js';
import homePlan from '../.test-build/HomePlan.js';

const { NativeControls } = native,
  { HousePreview, HousePreviewBody } = preview,
  { ErrorFallback } = boundary,
  { HomePlanCard } = homePlan;
const noop = () => {};
const props = {
  available: true,
  busy: false,
  csrf: 'token',
  refreshVersion: 0,
  onBusy: noop,
  onComplete: async () => {},
  onError: noop,
};
const snapshot = {
  queried_at: '2026-10-30T20:00:00Z',
  stable_sha256: '0'.repeat(64),
  patterns: [
    { id: 1, name: 'Warm white' },
    { id: 2, name: '' },
  ],
  playback: { mode: 'manual', matching_pattern_ids: [1] },
};
const render = (component, values) => renderToStaticMarkup(createElement(component, values));
const tags = (html, name) => html.match(new RegExp(`<${name}\\b[^>]*>`, 'g')) ?? [];
const text = (html) => html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ');

test('NativeControls renders a labelled region with the snapshot patterns', () => {
  const html = render(NativeControls, { ...props, snapshot });
  assert.match(html, /<section[^>]*aria-labelledby="native-controls-title"/);
  assert.match(html, /<h2 id="native-controls-title">Your lights<\/h2>/);
  assert.match(text(html), /Playing a pattern/);
  assert.match(html, /class="now-playing">Warm white</);
  // An unnamed pattern gets a readable fallback name.
  assert.deepEqual(tags(html, 'option').length, 3);
  assert.match(html, /<option value="2">Pattern 2<\/option>/);
  for (const label of ['Play pattern', 'Turn off', 'Follow timers'])
    assert.match(html, new RegExp(`<button[^>]*>${label}</button>`));
  assert.doesNotMatch(text(html), /Refresh your lights to get started/);
  assert.doesNotMatch(html, /role="alert"/);
});

test('NativeControls keeps every control action disabled until the phone app is acknowledged', () => {
  const html = render(NativeControls, { ...props, snapshot });
  const buttons = tags(html, 'button');
  assert.equal(buttons.length, 3);
  for (const button of buttons) assert.match(button, /\bdisabled=""/);
  const checkbox = tags(html, 'input').find((input) => /type="checkbox"/.test(input));
  assert.ok(checkbox);
  assert.doesNotMatch(checkbox, /checked/);
});

test('NativeControls before the first status shows a neutral checking state', () => {
  const html = render(NativeControls, { ...props, available: false, checking: true });
  assert.match(text(html), /Checking your lights…/);
  assert.match(text(html), /Checking…/);
  assert.doesNotMatch(text(html), /Refresh your lights to get started|Refresh to check/);
  assert.doesNotMatch(html, /role="alert"/);
});

test('NativeControls without a snapshot asks for a refresh and reports busy work', () => {
  const idle = render(NativeControls, { ...props, available: false });
  assert.match(text(idle), /Refresh to check/);
  assert.match(text(idle), /Refresh your lights to get started/);
  assert.equal(tags(idle, 'option').length, 1);
  const busy = render(NativeControls, { ...props, available: false, busy: true });
  assert.match(busy, /role="status"[^>]*>Updating lights…</);
  assert.match(tags(busy, 'select')[0], /disabled=""/);
});

test('HousePreview renders a closed disclosure with only its summary', () => {
  const html = render(HousePreview, { patternID: 'p1', revision: 3 });
  assert.match(
    html,
    /^<details class="house-preview"><summary>Preview on your house<\/summary><\/details>$/,
  );
});

test('HousePreviewBody shows an error alone, without the loading text', () => {
  const base = { error: '', photos: [], photoID: '', onPhoto: noop };
  const failed = render(HousePreviewBody, {
    ...base,
    error: 'Could not load your house. Please try again.',
  });
  assert.equal(failed, '<p role="alert">Could not load your house. Please try again.</p>');
  assert.equal(render(HousePreviewBody, base), '<p>Loading your house…</p>');
  // A pinned pattern that is unavailable fails after the house loaded: the error still stands alone.
  const house = { revision: 1, layout: { schema: 1, runs: [], views: [] } };
  assert.doesNotMatch(
    render(HousePreviewBody, { ...base, house, error: 'This saved pattern is unavailable' }),
    /Loading|Add a photo/,
  );
  assert.match(render(HousePreviewBody, { ...base, house }), /Add a photo and trace your roofline/);
});

test('ErrorFallback says that unsaved changes are lost and reloads only on request', () => {
  const html = render(ErrorFallback, { onReload: noop });
  assert.match(html, /role="alert"/);
  assert.match(text(html), /Unsaved changes on this page could not be kept/);
  assert.match(html, /<button[^>]*>Reload the app<\/button>/);
});

test('NativeControls offers favorites as choices that send nothing by themselves', () => {
  const html = render(NativeControls, {
    ...props,
    snapshot,
    favorites: [
      { slot: 1, name: 'Porch', saved: 'Warm white' },
      { slot: 2, name: 'Pattern 2', saved: 'Pattern 2' },
    ],
  });
  assert.match(html, /<div class="favorite-row" role="group" aria-label="Favorites">/);
  const favorite = tags(html, 'button').find((button) => /favorite-choice/.test(button));
  assert.ok(favorite);
  assert.match(favorite, /aria-pressed="false"/);
  // A favorite only fills in the choice, so it does not wait for the acknowledgement.
  assert.doesNotMatch(favorite, /\bdisabled=""/);
  // A favorite renamed in the Library also shows the name that the lights hold, which
  // the pattern list and the "Playing …" notice use. Equal names show once.
  assert.match(text(html), /★ Porch “Warm white” on the lights/);
  assert.match(text(html), /★ Pattern 2 (?!“)/);
  assert.equal((html.match(/favorite-saved/g) ?? []).length, 1);
  const none = render(NativeControls, { ...props, snapshot });
  assert.doesNotMatch(none, /favorite-row/);
});

const step = {
  label: 'Lights off',
  detail: 'Between routines',
  from: '2026-11-13T22:00:00Z',
  until: '2026-11-14T03:00:00Z',
};
const planData = { state: 'ready', patterns: [], summary: null, timezone: 'America/New_York' };

test('HomePlanCard always says that the plan is not what the lights are doing', () => {
  for (const state of ['loading', 'failed', 'ready']) {
    const html = render(HomePlanCard, { data: { ...planData, state }, onOpenCalendar: noop });
    assert.match(html, /<section[^>]*aria-labelledby="home-plan-title"/);
    assert.match(text(html), /Plan only/);
    assert.match(text(html), /not what the lights are doing now/);
    assert.match(html, /<button[^>]*>Open calendar<\/button>/);
  }
});

test('HomePlanCard shows loading, failure and an empty calendar in words', () => {
  const show = (/** @type {string} */ state) =>
    text(render(HomePlanCard, { data: { ...planData, state }, onOpenCalendar: noop }));
  assert.match(show('loading'), /Loading your plan…/);
  assert.match(show('failed'), /couldn’t load your calendar plan/);
  assert.match(show('ready'), /Nothing is planned for the next 7 days/);
});

test('HomePlanCard shows the current step and the next change', () => {
  const next = {
    label: 'Porch',
    detail: 'Evenings',
    from: '2026-11-14T03:00:00Z',
    until: '2026-11-14T05:00:00Z',
  };
  const html = render(HomePlanCard, {
    data: { ...planData, summary: { now: step, next } },
    onOpenCalendar: noop,
  });
  assert.match(text(html), /Now Lights off Between routines · until /);
  assert.match(html, /<time dateTime="2026-11-14T03:00:00Z">/i);
  assert.match(text(html), /Porch Evenings/);
  const alone = text(
    render(HomePlanCard, { data: { ...planData, summary: { now: step } }, onOpenCalendar: noop }),
  );
  assert.match(alone, /No change is planned in the next 7 days/);
  assert.doesNotMatch(alone, /· until/);
});
