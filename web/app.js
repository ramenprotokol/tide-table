// Tide Table: the page. Go (in a worker) computes; this file draws and listens.
import { Engine } from './engine.js';
import { renderStrip, renderDial, dialSummary, dayAt, esc } from './render.js';
import { PRESETS } from './presets.js';

const $ = (id) => document.getElementById(id);
const els = {
  form: $('query'),
  expr: $('expr'),
  ruler: $('ruler'),
  exprError: $('expr-error'),
  preset: $('preset'),
  presetNote: $('preset-note'),
  zones: $('zones'),
  addZone: $('add-zone'),
  tzList: $('tz-list'),
  from: $('from'),
  fromNow: $('from-now'),
  fromError: $('from-error'),
  sentence: $('sentence'),
  copy: $('copy'),
  copied: $('copied'),
  summary: $('summary'),
  loading: $('loading'),
  loadingDetail: $('loading-detail'),
  gauge: $('gauge-fill'),
  almanac: $('almanac'),
  engineInfo: $('engine-info'),
  tip: $('tip'),
  theme: $('theme'),
};

const MAX_ZONES = 3;
const FIELD_NAMES = ['minute', 'hour', 'day', 'month', 'weekday'];
const ROMAN = ['I', 'II', 'III'];
const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const SHORT_DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const SHORT_MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const num = (n) => n.toLocaleString('en-GB');

let state = null;
let result = null;
let layouts = [];
let ready = false;
let timer = 0;
let lastWidth = 0;

// ---------- state <-> URL ----------

function readHash() {
  try {
    const p = new URLSearchParams(location.hash.slice(1));
    const expr = (p.get('e') ?? '').slice(0, 120);
    const zones = (p.get('z') ?? '')
      .split(',')
      .map((s) => s.trim().slice(0, 64))
      .filter(Boolean)
      .slice(0, MAX_ZONES);
    if (!expr || !zones.length) return null;
    const f = p.get('f') ?? '';
    return { expr, zones, mode: p.get('m') === 'each' ? 'each' : 'shared', from: /^\d{4}-\d{2}$/.test(f) ? f : '', preset: '' };
  } catch {
    return null;
  }
}

function writeHash() {
  const p = new URLSearchParams({ e: state.expr, z: state.zones.join(','), m: state.mode });
  if (state.from) p.set('f', state.from);
  try {
    history.replaceState(null, '', `#${p.toString()}`);
  } catch {
    /* some embedded views forbid it; the page still works */
  }
}

function fromPreset(p) {
  return { expr: p.expr, zones: [...p.zones], mode: p.mode ?? 'shared', from: p.from ?? '', preset: p.id };
}

// ---------- form ----------

function buildPresetSelect() {
  const opts = ['<option value="">Choose a starting point…</option>'];
  for (const p of PRESETS) opts.push(`<option value="${esc(p.id)}">${esc(p.name)}</option>`);
  els.preset.innerHTML = opts.join('');
}

function syncForm() {
  els.expr.value = state.expr;
  els.preset.value = state.preset || '';
  const p = PRESETS.find((x) => x.id === state.preset);
  els.presetNote.textContent = p ? `${p.note[0].toUpperCase()}${p.note.slice(1)}.` : '';
  for (const r of els.form.elements.mode) r.checked = r.value === state.mode;
  els.from.value = state.from || (result?.from ?? '');
  renderZoneRows();
}

