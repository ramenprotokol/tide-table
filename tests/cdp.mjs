// A minimal Chrome DevTools Protocol driver for the browser test and for
// screenshots. It launches headless Chrome on a free debugging port, opens
// pages with exact device emulation (headless Chrome won't size a window
// below 500 px), and records console errors, uncaught exceptions, failed
// requests and CSP violations.
import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// An explicit CHROME_PATH wins; if it points at nothing there is no Chrome
// (rather than quietly falling back to another one).
export function findChrome(env = process.env) {
  if (env.CHROME_PATH) return existsSync(env.CHROME_PATH) ? env.CHROME_PATH : null;
  return (
    [
      '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
      '/Applications/Chromium.app/Contents/MacOS/Chromium',
      '/usr/bin/google-chrome',
      '/usr/bin/google-chrome-stable',
      '/usr/bin/chromium',
      '/usr/bin/chromium-browser',
    ].find((p) => existsSync(p)) ?? null
  );
}

// Run, skip or fail: a missing Chrome is a skip locally, a failure when
// REQUIRE_BROWSER=1 (set that in CI).
export function browserPlan(chromePath, env = process.env) {
  if (chromePath) return { run: true };
  if (env.REQUIRE_BROWSER === '1') return { fail: 'REQUIRE_BROWSER=1 is set but Chrome was not found. Install Chrome or set CHROME_PATH.' };
  return { skip: 'Chrome not found; set CHROME_PATH to run the browser test (REQUIRE_BROWSER=1 makes this a failure)' };
}

