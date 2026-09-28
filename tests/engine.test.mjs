// Unit tests for the main-thread engine client (web/engine.js) with a fake
// worker. The real worker runs one request at a time and can't be
// interrupted, so the client must not queue every request behind it: a burst
// of changes should cost one compute in flight plus the newest, not all of
// them.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Engine } from '../web/engine.js';

class FakeWorker {
  static last = null;
  constructor(url) {
    this.url = String(url);
    this.posted = [];
    this.onmessage = null;
    this.onerror = null;
    FakeWorker.last = this;
    queueMicrotask(() => this.emit({ type: 'ready', info: {}, zones: [] }));
  }
  postMessage(msg) {
    this.posted.push(msg);
  }
  emit(data) {
    this.onmessage?.({ data });
  }
  // Answers a request the way engine-worker.js does.
  answer(msg, result = { from: msg.from }) {
    this.emit({ type: 'result', id: msg.id, json: JSON.stringify(result), ms: 5 });
  }
}

async function startEngine() {
  const saved = globalThis.Worker;
  globalThis.Worker = FakeWorker;
  try {
    const engine = new Engine();
    await engine.start();
    return { engine, worker: FakeWorker.last };
  } finally {
    globalThis.Worker = saved;
  }
}

const req = (from) => ({ expr: '* * * * *', zones: ['UTC'], mode: 'each', from, now: 0 });
const tick = () => new Promise((r) => setImmediate(r));

test('a request goes straight to an idle worker', async () => {
  const { engine, worker } = await startEngine();
  const p = engine.compute(req('2199-01'));
  assert.equal(worker.posted.length, 1);
  assert.equal(worker.posted[0].from, '2199-01');
  worker.answer(worker.posted[0]);
  const out = await p;
  assert.equal(out.stale, false);
  assert.deepEqual(out.result, { from: '2199-01' });
});

test('six rapid requests: only the one in flight and the newest reach the worker', async () => {
  const { engine, worker } = await startEngine();
  const months = ['2199-01', '2199-02', '2199-03', '2199-04', '2199-05', '2199-06'];
  const settled = new Map();
  const ps = months.map((m) => engine.compute(req(m)).then((out) => (settled.set(m, out), out)));

  // While the first computes, the rest wait on the main thread; each new one
  // replaces the one before it.
  assert.deepEqual(
    worker.posted.map((m) => m.from),
    ['2199-01'],
  );

  // The replaced ones settle at once, as stale, so no caller hangs.
  await tick();
  for (const m of ['2199-02', '2199-03', '2199-04', '2199-05']) {
    const out = settled.get(m);
    assert.ok(out, `${m} settled without reaching the worker`);
    assert.equal(out.stale, true);
    assert.equal(out.superseded, true);
    assert.equal(out.result, null);
  }
  assert.ok(!settled.has('2199-01') && !settled.has('2199-06'));

  // When the first finishes it is stale, and the newest is sent next.
  worker.answer(worker.posted[0]);
  assert.equal((await ps[0]).stale, true);
  assert.deepEqual(
    worker.posted.map((m) => m.from),
    ['2199-01', '2199-06'],
  );

  worker.answer(worker.posted[1]);
  const last = await ps[5];
  assert.equal(last.stale, false);
  assert.deepEqual(last.result, { from: '2199-06' });
  assert.equal(worker.posted.length, 2, 'at most two requests reached the worker');
});

test('an error in flight still sends the waiting request', async () => {
  const { engine, worker } = await startEngine();
  const first = engine.compute(req('2199-01'));
  const second = engine.compute(req('2199-02'));
  assert.equal(worker.posted.length, 1);
  worker.emit({ type: 'result', id: worker.posted[0].id, error: 'boom' });
  await assert.rejects(first, /boom/);
  assert.equal(worker.posted.length, 2);
  worker.answer(worker.posted[1]);
  assert.equal((await second).stale, false);
});

test('a result for a request the client is not waiting on is ignored', async () => {
  const { engine, worker } = await startEngine();
  const p = engine.compute(req('2199-01'));
  worker.emit({ type: 'result', id: 999, json: '{}', ms: 1 });
  assert.equal(worker.posted.length, 1);
  worker.answer(worker.posted[0]);
  assert.equal((await p).stale, false);
});
