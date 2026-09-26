// SVG drawing for the year strip and the 24-hour dial. Pure functions: data
// in, markup out. Colours come from CSS classes so a theme switch needs no
// redraw.

export const BINS = 96; // 15-minute bins per day

export function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

const f1 = (n) => Math.round(n * 10) / 10;
const TICK_W = 2.4; // narrowest firing bar, in px

// Decode one day's bins (24 hex digits; digit k holds bins 4k..4k+3, bit j =
// bin 4k+j) into runs of [firstBin, length].
export function binRuns(hex) {
  const runs = [];
  if (!hex) return runs;
  let start = -1;
  for (let b = 0; b < BINS; b++) {
    const on = (parseInt(hex[b >> 2], 16) >> (b & 3)) & 1;
    if (on && start < 0) start = b;
    else if (!on && start >= 0) {
      runs.push([start, b - start]);
      start = -1;
    }
  }
  if (start >= 0) runs.push([start, BINS - start]);
  return runs;
}

// The label printed on the strip: the part before any comma ("12:00
// skipped, 30 days" prints as "12:00 skipped"); the full label is in the
// seam's tooltip and the seams list.
export function stripLabel(label) {
  return String(label).split(',')[0];
}

// How the twelve month columns wrap at a given width.
export function stripLayout(width) {
  const cols = width >= 860 ? 12 : width >= 560 ? 6 : width >= 330 ? 4 : 3;
  const colW = width / cols;
  const gutter = 17;
  const padR = 7;
  const trackW = colW - gutter - padR;
  const rowH = cols === 12 ? 6 : 5;
  const headH = 28;
  const footH = 16;
  const blockH = headH + 31 * rowH + footH;
  const gap = cols === 12 ? 0 : 14;
  const bands = Math.ceil(12 / cols);
  return { cols, colW, gutter, padR, trackW, rowH, headH, footH, blockH, gap, bands, width, height: bands * blockH + (bands - 1) * gap };
}

function blockOrigin(L, m) {
  return { x: (m % L.cols) * L.colW, y: Math.floor(m / L.cols) * (L.blockH + L.gap) };
}

// Where a view-day sits: month index, row, and pixel origin of the row.
export function dayPos(view, L, day) {
  for (let m = 0; m < view.months.length; m++) {
    const mo = view.months[m];
    if (day >= mo.first && day < mo.first + mo.days) {
      const o = blockOrigin(L, m);
      const row = day - mo.first;
      return { m, row, x: o.x, y: o.y + L.headH + row * L.rowH };
    }
  }
  return null;
}

// The day under a point, or -1.
export function dayAt(view, L, px, py) {
  const col = Math.floor(px / L.colW);
  const band = Math.floor(py / (L.blockH + L.gap));
  const m = band * L.cols + col;
  if (col < 0 || col >= L.cols || m < 0 || m >= view.months.length) return -1;
  const o = blockOrigin(L, m);
  const row = Math.floor((py - o.y - L.headH) / L.rowH);
  const mo = view.months[m];
  if (row < 0 || row >= mo.days) return -1;
  return mo.first + row;
}

export function minuteAt(L, px) {
  const x = px - Math.floor(px / L.colW) * L.colW - L.gutter;
  return Math.max(0, Math.min(1439, Math.round((x / L.trackW) * 1440)));
}

const WEEKDAY_CACHE = new Map();
function weekday(isoStart, day) {
  const key = `${isoStart}:${day}`;
  if (!WEEKDAY_CACHE.has(key)) {
    const [y, m, d] = isoStart.split('-').map(Number);
    WEEKDAY_CACHE.set(key, new Date(Date.UTC(y, m - 1, d + day)).getUTCDay());
  }
  return WEEKDAY_CACHE.get(key);
}

