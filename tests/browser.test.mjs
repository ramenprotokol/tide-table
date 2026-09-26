// End-to-end check in real headless Chrome, against dist/ served with the
// production headers (CSP included). It needs a local Chrome: without one it
// skips, unless REQUIRE_BROWSER=1, when it fails.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { join } from 'node:path';
import { dist } from './helpers.mjs';
import { serve } from '../scripts/serve.mjs';
import { findChrome, browserPlan, launchChrome } from './cdp.mjs';

const chromePath = findChrome();
const plan = browserPlan(chromePath);

// Ignore nothing: the page makes no third-party requests, so any console
// error, exception, failed request or CSP report is a failure.
const problemsOf = (page) => page.problems.filter((p) => !/favicon/.test(p.text ?? ''));

async function withPage(chrome, base, opts, hash, fn) {
  const page = await chrome.openPage(opts);
  try {
    await page.navigate(`${base}/${hash ?? ''}`);
    await page.waitFor(`document.documentElement.dataset.state === 'ready'`, 30000);
    await fn(page);
    assert.deepEqual(problemsOf(page), [], `console or network problems at ${opts.width}px ${opts.scheme ?? ''}`);
  } finally {
    await page.close();
  }
}

const setValue = (id, value) =>
  `(() => { const el = document.getElementById(${JSON.stringify(id)}); el.value = ${JSON.stringify(value)}; el.dispatchEvent(new Event('input', { bubbles: true })); el.dispatchEvent(new Event('change', { bubbles: true })); return true; })()`;