function renderZoneRows() {
  els.zones.textContent = '';
  state.zones.forEach((z, i) => {
    const row = document.createElement('div');
    row.className = 'zone-row';
    const ord = document.createElement('span');
    ord.className = 'ord';
    ord.textContent = `${ROMAN[i]}.`;
    ord.setAttribute('aria-hidden', 'true');
    const input = document.createElement('input');
    input.type = 'text';
    input.id = `zone-${i}`;
    input.value = z;
    input.setAttribute('list', 'tz-list');
    input.setAttribute('aria-label', `Time zone ${i + 1}`);
    input.setAttribute('aria-describedby', `zone-${i}-role zone-${i}-error`);
    input.spellcheck = false;
    input.autocomplete = 'off';
    input.autocapitalize = 'off';
    input.maxLength = 64;
    input.addEventListener('input', () => {
      state.zones[i] = input.value;
      customised();
      schedule(260);
    });
    row.append(ord, input);
    if (state.zones.length > 1) {
      const rm = document.createElement('button');
      rm.type = 'button';
      rm.className = 'zone-remove';
      rm.textContent = 'Remove';
      rm.setAttribute('aria-label', `Remove time zone ${i + 1}${z ? `, ${z}` : ''}`);
      rm.addEventListener('click', () => {
        state.zones.splice(i, 1);
        customised();
        renderZoneRows();
        $(`zone-${Math.min(i, state.zones.length - 1)}`)?.focus();
        schedule(0);
      });
      row.append(rm);
    } else {
      row.append(document.createElement('span'));
    }
    const role = document.createElement('p');
    role.className = 'role';
    role.id = `zone-${i}-role`;
    role.textContent = roleText(i);
    const err = document.createElement('p');
    err.className = 'error zone-error';
    err.id = `zone-${i}-error`;
    err.setAttribute('aria-live', 'polite');
    err.hidden = true;
    row.append(role, err);
    els.zones.append(row);
  });
  els.addZone.hidden = state.zones.length >= MAX_ZONES;
}

function roleText(i) {
  if (state.mode === 'each') return 'runs its own copy of the job';
  return i === 0 ? 'cron reads this clock' : 'reads the same firings';
}

function customised() {
  if (state.preset) {
    state.preset = '';
    els.preset.value = '';
    els.presetNote.textContent = '';
  }
}

function renderRuler(tokens, bad = -1) {
  els.ruler.textContent = '';
  if (tokens.length !== 5) return;
  tokens.forEach((t, i) => {
    const li = document.createElement('li');
    if (i === bad) li.className = 'bad';
    const tok = document.createElement('span');
    tok.className = 'tok';
    tok.textContent = t;
    const fld = document.createElement('span');
    fld.className = 'fld';
    fld.textContent = FIELD_NAMES[i];
    li.append(tok, fld);
    els.ruler.append(li);
  });
}

function clearErrors() {
  els.exprError.hidden = true;
  els.expr.removeAttribute('aria-invalid');
  els.fromError.hidden = true;
  els.from.removeAttribute('aria-invalid');
  document.querySelectorAll('.zone-error').forEach((e) => (e.hidden = true));
  document.querySelectorAll('.zone-row input').forEach((e) => e.removeAttribute('aria-invalid'));
}

function showError(err) {
  if (err.kind === 'expr') {
    els.exprError.textContent = err.message;
    els.exprError.hidden = false;
    els.expr.setAttribute('aria-invalid', 'true');
    renderRuler(els.expr.value.trim().split(/\s+/), err.field);
  } else if (err.kind === 'zone') {
    const i = Math.min(Math.max(err.index, 0), state.zones.length - 1);
    const box = $(`zone-${i}-error`);
    if (box) {
      box.textContent = err.message;
      box.hidden = false;
    }
    $(`zone-${i}`)?.setAttribute('aria-invalid', 'true');
  } else {
    els.fromError.textContent = err.message;
    els.fromError.hidden = false;
    els.from.setAttribute('aria-invalid', 'true');
  }
  els.almanac.classList.toggle('stale', !!result);
  els.summary.textContent = result ? 'The almanac below still shows the last reading that worked.' : '';
}

// ---------- computing ----------

function schedule(delay = 160) {
  clearTimeout(timer);
  timer = setTimeout(run, delay);
}

