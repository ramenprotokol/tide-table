// Colour checks straight from styles.css: text tokens reach WCAG AA (4.5:1)
// on both paper tones in both themes, firing ticks reach 3:1, the two copies
// of the dark theme agree, and red is only used for daylight-saving marks.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { root } from './helpers.mjs';

const css = readFileSync(join(root, 'web', 'styles.css'), 'utf8');

function block(selector) {
  const i = css.indexOf(selector);
  assert.ok(i >= 0, `no ${selector} block`);
  const open = css.indexOf('{', i);
  const close = css.indexOf('}', open);
  return css.slice(open + 1, close);
}

function tokens(body) {
  const out = {};
  for (const m of body.matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim();
  return out;
}

function rgb(hex) {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  assert.ok(m, `not a hex colour: ${hex}`);
  return [0, 2, 4].map((i) => parseInt(m[1].slice(i, i + 2), 16) / 255);
}
const lin = (c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const lum = (hex) => {
  const [r, g, b] = rgb(hex).map(lin);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
export const ratio = (a, b) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

const light = tokens(block(':root {'));
const dark = tokens(block(":root[data-theme='dark'] {"));
const darkMedia = tokens(block(":root:not([data-theme='light']) {"));

test('the two copies of the dark theme are identical', () => {
  assert.deepEqual(darkMedia, dark);
});

for (const [name, t] of [
  ['light', light],
  ['dark', dark],
]) {
  test(`${name} theme: text reaches 4.5:1 on both paper tones`, () => {
    for (const bg of ['paper', 'paper-2']) {
      for (const fg of ['ink', 'ink-2', 'ink-3', 'red']) {
        const r = ratio(t[fg], t[bg]);
        assert.ok(r >= 4.5, `${name}: --${fg} ${t[fg]} on --${bg} ${t[bg]} is ${r.toFixed(2)}:1`);
      }
    }
  });
  test(`${name} theme: past-day ticks still reach 3:1 as graphics`, () => {
    const r = ratio(t['tick-past'], t.paper);
    assert.ok(r >= 3, `--tick-past on paper is ${r.toFixed(2)}:1`);
  });
}

test('red is used only for daylight-saving marks', () => {
  const allowed = /seam|mark-|d-skip|d-double|d-moved|red-note|\.red|tr\.note/;
  for (const m of css.matchAll(/([^{}]+)\{([^}]*)\}/g)) {
    const [, sel, body] = m;
    if (!/var\(--red\)/.test(body)) continue;
    assert.match(sel.trim(), allowed, `var(--red) used by ${sel.trim()}`);
  }
});
