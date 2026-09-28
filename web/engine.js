// Main-thread client for the Go engine running in a Web Worker.

export class Engine {
  constructor({ onProgress } = {}) {
    this.onProgress = onProgress ?? (() => {});
    this.seq = 0;
    this.inflight = null; // the request the worker is computing
    this.queued = null; // the newest request waiting for it
    this.worker = null;
    this.ready = null;
  }

  start() {
    if (this.ready) return this.ready;
    this.ready = new Promise((resolve, reject) => {
      let w;
      try {
        w = new Worker(new URL('./engine-worker.js', import.meta.url));
      } catch (err) {
        reject(new Error(`this browser could not start a worker (${err.message})`));
        return;
      }
      this.worker = w;
      w.onmessage = (ev) => {
        const m = ev.data;
        if (m.type === 'progress') this.onProgress(m.loaded, m.total);
        else if (m.type === 'ready') resolve(m);
        else if (m.type === 'failed') reject(new Error(m.message));
        else if (m.type === 'result') {
          const p = this.inflight;
          if (!p || p.id !== m.id) return;
          this.inflight = null;
          this.send();
          if (m.error) p.reject(new Error(m.error));
          else p.resolve({ result: JSON.parse(m.json), ms: m.ms, stale: m.id !== this.seq });
        }
      };
      w.onerror = (ev) => {
        ev.preventDefault();
        reject(new Error(ev.message || 'the engine worker failed to load'));
      };
    });
    return this.ready;
  }

  // compute resolves with { result, ms, stale }; stale is true when a newer
  // request was made before this one finished. The worker computes one
  // request at a time and can't be interrupted, so only the newest request
  // waits for it: an older one still waiting is dropped, resolving at once
  // with { result: null, stale: true, superseded: true }.
  compute({ expr, zones, mode, from, now = Date.now() }) {
    const id = ++this.seq;
    return new Promise((resolve, reject) => {
      this.queued?.resolve({ result: null, ms: 0, stale: true, superseded: true });
      this.queued = { id, msg: { id, expr, zones, mode, from, now }, resolve, reject };
      this.send();
    });
  }

  // Sends the waiting request if the worker is free.
  send() {
    if (this.inflight || !this.queued) return;
    this.inflight = this.queued;
    this.queued = null;
    this.worker.postMessage(this.inflight.msg);
  }
}