async function run() {
  if (!ready) return;
  const req = { expr: state.expr, zones: state.zones.map((z) => z.trim()), mode: state.mode, from: state.from, now: Date.now() };
  let out;
  try {
    out = await engine.compute(req);
  } catch (err) {
    els.summary.textContent = `The engine stopped: ${err.message}. Reload the page to start it again.`;
    return;
  }
  if (out.stale) return;
  clearErrors();
  if (out.result.error) {
    showError(out.result.error);
    return;
  }
  result = out.result;
  els.almanac.classList.remove('stale');
  if (!state.from) els.from.value = result.from;
  renderRuler(result.fields);
  renderReading(result, out.ms);
  renderPlates(result);
  writeHash();
  document.documentElement.dataset.state = 'ready';
}

// ---------- the reading ----------

function zoneClause(res) {
  return res.mode === 'each' ? 'each zone’s own clock' : `${res.views[0].runZone} time`;
}

function renderReading(res, ms) {
  els.sentence.innerHTML = `${esc(res.description)}<span class="zone-clause">&ensp;— ${esc(zoneClause(res))}</span>`;
  els.copy.disabled = false;
  const v = res.views[0];
  const m0 = v.months[0];
  const m11 = v.months[11];
  const reds = res.views.reduce((n, x) => n + x.seams.filter((s) => s.red).length, 0);
  const parts = [];
  if (res.never) {
    parts.push('This schedule never fires: robfig/cron finds no matching time within the five years it searches.');
  } else {
    parts.push(`${num(v.total)} ${v.total === 1 ? 'firing' : 'firings'} from ${MONTHS[m0.month - 1]} ${m0.year} through ${MONTHS[m11.month - 1]} ${m11.year} on ${esc(v.zone)}’s calendar`);
  }
  const redText = reds === 0 ? 'no red seams' : `${reds} red ${reds === 1 ? 'seam' : 'seams'}`;
  parts.push(`<span class="${reds ? 'red-note' : ''}">${redText}</span>`);
  parts.push(`${num(res.walked)} firings walked through the library’s <code>Next</code>, in ${ms < 1 ? 'under 1' : num(Math.round(ms))} ms on this device (measured)`);
  els.summary.innerHTML = parts.join(' · ');
}

async function copySentence() {
  if (!result) return;
  const text = `${result.description} (${zoneClause(result)})`;
  let ok = false;
  try {
    await navigator.clipboard.writeText(text);
    ok = true;
  } catch {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.className = 'offscreen';
    document.body.append(ta);
    ta.select();
    try {
      ok = document.execCommand('copy');
    } catch {
      ok = false;
    }
    ta.remove();
  }
  els.copied.textContent = ok ? 'Copied.' : 'Couldn’t reach the clipboard; select the sentence to copy it.';
  setTimeout(() => (els.copied.textContent = ''), 2600);
}

// ---------- plates ----------

function dateOf(start, day) {
  const [y, m, d] = start.split('-').map(Number);
  const t = new Date(Date.UTC(y, m - 1, d + day));
  return `${SHORT_DAYS[t.getUTCDay()]} ${t.getUTCDate()} ${SHORT_MONTHS[t.getUTCMonth()]} ${t.getUTCFullYear()}`;
}

function roleLine(res, v, i) {
  if (res.mode === 'each') return 'Standard port: this zone runs its own copy of the job';
  if (i === 0) return 'Standard port: the clock cron reads';
  return `Secondary port: ${esc(v.runZone)}’s firings, read on this clock`;
}

function stripTitle(v) {
  return `Year strip for ${v.zone}, ${MONTHS[v.months[0].month - 1]} ${v.months[0].year} to ${MONTHS[v.months[11].month - 1]} ${v.months[11].year}`;
}

function stripDesc(v) {
  const reds = v.seams.filter((s) => s.red);
  const s = reds.length ? ` Red seams: ${reds.map((x) => `${x.date}, ${x.label}`).join('; ')}.` : ' No red seams.';
  return `${num(v.total)} firings. Each row is a day, midnight to midnight on this clock.${s} Details are in the seams list below.`;
}

