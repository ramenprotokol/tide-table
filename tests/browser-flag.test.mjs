// The browser test must not pass silently in CI: with REQUIRE_BROWSER=1 and
// no Chrome it fails; locally it skips.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { browserPlan, findChrome } from './cdp.mjs';

test('browserPlan runs when Chrome is found', () => {
  assert.deepEqual(browserPlan('/some/chrome', {}), { run: true });
});

test('browserPlan skips without Chrome by default', () => {
  assert.ok(browserPlan(null, {}).skip);
});

test('browserPlan fails without Chrome when REQUIRE_BROWSER=1', () => {
  assert.match(browserPlan(null, { REQUIRE_BROWSER: '1' }).fail, /REQUIRE_BROWSER=1/);
});

test('an explicit CHROME_PATH that does not exist means no Chrome', () => {
  assert.equal(findChrome({ CHROME_PATH: '/nowhere/chrome' }), null);
});