test('the almanac works in a real browser', { skip: plan.skip, timeout: 180000 }, async (t) => {
  if (plan.fail) assert.fail(plan.fail);
  const server = await serve(dist, 0);
  const base = `http://127.0.0.1:${server.address().port}`;
  const chrome = await launchChrome(chromePath);
  try {
    await t.test('desktop: default preset renders three plates with red seams', () =>
      withPage(chrome, base, { width: 1280, height: 800, scheme: 'light' }, '', async (page) => {
        const r = await page.evaluate(`({
          plates: document.querySelectorAll('.plate').length,
          strips: document.querySelectorAll('svg.strip').length,
          dials: document.querySelectorAll('svg.dial').length,
          red: document.querySelectorAll('svg.strip .seam.red').length,
          sentence: document.getElementById('sentence').textContent,
          summary: document.getElementById('summary').textContent,
          engine: document.getElementById('engine-info').textContent,
          next: document.querySelectorAll('.plate .next tbody tr').length,
          scroll: document.documentElement.scrollWidth <= innerWidth,
        })`);
        assert.equal(r.plates, 3);
        assert.equal(r.strips, 3);
        assert.equal(r.dials, 3);
        assert.ok(r.red >= 6, `red seams drawn: ${r.red}`);
        assert.match(r.sentence, /At 02:30, every day/);
        assert.match(r.summary, /walked through the library/);
        assert.match(r.engine, /robfig\/cron\/v3 v3\.0\.1/);
        assert.match(r.engine, /tz database 20\d\d[a-z]/);
        assert.ok(r.next >= 30, `next-firing rows: ${r.next}`);
        assert.ok(r.scroll, 'no horizontal scroll');
      }));

    await t.test('bad input gets a clear message and keeps the last almanac', () =>
      withPage(chrome, base, { width: 1280, height: 800 }, '', async (page) => {
        await page.evaluate(setValue('expr', '0 24 * * *'));
        await page.waitFor(`!document.getElementById('expr-error').hidden`);
        const r = await page.evaluate(`({
          msg: document.getElementById('expr-error').textContent,
          bad: document.querySelector('#ruler li.bad .fld')?.textContent,
          invalid: document.getElementById('expr').getAttribute('aria-invalid'),
          stale: document.getElementById('almanac').classList.contains('stale'),
          plates: document.querySelectorAll('.plate').length,
        })`);
        assert.match(r.msg, /Hour “24”: 24 is above the largest hour, 23/);
        assert.equal(r.bad, 'hour');
        assert.equal(r.invalid, 'true');
        assert.ok(r.stale && r.plates === 3, 'the previous almanac stays, dimmed');

        await page.evaluate(setValue('zone-1', 'Mars/Base'));
        await page.evaluate(setValue('expr', '0 9 * * 1-5'));
        await page.waitFor(`!document.getElementById('zone-1-error').hidden`);
        assert.match(await page.evaluate(`document.getElementById('zone-1-error').textContent`), /Unknown time zone “Mars\/Base”/);

        await page.evaluate(setValue('zone-1', 'Europe/London'));
        await page.waitFor(`document.getElementById('sentence').textContent.includes('Monday through Friday')`);
        assert.equal(await page.evaluate(`document.getElementById('expr-error').hidden && document.getElementById('zone-1-error').hidden`), true);
      }));

    await t.test('presets, the leap day and the shared link all work', () =>
      withPage(chrome, base, { width: 1280, height: 800 }, '', async (page) => {
        await page.evaluate(setValue('preset', 'leap-day'));
        await page.waitFor(`document.getElementById('sentence').textContent.includes('29 February')`);
        const r = await page.evaluate(`({ plates: document.querySelectorAll('.plate').length, from: document.getElementById('from').value, hash: location.hash })`);
        assert.equal(r.plates, 1);
        assert.equal(r.from, '2028-01');
        assert.match(r.hash, /f=2028-01/);
        assert.match(await page.evaluate(`document.querySelector('.plate-meta').textContent`), /1 firing/);
        await page.evaluate(`document.getElementById('copy').click()`);
        await page.waitFor(`document.getElementById('copied').textContent.length > 0`);
      }));

    await t.test('state in the URL is restored', () =>
      withPage(chrome, base, { width: 1280, height: 800 }, '#e=0%209%20*%20*%201-5&z=America/New_York,Asia/Kolkata&m=shared', async (page) => {
        const r = await page.evaluate(`({ s: document.getElementById('sentence').textContent, z: document.getElementById('zone-1').value, n: document.querySelectorAll('.plate').length })`);
        assert.match(r.s, /At 09:00, Monday through Friday/);
        assert.equal(r.z, 'Asia/Kolkata');
        assert.equal(r.n, 2);
      }));

    await t.test('keyboard: skip link first, then the controls, with a visible focus ring', () =>
      withPage(chrome, base, { width: 1280, height: 800 }, '', async (page) => {
        const order = [];
        for (let i = 0; i < 3; i++) {
          await page.key('Tab', 'Tab', 9);
          order.push(await page.evaluate(`document.activeElement.id || document.activeElement.className`));
        }
        assert.deepEqual(order, ['skip', 'theme', 'expr']); // the skip link has no id, only its class
        const ring = await page.evaluate(`getComputedStyle(document.activeElement).outlineStyle`);
        assert.notEqual(ring, 'none');
        await page.key('Enter', 'Enter', 13);
      }));

    for (const scheme of ['light', 'dark']) {
      await t.test(`phone at a true 400 px, ${scheme}: no sideways scroll`, () =>
        withPage(chrome, base, { width: 400, height: 860, mobile: true, scale: 2, scheme }, '', async (page) => {
          const r = await page.evaluate(`({ iw: innerWidth, sw: document.documentElement.scrollWidth, strip: document.querySelector('svg.strip').getBoundingClientRect().width, dark: getComputedStyle(document.body).backgroundColor })`);
          assert.equal(r.iw, 400);
          assert.ok(r.sw <= r.iw, `scrollWidth ${r.sw} > ${r.iw}`);
          assert.ok(r.strip <= 400 - 32 + 1, `strip is ${r.strip}px wide`);
          if (scheme === 'dark') assert.equal(r.dark, 'rgb(13, 20, 25)');
        }));
    }

    await t.test('reduced motion stops the loading hand', async () => {
      const page = await chrome.openPage({ width: 1280, height: 800, reducedMotion: 'reduce' });
      try {
        await page.navigate(`${base}/`);
        await page.waitFor(`document.readyState === 'complete'`);
        assert.equal(await page.evaluate(`getComputedStyle(document.querySelector('.loading-hand')).animationName`), 'none');
        await page.waitFor(`document.documentElement.dataset.state === 'ready'`, 30000);
        assert.deepEqual(problemsOf(page), []);
      } finally {
        await page.close();
      }
    });
  } finally {
    await chrome.close();
    server.close();
  }
});