function legend() {
  const sw = (inner) => `<svg viewBox="0 0 22 10" aria-hidden="true">${inner}</svg>`;
  return `<ul class="strip-key">
    <li>${sw('<rect class="s-tick" x="9" y="1" width="2" height="8"/>')}a firing (15-min bins)</li>
    <li><span class="key-count">31×</span>firings that month</li>
    <li>${sw('<rect class="s-tick s-past" x="9" y="1" width="2" height="8"/>')}already past</li>
    <li>${sw('<g class="seam red"><line class="seam-line" x1="0" y1="5" x2="22" y2="5"/></g>')}clock change that hits the job</li>
    <li>${sw('<g class="seam quiet"><line class="seam-line" x1="0" y1="5" x2="22" y2="5"/></g>')}clock change, no effect</li>
    <li>${sw('<circle class="mark-skip" cx="11" cy="5" r="2.6"/>')}skipped</li>
    <li>${sw('<path class="mark-double" d="M9.6 0v10M12.4 0v10"/>')}fired twice</li>
  </ul>`;
}

function seamsHTML(v) {
  if (!v.seams.length) return '<p class="empty">No clock changes on this page’s twelve months.</p>';
  const items = v.seams.map(
    (s, j) => `<li class="seam-item${s.red ? ' red' : ''}" data-seam="${j}">
      <div class="when"><span class="date">${esc(s.date)}</span><span class="tag">${esc(s.label)}</span></div>
      <p>${esc(s.text)}</p></li>`,
  );
  return `<ol>${items.join('')}</ol>`;
}

function nextHTML(v) {
  if (!v.next.length) return `<p class="empty">${esc(v.nextNote || 'No upcoming firings.')}</p>`;
  const rows = v.next.map((f) => {
    const main = `<tr><td>${esc(f.date)}</td><td class="t">${esc(f.time)}</td><td class="z">${esc(f.abbr)}</td></tr>`;
    return f.note ? `${main}<tr class="note"><td colspan="3">${esc(f.note)}</td></tr>` : main;
  });
  return `<table><thead><tr><th scope="col">Date</th><th scope="col">Time</th><th scope="col">Zone</th></tr></thead><tbody>${rows.join('')}</tbody></table>`;
}

function stripWidth() {
  return Math.max(280, Math.floor(els.almanac.clientWidth));
}

function renderPlates(res) {
  const width = stripWidth();
  lastWidth = width;
  layouts = [];
  const html = res.views.map((v, i) => {
    const strip = renderStrip(v, width, { title: stripTitle(v), desc: stripDesc(v), id: `strip-${i}` });
    layouts[i] = strip.layout;
    const reds = v.seams.filter((s) => s.red).length;
    const empty = v.total === 0 && v.next.length ? ` None fall in these twelve months; the next is ${esc(v.next[0].date)} at ${esc(v.next[0].time)}.` : '';
    return `<article class="plate" data-i="${i}" aria-labelledby="plate-${i}-h">
      <header class="plate-head">
        <p class="plate-no">Plate ${ROMAN[i]}</p>
        <h2 id="plate-${i}-h">${esc(v.zone)}</h2>
        <p class="zone-now">${esc(v.abbr)} · ${esc(v.offset)}</p>
        <p class="plate-meta">${roleLine(res, v, i)}. <em>${num(v.total)}</em> ${v.total === 1 ? 'firing' : 'firings'}${reds ? `, ${reds} red ${reds === 1 ? 'seam' : 'seams'}` : ''}.${empty}</p>
      </header>
      <div class="strip-wrap">${strip.svg}</div>
      ${legend()}
      <div class="plate-foot">
        <figure class="dial-fig">${renderDial(v, { title: dialSummary(v), id: `dial-${i}` })}<figcaption>Firings by time of day on this clock. The dashed hand is now.</figcaption></figure>
        <section class="seams" aria-labelledby="seams-${i}-h"><h3 id="seams-${i}-h">Seams</h3>${seamsHTML(v)}</section>
        <section class="next" aria-labelledby="next-${i}-h"><h3 id="next-${i}-h">Next ten firings</h3>${nextHTML(v)}</section>
      </div>
    </article>`;
  });
  els.almanac.innerHTML = html.join('');
}

