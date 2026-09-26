// Smoke tests of the built dist/: every reference resolves, hashed names
// match their content, headers are safe to cache, and the WebAssembly engine
// loads in Node and returns firings.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { dist, asset, bootEngine, compute } from './helpers.mjs';
import { parseHeaders, headersFor } from '../scripts/serve.mjs';
import { PRESETS } from '../web/presets.js';

test('index.html references only files that exist', () => {
  const html = readFileSync(join(dist, 'index.html'), 'utf8');
  const refs = [...html.matchAll(/(?:href|src)="([^"#:]+)"/g)].map((m) => m[1]);
  assert.ok(refs.length >= 4, 'expected stylesheet, script, font and icon references');
  for (const r of refs) assert.ok(existsSync(join(dist, r)), `missing ${r}`);
  assert.match(html, /a\/app\.[0-9a-f]{10}\.js/);
  assert.match(html, /a\/styles\.[0-9a-f]{10}\.css/);
});

test('every hashed asset is named after its own content', () => {
  const files = readdirSync(join(dist, 'a'));
  assert.ok(files.length >= 10);
  for (const f of files) {
    const m = /\.([0-9a-f]{10})\.[a-z0-9]+$/.exec(f);
    assert.ok(m, `${f} has no content hash`);
    const sum = createHash('sha256').update(readFileSync(join(dist, 'a', f))).digest('hex').slice(0, 10);
    assert.equal(m[1], sum, `${f} hash does not match its bytes`);
  }
});

test('JS, CSS and the worker point at hashed files that exist', () => {
  const dir = join(dist, 'a');
  for (const f of readdirSync(dir).filter((x) => /\.(js|css)$/.test(x))) {
    const text = readFileSync(join(dir, f), 'utf8');
    for (const m of text.matchAll(/['"(]\.?\/?([a-z0-9_-]+\.[0-9a-f]{10}\.(?:js|css|wasm|woff2))['")]/g)) {
      assert.ok(existsSync(join(dir, m[1])), `${f} refers to missing ${m[1]}`);
    }
  }
  const worker = readFileSync(asset('engine-worker', '.js'), 'utf8');
  assert.match(worker, /tide\.[0-9a-f]{10}\.wasm/);
  assert.match(worker, /wasm_exec\.[0-9a-f]{10}\.js/);
  assert.ok(!worker.includes('__WASM_BYTES__'), 'the build fills in the engine size');
  const size = readFileSync(asset('tide', '.wasm')).length;
  assert.ok(worker.includes(`Number('${size}')`), 'worker knows the real engine size for its progress bar');
});

test('headers: strict CSP, long caching only for hashed files', () => {
  const rules = parseHeaders(readFileSync(join(dist, '_headers'), 'utf8'));
  const page = headersFor(rules, '/index.html');
  const csp = page['content-security-policy'];
  assert.ok(csp, 'a Content-Security-Policy is set');
  assert.match(csp, /script-src 'self' 'wasm-unsafe-eval'/);
  assert.ok(!csp.includes('unsafe-inline'), 'no unsafe-inline');
  assert.ok(!/'unsafe-eval'/.test(csp), 'no unsafe-eval');
  assert.equal(page['cache-control'], undefined, 'index.html is not given a max-age');
  assert.match(headersFor(rules, '/a/tide.0123456789.wasm')['cache-control'], /max-age=31536000, immutable/);
  for (const r of rules) {
    if (/max-age=[1-9]|immutable/.test(r.headers['cache-control'] ?? '')) {
      assert.equal(r.pattern, '/a/*', `long caching on ${r.pattern}, which is not hashed`);
    }
  }
});

test('the WebAssembly engine loads in Node and reports its versions', async () => {
  const T = await bootEngine();
  const info = JSON.parse(T.info);
  assert.match(info.go, /^go1\.\d+/);
  assert.equal(info.cron, 'github.com/robfig/cron/v3 v3.0.1');
  assert.match(info.tzdata, /^20\d\d[a-z]$/);
  const zones = JSON.parse(T.zones);
  for (const z of ['UTC', 'Australia/Sydney', 'America/New_York', 'Australia/Lord_Howe']) assert.ok(zones.includes(z), z);
});

test('the engine returns firings and DST seams', async () => {
  const T = await bootEngine();
  const r = compute(T, '30 2 * * *', ['Australia/Sydney', 'Europe/London'], 'shared', '2026-09');
  assert.equal(r.error, undefined);
  assert.equal(r.description, 'At 02:30, every day');
  const [syd, ldn] = r.views;
  assert.equal(syd.days, 365);
  assert.equal(syd.total, 365); // one skipped in October, one repeated in April
  assert.deepEqual(
    syd.seams.filter((s) => s.red).map((s) => [s.date, s.kind]),
    [
      ['Sun 4 Oct 2026', 'skip'],
      ['Sun 4 Apr 2027', 'double'],
    ],
  );
  assert.equal(syd.next[0].time, '02:30');
  assert.equal(ldn.runZone, 'Australia/Sydney');
  assert.ok(ldn.seams.some((s) => s.kind === 'shift' && s.date === 'Sun 25 Oct 2026'));
  assert.equal(syd.bins.length, 365);
  assert.equal(syd.counts.reduce((a, b) => a + b, 0), 365);
});

test('the engine reports bad input with the field at fault', async () => {
  const T = await bootEngine();
  let r = compute(T, '0 24 * * *', ['UTC']);
  assert.equal(r.error.kind, 'expr');
  assert.equal(r.error.field, 1);
  assert.match(r.error.message, /Hours run 0–23/);
  r = compute(T, '0 9 * * *', ['UTC', 'Mars/Base']);
  assert.equal(r.error.kind, 'zone');
  assert.equal(r.error.index, 1);
  r = compute(T, '0 9 * * *', ['A/B', 'C/D', 'E/F', 'G/H', 'I/J']);
  assert.match(r.error.message, /At most 3/);
  r = compute(T, '0 9 * * *', ['UTC'], 'shared', '1900-01');
  assert.equal(r.error.kind, 'from');
  // Arguments of the wrong type are treated as empty, not as a crash.
  r = JSON.parse(T.compute(42, null, undefined, {}, 'x'));
  assert.equal(r.error.kind, 'expr');
});

test('every preset computes without an error', async () => {
  const T = await bootEngine();
  for (const p of PRESETS) {
    const r = compute(T, p.expr, p.zones, p.mode, p.from ?? '');
    assert.equal(r.error, undefined, `${p.id}: ${r.error?.message}`);
    assert.equal(r.views.length, p.zones.length, p.id);
  }
});

test('the heaviest request stays within the caps', async () => {
  const T = await bootEngine();
  const r = compute(T, '* * * * *', ['America/New_York', 'Europe/London', 'Australia/Sydney'], 'each', '2026-09');
  assert.equal(r.error, undefined);
  for (const v of r.views) {
    assert.equal(v.total, 365 * 1440);
    assert.equal(v.dial.length, 1440);
  }
  assert.ok(r.walked <= 3 * 368 * 1440, `walked ${r.walked}`);
});
