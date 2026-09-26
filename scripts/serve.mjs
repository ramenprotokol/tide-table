#!/usr/bin/env node
// A tiny static server for dist/, for local preview and the browser test.
// It applies dist/_headers (Cloudflare Pages format) so local runs get the
// production Content-Security-Policy and cache rules, and serves .wasm as
// application/wasm. PORT=0 (the default) picks a free port.
import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { dirname, extname, join, normalize, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.wasm': 'application/wasm',
  '.svg': 'image/svg+xml',
  '.woff2': 'font/woff2',
  '.png': 'image/png',
  '.json': 'application/json',
  '.txt': 'text/plain; charset=utf-8',
};

// Parse _headers into [{ pattern, headers }]. Supports "/*" and "/dir/*".
export function parseHeaders(text) {
  const rules = [];
  let cur = null;
  for (const line of text.split('\n')) {
    if (!line.trim() || line.trim().startsWith('#')) continue;
    if (!/^\s/.test(line)) {
      cur = { pattern: line.trim(), headers: {} };
      rules.push(cur);
      continue;
    }
    const m = /^\s+([^:]+):\s*(.*)$/.exec(line);
    if (cur && m) cur.headers[m[1].toLowerCase()] = m[2];
  }
  return rules;
}

export function headersFor(rules, path) {
  const out = {};
  for (const r of rules) {
    const p = r.pattern;
    const hit = p.endsWith('*') ? path.startsWith(p.slice(0, -1)) : path === p;
    if (hit) Object.assign(out, r.headers);
  }
  return out;
}

export async function serve(dir, port = 0) {
  const rules = parseHeaders(await readFile(join(dir, '_headers'), 'utf8').catch(() => ''));
  const server = createServer(async (req, res) => {
    try {
      const path = decodeURIComponent(new URL(req.url, 'http://x').pathname);
      let file = normalize(join(dir, path));
      if (!file.startsWith(normalize(dir + sep)) && file !== normalize(dir)) {
        res.writeHead(403).end();
        return;
      }
      if ((await stat(file).catch(() => null))?.isDirectory()) file = join(file, 'index.html');
      const body = await readFile(file);
      const urlPath = file.slice(normalize(dir).length).split(sep).join('/') || '/';
      res.writeHead(200, { ...headersFor(rules, urlPath), 'content-type': TYPES[extname(file)] ?? 'application/octet-stream' });
      res.end(body);
    } catch {
      res.writeHead(404, { 'content-type': 'text/plain' }).end('not found');
    }
  });
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server)));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const dir = join(dirname(fileURLToPath(import.meta.url)), '..', 'dist');
  const server = await serve(dir, Number(process.env.PORT ?? 0));
  console.log(`serving dist/ at http://127.0.0.1:${server.address().port}/`);
}
