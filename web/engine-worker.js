// Runs the Go engine off the main thread. Loads wasm_exec.js (Go's own
// loader), streams the .wasm with progress, then answers compute requests.
/* global Go, tideTable */
'use strict';

const WASM_URL = new URL('./tide.wasm', self.location.href).href;
const WASM_BYTES = Number('__WASM_BYTES__'); // filled in by the build

importScripts('./wasm_exec.js');

async function fetchWithProgress(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`the engine file answered ${res.status}`);
  if (!res.body) return new Uint8Array(await res.arrayBuffer());
  const reader = res.body.getReader();
  const chunks = [];
  let loaded = 0;
  let last = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    loaded += value.length;
    const now = performance.now();
    if (now - last > 60) {
      last = now;
      postMessage({ type: 'progress', loaded, total: WASM_BYTES || loaded });
    }
  }
  postMessage({ type: 'progress', loaded, total: WASM_BYTES || loaded });
  const out = new Uint8Array(loaded);
  let off = 0;
  for (const c of chunks) {
    out.set(c, off);
    off += c.length;
  }
  return out;
}

function transfer(url) {
  const e = performance.getEntriesByName(url)[0];
  if (!e) return null;
  return { transferSize: e.transferSize, encodedBodySize: e.encodedBodySize, decodedBodySize: e.decodedBodySize };
}

async function start() {
  const t0 = performance.now();
  const bytes = await fetchWithProgress(WASM_URL);
  const go = new Go();
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
  go.run(instance); // returns once Go's main blocks; the API is then on self
  if (typeof tideTable === 'undefined') throw new Error('the engine started but did not register itself');
  postMessage({
    type: 'ready',
    info: JSON.parse(tideTable.info),
    zones: JSON.parse(tideTable.zones),
    wasmBytes: bytes.length,
    transfer: transfer(WASM_URL),
    loadMs: performance.now() - t0,
  });
}

self.onmessage = (ev) => {
  const { id, expr, zones, mode, from, now } = ev.data;
  try {
    const t0 = performance.now();
    const json = tideTable.compute(expr, zones, mode, from, now);
    postMessage({ type: 'result', id, json, ms: performance.now() - t0 });
  } catch (err) {
    postMessage({ type: 'result', id, error: String(err && err.message ? err.message : err) });
  }
};

start().catch((err) => postMessage({ type: 'failed', message: String(err && err.message ? err.message : err) }));
