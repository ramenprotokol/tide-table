#!/usr/bin/env node
// Builds dist/ from a clean clone: compiles the Go engine to WebAssembly
// (GOOS=js GOARCH=wasm), copies Go's wasm_exec.js, content-hashes every asset
// into dist/a/ and rewrites references, so the hashed files can be cached
// forever while index.html is always revalidated.
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync, brotliCompressSync, constants } from 'node:zlib';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const web = join(root, 'web');
const dist = join(root, 'dist');
const assets = join(dist, 'a');
const work = join(root, 'build');

const go = (args, env = {}) =>
  execFileSync('go', args, { cwd: root, env: { ...process.env, ...env }, encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'] }).trim();

rmSync(dist, { recursive: true, force: true });
mkdirSync(assets, { recursive: true });
mkdirSync(work, { recursive: true });

// 1. The engine.
const goVersion = go(['env', 'GOVERSION']);
const goroot = go(['env', 'GOROOT']);
const wasmOut = join(work, 'tide.wasm');
go(['build', '-trimpath', '-buildvcs=false', '-ldflags=-s -w', '-o', wasmOut, './cmd/tidewasm'], { GOOS: 'js', GOARCH: 'wasm', CGO_ENABLED: '0' });
const wasmExec = [join(goroot, 'lib', 'wasm', 'wasm_exec.js'), join(goroot, 'misc', 'wasm', 'wasm_exec.js')].find(existsSync);
if (!wasmExec) throw new Error('wasm_exec.js not found in GOROOT');

// 2. Hash assets, leaves first, rewriting references to already-hashed names.
const hashed = new Map(); // original basename -> hashed basename
const hash = (buf) => createHash('sha256').update(buf).digest('hex').slice(0, 10);

function rewrite(text) {
  for (const [from, to] of hashed) {
    const esc = from.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    text = text.replace(new RegExp(`(?<=[\\s'"(/])${esc}(?=[\\s'")?#])`, 'g'), to);
  }
  return text;
}

function emit(name, source, { text = false, replace = {} } = {}) {
  let buf = readFileSync(source);
  if (text) {
    let s = rewrite(buf.toString('utf8'));
    for (const [k, v] of Object.entries(replace)) s = s.split(k).join(v);
    buf = Buffer.from(s, 'utf8');
  }
  const dot = name.lastIndexOf('.');
  const out = `${name.slice(0, dot)}.${hash(buf)}${name.slice(dot)}`;
  writeFileSync(join(assets, out), buf);
  hashed.set(name, out);
  return { out, bytes: buf.length, buf };
}

const wasm = emit('tide.wasm', wasmOut);
emit('wasm_exec.js', wasmExec);
emit('source-serif-4-roman.woff2', join(web, 'fonts', 'source-serif-4-roman.woff2'));
emit('source-serif-4-italic.woff2', join(web, 'fonts', 'source-serif-4-italic.woff2'));
emit('engine-worker.js', join(web, 'engine-worker.js'), { text: true, replace: { __WASM_BYTES__: String(wasm.bytes) } });
for (const f of ['presets.js', 'render.js', 'engine.js', 'app.js', 'styles.css']) emit(f, join(web, f), { text: true });

// 3. Unhashed entry points.
writeFileSync(join(dist, 'index.html'), rewrite(readFileSync(join(web, 'index.html'), 'utf8')));
for (const f of ['_headers', 'favicon.svg']) cpSync(join(web, f), join(dist, f));
cpSync(join(web, 'fonts', 'OFL.txt'), join(dist, 'fonts-OFL.txt'));

// 4. Report sizes, measured.
const gz = gzipSync(wasm.buf, { level: 9 }).length;
const br = brotliCompressSync(wasm.buf, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } }).length;
const kib = (n) => `${(n / 1024).toFixed(0)} KiB`;
writeFileSync(join(work, 'sizes.json'), JSON.stringify({ go: goVersion, wasm: wasm.bytes, gzip9: gz, brotli11: br }, null, 2));
console.log(`${goVersion}: dist/a/${wasm.out} is ${kib(wasm.bytes)} (${wasm.bytes} bytes); gzip -9 ${kib(gz)} (${gz}); brotli 11 ${kib(br)} (${br})`);
console.log(`dist/ ready: ${[...hashed.values()].length} hashed assets in ${relative(root, assets)}/`);