export function renderStrip(view, width, { title, desc, id }) {
  const L = stripLayout(width);
  const out = [];
  const future = [];
  const past = [];
  const tx = (x0) => x0 + L.gutter;
  const bw = L.trackW / BINS;

  out.push(`<svg class="strip" id="${id}" xmlns="http://www.w3.org/2000/svg" width="${f1(L.width)}" height="${L.height}" viewBox="0 0 ${f1(L.width)} ${L.height}" role="img" aria-labelledby="${id}-t ${id}-d">`);
  out.push(`<title id="${id}-t">${esc(title)}</title><desc id="${id}-d">${esc(desc)}</desc>`);

  view.months.forEach((mo, m) => {
    const o = blockOrigin(L, m);
    const x0 = tx(o.x);
    const top = o.y + L.headH;
    const bottom = top + mo.days * L.rowH;
    const yearMark = m === 0 || mo.month === 1 ? ` <tspan class="s-year">${mo.year}</tspan>` : '';
    out.push(`<text class="s-month" x="${f1(x0)}" y="${o.y + 11}">${esc(mo.label.toUpperCase())}${yearMark}</text>`);
    out.push(`<text class="s-count" x="${f1(x0 + L.trackW)}" y="${f1(top + 31 * L.rowH + 11)}" text-anchor="end">${mo.count.toLocaleString('en-GB')}×</text>`);
    // Hour scale: 00, 06, 12, 18 and 24 hairlines; figures on the first column.
    for (let h = 0; h <= 24; h += 6) {
      const x = f1(x0 + (h / 24) * L.trackW);
      out.push(`<line class="${h % 24 === 0 ? 's-edge' : 's-guide'}" x1="${x}" y1="${top - 3}" x2="${x}" y2="${f1(bottom)}"/>`);
      if (m % L.cols === 0 && h < 24) out.push(`<text class="s-hour" x="${x}" y="${top - 6}" text-anchor="middle">${String(h).padStart(2, '0')}</text>`);
    }
    out.push(`<line class="s-rule" x1="${f1(o.x + 2)}" y1="${top - 0.5}" x2="${f1(x0 + L.trackW)}" y2="${top - 0.5}"/>`);
    for (let r = 0; r < mo.days; r++) {
      const day = mo.first + r;
      const y = top + r * L.rowH;
      if (r > 0 && weekday(view.start, day) === 1) {
        out.push(`<line class="s-week" x1="${f1(x0)}" y1="${f1(y)}" x2="${f1(x0 + L.trackW)}" y2="${f1(y)}"/>`);
      }
      if (r === 0 || (r + 1) % 5 === 0) {
        out.push(`<text class="s-day" x="${f1(x0 - 4)}" y="${f1(y + L.rowH - 0.5)}" text-anchor="end">${r + 1}</text>`);
      }
      const runs = binRuns(view.bins[day]);
      if (!runs.length) continue;
      const bucket = view.today >= 0 && day < view.today ? past : future;
      // A firing is a bar the full height of its row, at least TICK_W wide,
      // so a daily job reads as a solid rule and a missed day as a clear
      // break: heavier than any line on the page, unlike the dotted seam.
      for (const [b, n] of runs) {
        const w = Math.max(TICK_W, n * bw);
        const x = x0 + b * bw + (n * bw < TICK_W ? (n * bw - TICK_W) / 2 : 0);
        bucket.push(`M${f1(x)} ${f1(y)}h${f1(w)}v${L.rowH}h${f1(-w)}z`);
      }
    }
    if (view.today >= mo.first && view.today < mo.first + mo.days) {
      const y = top + (view.today - mo.first) * L.rowH + L.rowH / 2;
      out.push(`<path class="s-today" d="M${f1(o.x + 1)} ${f1(y - 3)}l4.5 3l-4.5 3z"/>`);
    }
  });
  if (past.length) out.push(`<path class="s-tick s-past" d="${past.join('')}"/>`);
  if (future.length) out.push(`<path class="s-tick" d="${future.join('')}"/>`);

  const labelled = new Map(); // month -> y of the last label printed there
  view.seams.forEach((s, i) => {
    const p = dayPos(view, L, s.day);
    if (!p) return;
    const x0 = tx(p.x);
    const mx = f1(x0 + (s.minute / 1440) * L.trackW);
    const cls = s.red ? 'seam red' : 'seam quiet';
    const g = [`<g class="${cls}" data-seam="${i}"><title>${esc(`${s.date}: ${s.label}`)}</title>`];
    g.push(`<line class="seam-line" x1="${f1(x0 - 2)}" y1="${f1(p.y)}" x2="${f1(x0 + L.trackW + 2)}" y2="${f1(p.y)}"/>`);
    g.push(`<line class="seam-notch" x1="${mx}" y1="${f1(p.y - 3.5)}" x2="${mx}" y2="${f1(p.y + L.rowH + 1)}"/>`);
    if (s.red) {
      const rx = f1(p.x + L.colW - 1.5);
      g.push(`<path class="seam-flag" d="M${rx} ${f1(p.y - 3)}l-4.5 3l4.5 3z"/>`);
      // Its short label, printed beside the seam: above it, or below it on a
      // month's first rows, and nudged down clear of an earlier label.
      let ly = p.row >= 2 ? p.y - 2.5 : p.y + L.rowH + 7.5;
      const prev = labelled.get(p.m);
      if (prev !== undefined && Math.abs(ly - prev) < 9) ly = prev + 9;
      labelled.set(p.m, ly);
      g.push(`<text class="seam-label" x="${f1(x0 + L.trackW)}" y="${f1(ly)}" text-anchor="end">${esc(stripLabel(s.label))}</text>`);
    }
    for (const mk of s.marks ?? []) {
      const q = dayPos(view, L, mk.day);
      if (!q) continue;
      const x = tx(q.x) + (mk.minute / 1440) * L.trackW;
      const cy = q.y + L.rowH / 2;
      if (mk.kind === 'skip') g.push(`<circle class="mark-skip" cx="${f1(x)}" cy="${f1(cy)}" r="2.3"/>`);
      else if (mk.kind === 'double') g.push(`<path class="mark-double" d="M${f1(x - 1.2)} ${f1(q.y - 1)}v${f1(L.rowH + 2)}M${f1(x + 1.2)} ${f1(q.y - 1)}v${f1(L.rowH + 2)}"/>`);
      else g.push(`<circle class="mark-extra" cx="${f1(x)}" cy="${f1(cy)}" r="2"/>`);
    }
    g.push('</g>');
    out.push(g.join(''));
  });
  out.push('</svg>');
  return { svg: out.join(''), layout: L };
}

