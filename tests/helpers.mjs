// Shared helpers for the JS tests: find built assets and boot the Go engine
// in Node exactly as the worker does in the browser.
import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

export const root = join(dirname(fileURLToPath(import.meta.url)), '..');
export const dist = join(root, 'dist');

export function asset(prefix, ext) {
  const dir = join(dist, 'a');
  if (!existsSync(dir)) throw new Error('dist/ is missing: run `npm run build` first');
  const hits = readdirSync(dir).filter((f) => f.startsWith(`${prefix}.`) && f.endsWith(ext));
  if (hits.length !== 1) throw new Error(`expected one ${prefix}.*${ext} in dist/a, found ${hits.length}`);
  return join(dir, hits[0]);
}

let booted = null;

// Loads Go's wasm_exec.js and the engine, returns globalThis.tideTable.
export async function bootEngine() {
  if (booted) return booted;
  await import(pathToFileURL(asset('wasm_exec', '.js')).href);
  const go = new globalThis.Go();
  const bytes = readFileSync(asset('tide', '.wasm'));
  const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
  go.run(instance); // returns when Go's main blocks; the API is registered by then
  booted = globalThis.tideTable;
  if (!booted) throw new Error('the engine did not register globalThis.tideTable');
  return booted;
}

// A fixed "now": 26 September 2026, 12:00 UTC.
export const NOW = Date.UTC(2026, 8, 26, 12, 0, 0);

export function compute(T, expr, zones, mode = 'shared', from = '', now = NOW) {
  return JSON.parse(T.compute(expr, zones, mode, from, now));
}
