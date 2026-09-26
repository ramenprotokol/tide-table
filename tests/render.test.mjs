// Unit tests for the drawing helpers (pure functions, no DOM).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { binRuns, stripLayout, dayPos, dayAt, renderStrip, renderDial, dialSummary, esc } from '../web/render.js';

// Encode bins the way the Go engine does: digit k holds bins 4k..4k+3.
function encode(bins) {
  const nib = new Array(24).fill(0);
  for (const b of bins) nib[b >> 2] |= 1 << (b & 3);
  return nib.map((n) => n.toString(16)).join('');
}

test('binRuns decodes the engine encoding', () => {
  assert.deepEqual(binRuns(''), []);
  assert.deepEqual(binRuns(encode([0])), [[0, 1]]);
  assert.deepEqual(binRuns(encode([7])), [[7, 1]]);
  assert.deepEqual(binRuns('080000000000000000000000'), [[7, 1]]); // 01:45, as the Go test pins it
  assert.deepEqual(binRuns(encode([0, 1, 2, 3, 4, 50, 95])), [[0, 5], [50, 1], [95, 1]]);
  assert.deepEqual(binRuns('f'.repeat(24)), [[0, 96]]);
});

const months = (() => {
  // Sep 2026 – Aug 2027
  const lengths = [30, 31, 30, 31, 31, 28, 31, 30, 31, 30, 31, 31];
  let first = 0;
  return lengths.map((days, i) => {
    const m = { year: i < 4 ? 2026 : 2027, month: ((8 + i) % 12) + 1, label: 'Mon', first, days, count: 0 };
    first += days;
    return m;
  });
})();

const view = {
  zone: 'Test/Zone',
  start: '2026-09-01',
  days: 365,
  today: 25,
  nowMinute: 720,
  abbr: 'TST',
  months,
  bins: Array.from({ length: 365 }, () => encode([10])),
  counts: new Array(365).fill(1),
  dial: [[150, 364]],
  seams: [{ kind: 'skip', red: true, day: 33, minute: 120, date: 'Sun 4 Oct 2026', label: '02:30 skipped', marks: [{ day: 33, minute: 150, kind: 'skip' }] }],
};

test('stripLayout wraps twelve months to the width', () => {
  assert.equal(stripLayout(1120).cols, 12);
  assert.equal(stripLayout(700).cols, 6);
  assert.equal(stripLayout(368).cols, 4);
  assert.equal(stripLayout(300).cols, 3);
  for (const w of [300, 368, 700, 1120]) {
    const L = stripLayout(w);
    assert.ok(L.trackW > 40, `track at ${w}px is ${L.trackW}`);
    assert.equal(L.cols * L.colW, w);
  }
});

test('dayAt finds the day that dayPos placed', () => {
  for (const w of [368, 700, 1120]) {
    const L = stripLayout(w);
    for (let d = 0; d < 365; d += 7) {
      const p = dayPos(view, L, d);
      assert.equal(dayAt(view, L, p.x + L.gutter + 5, p.y + L.rowH / 2), d, `day ${d} at ${w}px`);
    }
    assert.equal(dayAt(view, L, -5, 10), -1);
  }
});

test('renderStrip draws ticks, today, and a red seam with its mark', () => {
  const { svg, layout } = renderStrip(view, 1120, { title: 'T <x>', desc: 'D', id: 's0' });
  assert.match(svg, /^<svg class="strip"/);
  assert.match(svg, /<title id="s0-t">T &lt;x&gt;<\/title>/);
  assert.match(svg, /class="s-tick s-past"/); // days before "today"
  assert.match(svg, /class="s-tick"/);
  assert.match(svg, /class="s-today"/);
  assert.match(svg, /<g class="seam red" data-seam="0">/);
  assert.match(svg, /class="mark-skip"/);
  assert.equal(layout.cols, 12);
  assert.ok(!/style=/.test(svg), 'no inline style attributes (the CSP forbids them)');
});

test('renderDial draws a tick per firing minute and the red mark', () => {
  const svg = renderDial(view, { title: dialSummary(view), id: 'd0' });
  assert.match(svg, /class="d-tick d-tick-bold"/);
  assert.match(svg, /class="d-skip"/);
  assert.match(svg, /class="d-hand"/);
  assert.ok(!/style=/.test(svg));
  assert.match(dialSummary(view), /firings at 02:30 \(364\)/);
});

test('esc escapes markup', () => {
  assert.equal(esc(`<a href="x">'&'</a>`), '&lt;a href=&quot;x&quot;&gt;&#39;&amp;&#39;&lt;/a&gt;');
});