// The polar 24-hour dial.
export function renderDial(view, { title, id, size = 240 }) {
  const c = size / 2;
  const R = c - 6; // rim
  const R2 = R - 5; // inner rim
  const rNum = R2 - 16; // hour figures
  const r1 = rNum - 12; // outer edge of the firing band
  const r0 = size * 0.19; // inner edge of the firing band
  const ang = (min) => (min / 1440) * 2 * Math.PI - Math.PI / 2;
  const pt = (min, r) => [c + r * Math.cos(ang(min)), c + r * Math.sin(ang(min))];
  const line = (min, ra, rb) => {
    const [x1, y1] = pt(min, ra);
    const [x2, y2] = pt(min, rb);
    return `M${f1(x1)} ${f1(y1)}L${f1(x2)} ${f1(y2)}`;
  };
  const o = [];
  o.push(`<svg class="dial" id="${id}" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" role="img" aria-labelledby="${id}-t">`);
  o.push(`<title id="${id}-t">${esc(title)}</title>`);
  o.push(`<circle class="d-rim" cx="${c}" cy="${c}" r="${R}"/><circle class="d-rim2" cx="${c}" cy="${c}" r="${R2}"/>`);
  o.push(`<circle class="d-band" cx="${c}" cy="${c}" r="${f1(r1)}"/><circle class="d-band" cx="${c}" cy="${c}" r="${f1(r0)}"/>`);
  // Graduations: quarter hours, hours, and the four cardinal hours.
  const grads = [];
  const majors = [];
  for (let q = 0; q < 96; q++) {
    const min = q * 15;
    if (q % 24 === 0) majors.push(line(min, R2 - 11, R));
    else if (q % 4 === 0) grads.push(line(min, R2 - 6, R2));
    else grads.push(line(min, R2 - 2.5, R2));
  }
  o.push(`<path class="d-grad" d="${grads.join('')}"/><path class="d-major" d="${majors.join('')}"/>`);
  // Cross hairs inside the band, like an engraved compass card.
  o.push(`<path class="d-cross" d="${line(0, 12, r0 - 3)}${line(360, 12, r0 - 3)}${line(720, 12, r0 - 3)}${line(1080, 12, r0 - 3)}"/>`);
  for (let h = 0; h < 24; h++) {
    const [x, y] = pt(h * 60, rNum);
    if (h % 2 === 0) o.push(`<text class="d-num${h % 6 === 0 ? ' d-num-major' : ''}" x="${f1(x)}" y="${f1(y + 3.4)}" text-anchor="middle">${String(h).padStart(2, '0')}</text>`);
    else o.push(`<circle class="d-dot" cx="${f1(x)}" cy="${f1(y)}" r="1.1"/>`);
  }
  // Firings: one radial tick per minute of the day that fires, long in
  // proportion to how many days fire then.
  const max = view.dial.reduce((a, [, n]) => Math.max(a, n), 0);
  if (max > 0) {
    const ticks = view.dial.map(([min, n]) => line(min, r0, r0 + (r1 - r0) * (0.22 + 0.78 * (n / max)))).join('');
    const weight = view.dial.length > 240 ? 'thin' : view.dial.length > 48 ? 'mid' : 'bold';
    o.push(`<path class="d-tick d-tick-${weight}" d="${ticks}"/>`);
  }
  // Red marks where a clock change touched a firing.
  const seen = new Set();
  for (const s of view.seams) {
    for (const mk of s.marks ?? []) {
      const key = `${mk.kind}:${mk.minute}`;
      if (seen.has(key)) continue;
      seen.add(key);
      if (mk.kind === 'skip') {
        const [x, y] = pt(mk.minute, r1 + 5.5);
        o.push(`<circle class="d-skip" cx="${f1(x)}" cy="${f1(y)}" r="2.6"><title>${esc(`${s.label}, ${s.date}`)}</title></circle>`);
      } else if (mk.kind === 'double') {
        // Inside the band, so it never sits on a skip ring at the same time.
        const off = 1.7 / (r0 - 6);
        const a = (mk.minute / 1440) * 2 * Math.PI - Math.PI / 2;
        const seg = (d) => {
          const x1 = c + (r0 - 2.5) * Math.cos(a + d), y1 = c + (r0 - 2.5) * Math.sin(a + d);
          const x2 = c + (r0 - 10) * Math.cos(a + d), y2 = c + (r0 - 10) * Math.sin(a + d);
          return `M${f1(x1)} ${f1(y1)}L${f1(x2)} ${f1(y2)}`;
        };
        o.push(`<path class="d-double" d="${seg(-off)}${seg(off)}"><title>${esc(`${s.label}, ${s.date}`)}</title></path>`);
      } else {
        const [x, y] = pt(mk.minute, r1 + 5.5);
        o.push(`<circle class="d-extra" cx="${f1(x)}" cy="${f1(y)}" r="2.2"><title>${esc(`${s.label}, ${s.date}`)}</title></circle>`);
      }
    }
  }
  // Now: a fine hand.
  const [hx, hy] = pt(view.nowMinute, R2 - 13);
  const [bx, by] = pt(view.nowMinute + 720, 9);
  o.push(`<path class="d-hand" d="M${f1(bx)} ${f1(by)}L${f1(hx)} ${f1(hy)}"/><circle class="d-pivot" cx="${c}" cy="${c}" r="2.2"/>`);
  o.push(`<text class="d-abbr" x="${c}" y="${f1(c + r0 * 0.55 + 4)}" text-anchor="middle">${esc(view.abbr)}</text>`);
  o.push('</svg>');
  return o.join('');
}

// A short spoken summary of the dial for screen readers.
export function dialSummary(view) {
  if (!view.dial.length) return `24-hour dial for ${view.zone}: no firings in these twelve months.`;
  const top = [...view.dial].sort((a, b) => b[1] - a[1]).slice(0, 6);
  const hm = (m) => `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
  const parts = top.map(([m, n]) => `${hm(m)} (${n.toLocaleString('en-GB')})`);
  const more = view.dial.length > top.length ? `, and ${view.dial.length - top.length} more times of day` : '';
  return `24-hour dial for ${view.zone}, local time: firings at ${parts.join(', ')}${more}. The hand shows now, ${hm(view.nowMinute)}.`;
}