export async function launchChrome(chromePath) {
  const profile = mkdtempSync(join(tmpdir(), 'tide-table-chrome-'));
  const proc = spawn(
    chromePath,
    ['--headless=new', '--remote-debugging-port=0', `--user-data-dir=${profile}`, '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--hide-scrollbars', '--force-color-profile=srgb', 'about:blank'],
    { stdio: ['ignore', 'ignore', 'pipe'] },
  );
  const wsUrl = await new Promise((resolve, reject) => {
    let buf = '';
    const timer = setTimeout(() => reject(new Error('Chrome did not start within 20 s')), 20000);
    proc.stderr.on('data', (d) => {
      buf += d;
      const m = /DevTools listening on (ws:\/\/\S+)/.exec(buf);
      if (m) {
        clearTimeout(timer);
        resolve(m[1]);
      }
    });
    proc.once('exit', (code) => reject(new Error(`Chrome exited early (${code})`)));
  });

  const ws = new WebSocket(wsUrl);
  await new Promise((resolve, reject) => {
    ws.onopen = resolve;
    ws.onerror = reject;
  });
  let nextId = 0;
  const pending = new Map();
  const listeners = new Set();
  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) {
      const p = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) p.reject(new Error(msg.error.message));
      else p.resolve(msg.result);
    } else {
      for (const l of listeners) l(msg);
    }
  };
  const send = (method, params = {}, sessionId) => {
    const id = ++nextId;
    ws.send(JSON.stringify({ id, method, params, sessionId }));
    return new Promise((resolve, reject) => pending.set(id, { resolve, reject }));
  };

  async function openPage({ width, height, mobile = false, scale = 1, scheme, reducedMotion }) {
    const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
    const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
    const s = (method, params) => send(method, params, sessionId);
    const problems = [];
    const sessions = new Set([sessionId]); // the page plus its workers
    const onEvent = (msg) => {
      if (!sessions.has(msg.sessionId)) return;
      if (msg.method === 'Target.attachedToTarget') {
        const child = msg.params.sessionId;
        sessions.add(child);
        for (const m of ['Runtime.enable', 'Log.enable', 'Network.enable']) send(m, {}, child).catch(() => {});
        return;
      }
      if (msg.method === 'Runtime.exceptionThrown') {
        const d = msg.params.exceptionDetails;
        problems.push({ kind: 'exception', text: d.exception?.description ?? d.text });
      } else if (msg.method === 'Runtime.consoleAPICalled' && (msg.params.type === 'error' || msg.params.type === 'warning' || msg.params.type === 'assert')) {
        problems.push({ kind: `console.${msg.params.type}`, text: msg.params.args.map((a) => a.value ?? a.description).join(' ') });
      } else if (msg.method === 'Log.entryAdded' && (msg.params.entry.level === 'error' || msg.params.entry.level === 'warning')) {
        problems.push({ kind: `log.${msg.params.entry.level}`, text: msg.params.entry.text, url: msg.params.entry.url ?? '' });
      } else if (msg.method === 'Network.loadingFailed' && !msg.params.canceled) {
        problems.push({ kind: 'request failed', text: msg.params.errorText });
      } else if (msg.method === 'Network.responseReceived' && msg.params.response.status >= 400) {
        problems.push({ kind: 'http', text: `${msg.params.response.status} ${msg.params.response.url}` });
      }
    };
    listeners.add(onEvent);
    await s('Page.enable');
    await s('Runtime.enable');
    await s('Log.enable');
    await s('Network.enable');
    // Worker targets (the engine) report their console through auto-attach.
    await s('Target.setAutoAttach', { autoAttach: true, waitForDebuggerOnStart: false, flatten: true });
    await s('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: scale, mobile });
    // A headless page never has focus, and the async clipboard API refuses
    // unfocused documents; emulate focus so it behaves as in a real window.
    await s('Emulation.setFocusEmulationEnabled', { enabled: true });
    const features = [];
    if (scheme) features.push({ name: 'prefers-color-scheme', value: scheme });
    if (reducedMotion) features.push({ name: 'prefers-reduced-motion', value: reducedMotion });
    if (features.length) await s('Emulation.setEmulatedMedia', { features });

    const evaluate = async (expression) => {
      const r = await s('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description ?? r.exceptionDetails.text);
      return r.result.value;
    };
    const waitFor = async (expression, timeout = 20000) => {
      const end = Date.now() + timeout;
      while (Date.now() < end) {
        if (await evaluate(expression).catch(() => false)) return true;
        await new Promise((r) => setTimeout(r, 100));
      }
      throw new Error(`timed out waiting for: ${expression}`);
    };
    const key = async (keyName, code, keyCode, modifiers = 0) => {
      await s('Input.dispatchKeyEvent', { type: 'keyDown', key: keyName, code, windowsVirtualKeyCode: keyCode, modifiers });
      await s('Input.dispatchKeyEvent', { type: 'keyUp', key: keyName, code, windowsVirtualKeyCode: keyCode, modifiers });
    };
    return {
      problems,
      evaluate,
      waitFor,
      key,
      insertText: (text) => s('Input.insertText', { text }),
      navigate: (url) => s('Page.navigate', { url }),
      screenshot: async (full = false) => {
        let clip;
        if (full) {
          const m = await s('Page.getLayoutMetrics');
          const size = m.cssContentSize ?? m.contentSize;
          clip = { x: 0, y: 0, width, height: Math.ceil(size.height), scale: 1 };
        }
        const r = await s('Page.captureScreenshot', { format: 'png', captureBeyondViewport: !!full, ...(clip ? { clip } : {}) });
        return Buffer.from(r.data, 'base64');
      },
      close: async () => {
        listeners.delete(onEvent);
        await send('Target.closeTarget', { targetId }).catch(() => {});
      },
    };
  }

  async function close() {
    try {
      ws.close();
    } catch {
      /* already closed */
    }
    proc.kill();
    await new Promise((r) => (proc.exitCode !== null ? r() : proc.once('exit', r)));
    rmSync(profile, { recursive: true, force: true });
  }

  // Grants browser permissions (such as the clipboard) to an origin.
  const grant = (origin, permissions) => send('Browser.grantPermissions', { origin, permissions });

  return { openPage, close, grant };
}
