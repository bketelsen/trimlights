import test from 'node:test';
import assert from 'node:assert/strict';
import {
  APIError,
  MissingCSRF,
  photoUploadMessage,
  request,
  SignedOut,
  userMessage,
} from './api.ts';

const fallback = 'Could not save. Please try again.';
// Each call answers with one response and records what the browser would send.
/** @param {number} status @param {string} body */
function serve(status, body, type = 'application/json') {
  /** @type {{path:unknown,init:any}[]} */
  const calls = [];
  globalThis.fetch = async (path, init) => {
    calls.push({ path, init });
    return new Response(body, { status, headers: { 'Content-Type': type } });
  };
  return calls;
}
async function failure(/** @type {Promise<unknown>} */ promise) {
  try {
    await promise;
  } catch (e) {
    return e;
  }
  assert.fail('request did not fail');
}

test('validation responses show the service message as a sentence', () => {
  assert.equal(
    userMessage(
      new APIError('run needs an ID, name, output and addresses within 1–2048', 400, true),
      fallback,
    ),
    'Run needs an ID, name, output and addresses within 1–2048.',
  );
  assert.equal(
    userMessage(new APIError('invalid timezone', 422, true), fallback),
    'Invalid timezone.',
  );
  assert.equal(
    userMessage(new APIError('Choose another name.', 400, true), fallback),
    'Choose another name.',
  );
  assert.equal(
    userMessage(new APIError('pattern or cached snapshot not found', 404, true), fallback),
    'Pattern or cached snapshot not found.',
  );
});
test('responses without service text keep the general messages', () => {
  assert.equal(
    userMessage(new APIError('Request failed (400)', 400), fallback),
    'Check your entries and try again.',
  );
  assert.equal(
    userMessage(new APIError('Request failed (422)', 422), fallback),
    'Check your entries and try again.',
  );
  assert.equal(
    userMessage(new APIError('  ', 400, true), fallback),
    'Check your entries and try again.',
  );
  assert.equal(
    userMessage(new APIError('Request failed (413)', 413), fallback),
    'This file is too large. Choose a smaller file.',
  );
  assert.equal(
    userMessage(new APIError('Request failed (429)', 429), fallback),
    'The app is busy. Wait a moment and try again.',
  );
  assert.equal(userMessage(new APIError('Request failed (502)', 502), fallback), fallback);
  assert.equal(userMessage(new APIError('database is locked', 500, true), fallback), fallback);
});
test('conflict, access, Sunday and native-copy messages stay fixed', () => {
  assert.match(
    userMessage(new APIError('house mapping changed on another device', 409, true), fallback),
    /^Someone else saved a change/,
  );
  assert.equal(
    userMessage(new APIError('Household access denied', 403, true), fallback),
    'Please sign in again to continue.',
  );
  assert.equal(
    userMessage(new APIError('controls are unavailable on Sunday', 400, true), fallback),
    'Light controls are unavailable on Sundays for now.',
  );
  assert.equal(
    userMessage(
      new APIError(
        'a native pattern can be created only as a copy of a matching slot in the latest controller snapshot; use import from the controller',
        400,
        true,
      ),
      fallback,
    ),
    'This saved-on-lights pattern no longer matches your lights, so it cannot be added as a copy.',
  );
});
test('request keeps JSON error text and ignores gateway pages', async () => {
  serve(400, JSON.stringify({ error: 'address ranges overlap on output output-1' }));
  const rejected = await failure(
    request('/api/v1/house', 'token', {}, { method: 'PUT', revision: 3 }),
  );
  assert.ok(rejected instanceof APIError);
  assert.equal(rejected.status, 400);
  assert.equal(rejected.fromServer, true);
  assert.equal(userMessage(rejected, fallback), 'Address ranges overlap on output output-1.');
  serve(502, '<html><body>Bad Gateway</body></html>', 'text/html');
  const gateway = await failure(request('/api/v1/house'));
  assert.ok(gateway instanceof APIError);
  assert.equal(gateway.fromServer, false);
  assert.equal(userMessage(gateway, fallback), fallback);
  serve(401, JSON.stringify({ error: 'sign_in_required' }));
  assert.ok((await failure(request('/api/v1/house'))) instanceof SignedOut);
});
test('raw request bodies keep their content type and CSRF token', async () => {
  const photo = new Blob([new Uint8Array([0xff, 0xd8, 0xff])], { type: 'image/jpeg' });
  const calls = serve(201, JSON.stringify({ id: 'p1' }));
  assert.deepEqual(await request('/api/v1/photos', 'token', undefined, { raw: photo }), {
    id: 'p1',
  });
  assert.equal(calls[0].init.method, 'POST');
  assert.equal(calls[0].init.body, photo);
  assert.deepEqual(calls[0].init.headers, {
    'Content-Type': 'image/jpeg',
    'X-CSRF-Token': 'token',
  });
  const untyped = serve(201, JSON.stringify({ id: 'p2' }));
  await request('/api/v1/photos', 'token', undefined, { raw: new Blob([new Uint8Array([1])]) });
  assert.equal(untyped[0].init.headers['Content-Type'], 'application/octet-stream');
});
test('photo upload failures explain the cause', async () => {
  const photo = new Blob([new Uint8Array([1])], { type: 'image/png' });
  serve(413, '<html><body>Request Entity Too Large</body></html>', 'text/html');
  assert.equal(
    photoUploadMessage(
      await failure(request('/api/v1/photos', 'token', undefined, { raw: photo })),
    ),
    'This file is too large. Choose a smaller file.',
  );
  serve(429, JSON.stringify({ error: 'another photo is processing; retry in a moment' }));
  assert.equal(
    photoUploadMessage(
      await failure(request('/api/v1/photos', 'token', undefined, { raw: photo })),
    ),
    'Another photo is processing; retry in a moment.',
  );
  serve(400, JSON.stringify({ error: 'photo is damaged or incomplete' }));
  assert.equal(
    photoUploadMessage(
      await failure(request('/api/v1/photos', 'token', undefined, { raw: photo })),
    ),
    'Photo is damaged or incomplete.',
  );
  serve(409, JSON.stringify({ error: 'household photo limit reached (20)' }));
  assert.equal(
    photoUploadMessage(
      await failure(request('/api/v1/photos', 'token', undefined, { raw: photo })),
    ),
    'This house already has 20 photos, the most it can keep.',
  );
  serve(502, '<html><body>Bad Gateway</body></html>', 'text/html');
  assert.equal(
    photoUploadMessage(
      await failure(request('/api/v1/photos', 'token', undefined, { raw: photo })),
    ),
    'Could not upload this photo. Check your connection and try again.',
  );
  assert.equal(
    photoUploadMessage(new TypeError('Failed to fetch')),
    'Could not upload this photo. Check your connection and try again.',
  );
  assert.equal(
    photoUploadMessage(new Error('Photo exceeds 12 MiB. Export a smaller JPEG.')),
    'Photo exceeds 12 MiB. Export a smaller JPEG.',
  );
});
test('request picks an explicit method and sends a body only with a mutation', async () => {
  const calls = serve(200, JSON.stringify({ ok: true }));
  await request('/api/v1/status');
  await request('/api/v1/logout', 'token');
  await request('/api/v1/house', 'token', { a: 1 }, { method: 'put', revision: 2 });
  await request('/api/v1/house/revisions', 'token', undefined, { method: 'GET' });
  assert.deepEqual(
    calls.map((c) => c.init.method),
    ['GET', 'POST', 'PUT', 'GET'],
  );
  assert.equal(calls[0].init.body, undefined);
  assert.deepEqual(calls[0].init.headers, {});
  assert.equal(calls[1].init.body, '{}');
  assert.deepEqual(calls[2].init.headers, {
    'Content-Type': 'application/json',
    'X-CSRF-Token': 'token',
    'If-Match': '"2"',
  });
  assert.equal(calls[2].init.body, '{"a":1}');
  // An explicit GET never sends the token or a body.
  assert.equal(calls[3].init.body, undefined);
  assert.deepEqual(calls[3].init.headers, {});
});
test('a mutation without a CSRF token throws and sends nothing', async () => {
  const calls = serve(200, JSON.stringify({ ok: true }));
  const photo = new Blob([new Uint8Array([1])], { type: 'image/png' });
  for (const args of [
    ['/api/v1/logout', ''],
    ['/api/v1/patterns', undefined, { name: 'Porch' }],
    ['/api/v1/photos', '', undefined, { raw: photo }],
    ['/api/v1/house', undefined, undefined, { method: 'PUT' }],
    ['/api/v1/patterns/p1', '', {}, { method: 'DELETE' }],
  ]) {
    const rejected = await failure(request(...args));
    assert.ok(rejected instanceof MissingCSRF, args[0]);
    assert.equal(
      userMessage(rejected, fallback),
      'Could not send this change. Reload the app and try again.',
    );
  }
  assert.equal(calls.length, 0);
});
