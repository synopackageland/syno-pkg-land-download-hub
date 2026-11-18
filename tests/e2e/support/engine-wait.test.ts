import { test } from 'node:test';
import assert from 'node:assert/strict';
import { whileEngineStarts } from './engine-wait.ts';

const replies = (...outcomes: (string | Error)[]) => {
  let calls = 0;
  const read = async () => { const outcome = outcomes[calls++]; if (outcome instanceof Error) throw outcome; return outcome; };
  return { read, calls: () => calls };
};

test('a read refused while the engine starts is repeated until it answers', async () => {
  const { read, calls } = replies(new Error('gopeed.plugins.list refused: engine_busy'), new Error('gopeed.plugins.delete refused: engine_unavailable'), 'listed');
  assert.equal(await whileEngineStarts(read, { timeout: 1_000, interval: 1 }), 'listed');
  assert.equal(calls(), 3);
});

test('any other refusal is reported at once', async () => {
  const { read, calls } = replies(new Error('gopeed.plugins.delete refused: plugin_not_found'), 'listed');
  await assert.rejects(whileEngineStarts(read, { timeout: 1_000, interval: 1 }), /plugin_not_found/);
  assert.equal(calls(), 1);
});

test('an engine that never answers is reported after the timeout with its last refusal', async () => {
  const read = async () => { throw new Error('x refused: engine_busy'); };
  await assert.rejects(whileEngineStarts(read, { timeout: 20, interval: 5 }), /engine_busy/);
});