function redrawStrips() {
  if (!result) return;
  const width = stripWidth();
  if (width === lastWidth) return;
  lastWidth = width;
  result.views.forEach((v, i) => {
    const wrap = els.almanac.querySelector(`.plate[data-i="${i}"] .strip-wrap`);
    if (!wrap) return;
    const strip = renderStrip(v, width, { title: stripTitle(v), desc: stripDesc(v), id: `strip-${i}` });
    layouts[i] = strip.layout;
    wrap.innerHTML = strip.svg;
  });
}

// Hover: day details on the strip, and seam list <-> strip highlighting.
function onPointerMove(e) {
  const svg = e.target.closest?.('svg.strip');
  if (!svg || !result) return hideTip();
  const plate = svg.closest('.plate');
  const i = Number(plate.dataset.i);
  const v = result.views[i];
  const L = layouts[i];
  const r = svg.getBoundingClientRect();
  const px = ((e.clientX - r.left) * L.width) / r.width;
  const py = ((e.clientY - r.top) * L.height) / r.height;
  const d = dayAt(v, L, px, py);
  if (d < 0) return hideTip();
  const n = v.counts[d];
  const times = v.times[d] ? `: ${v.times[d].split(' ').join(', ')}` : '';
  const seams = v.seams.filter((s) => s.day === d).map((s) => `<br><span class="${s.red ? 'red-note' : ''}">${esc(s.label)}</span>`);
  els.tip.innerHTML = `<strong>${esc(dateOf(v.start, d))}</strong><br>${n === 0 ? 'No firings' : `${num(n)} ${n === 1 ? 'firing' : 'firings'}`}${esc(times)}${seams.join('')}`;
  els.tip.hidden = false;
  const tw = els.tip.offsetWidth;
  const th = els.tip.offsetHeight;
  const x = Math.min(e.clientX + 14, window.innerWidth - tw - 8);
  const y = e.clientY + th + 24 > window.innerHeight ? e.clientY - th - 12 : e.clientY + 16;
  els.tip.style.left = `${Math.max(8, x)}px`;
  els.tip.style.top = `${Math.max(8, y)}px`;
}

function hideTip() {
  els.tip.hidden = true;
}

function setHot(target, on) {
  const item = target.closest?.('[data-seam]');
  if (!item) return;
  const plate = item.closest('.plate');
  if (!plate) return;
  const j = item.dataset.seam;
  plate.querySelectorAll(`[data-seam="${j}"]`).forEach((el) => el.classList.toggle('hot', on));
}

// ---------- theme ----------

function isDark() {
  const t = document.documentElement.dataset.theme;
  if (t) return t === 'dark';
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

function applyTheme(t) {
  if (t) document.documentElement.dataset.theme = t;
  else delete document.documentElement.dataset.theme;
  els.theme.setAttribute('aria-pressed', String(isDark()));
}

function initTheme() {
  let saved = null;
  try {
    saved = localStorage.getItem('tide-table-theme');
  } catch {
    saved = null;
  }
  applyTheme(saved === 'dark' || saved === 'light' ? saved : null);
  els.theme.addEventListener('click', () => {
    const next = isDark() ? 'light' : 'dark';
    applyTheme(next);
    try {
      localStorage.setItem('tide-table-theme', next);
    } catch {
      /* private mode: the choice lasts for this visit */
    }
  });
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener?.('change', () => applyTheme(document.documentElement.dataset.theme));
}

// ---------- engine ----------

const engine = new Engine({
  onProgress(loaded, total) {
    const pct = Math.min(100, (loaded / total) * 100);
    els.gauge.style.width = `${pct.toFixed(1)}%`;
    els.loadingDetail.textContent = `${num(Math.round(loaded / 1024))} of ${num(Math.round(total / 1024))} KB of WebAssembly received.`;
  },
});

function describeLoad(m) {
  const kb = (n) => `${num(Math.round(n / 1024))} KB`;
  const t = m.transfer;
  let wire = 'transfer size not reported by this browser';
  if (t && t.transferSize > 0) wire = `${kb(t.encodedBodySize || t.transferSize)} came over the network this visit`;
  else if (t) wire = 'this visit it came from the browser cache';
  return `Engine: ${esc(m.info.go)} compiled to WebAssembly and run in a worker thread; ${esc(m.info.cron)} (MIT licence); IANA tz database ${esc(m.info.tzdata)} built in. The engine file is ${kb(m.wasmBytes)} uncompressed; ${wire}; it was ready ${num(Math.round(m.loadMs))} ms after the worker asked for it (measured).`;
}

async function boot() {
  initTheme();
  buildPresetSelect();
  state = readHash() ?? fromPreset(PRESETS[0]);
  syncForm();
  renderRuler(state.expr.trim().split(/\s+/));

  els.form.addEventListener('submit', (e) => {
    e.preventDefault();
    schedule(0);
  });
  els.expr.addEventListener('input', () => {
    state.expr = els.expr.value;
    customised();
    schedule(180);
  });
  els.preset.addEventListener('change', () => {
    const p = PRESETS.find((x) => x.id === els.preset.value);
    if (!p) return;
    state = fromPreset(p);
    syncForm();
    schedule(0);
  });
  els.addZone.addEventListener('click', () => {
    if (state.zones.length >= MAX_ZONES) return;
    state.zones.push('');
    customised();
    renderZoneRows();
    $(`zone-${state.zones.length - 1}`)?.focus();
  });
  for (const r of els.form.elements.mode) {
    r.addEventListener('change', () => {
      state.mode = r.value;
      customised();
      document.querySelectorAll('.zone-row .role').forEach((el, i) => (el.textContent = roleText(i)));
      schedule(0);
    });
  }
  els.from.addEventListener('change', () => {
    state.from = els.from.value.trim();
    customised();
    schedule(0);
  });
  els.fromNow.addEventListener('click', () => {
    state.from = '';
    els.from.value = '';
    schedule(0);
  });
  // A pasted link changes only the hash; follow it. (Our own updates use
  // history.replaceState, which doesn't fire this.)
  window.addEventListener('hashchange', () => {
    const next = readHash();
    if (!next) return;
    state = next;
    syncForm();
    schedule(0);
  });
  els.copy.addEventListener('click', copySentence);
  els.almanac.addEventListener('pointermove', onPointerMove);
  els.almanac.addEventListener('pointerleave', hideTip);
  els.almanac.addEventListener('mouseover', (e) => setHot(e.target, true));
  els.almanac.addEventListener('mouseout', (e) => setHot(e.target, false));
  if ('ResizeObserver' in window) {
    let raf = 0;
    new ResizeObserver(() => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(redrawStrips);
    }).observe(els.almanac);
  }

  try {
    const m = await engine.start();
    ready = true;
    els.loading.hidden = true;
    els.tzList.innerHTML = m.zones.map((z) => `<option value="${esc(z)}"></option>`).join('');
    els.engineInfo.innerHTML = describeLoad(m);
    run();
  } catch (err) {
    els.loading.classList.add('failed');
    els.loadingDetail.textContent = `The almanac engine didn’t load (${err.message}). Reload the page to try again.`;
    document.documentElement.dataset.state = 'failed';
  }
}

boot();
