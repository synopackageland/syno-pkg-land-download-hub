import { test } from 'node:test';
import assert from 'node:assert/strict';
import { observe, settleSearchJob } from './work-poll.ts';

test('each observation completes before counting work and continuity checks cannot be swallowed', async () => {
  let reads = 0;
  assert.equal(await observe('progress', async () => ++reads, value => value === 3, { maxReads: 3, intervalMs: 0 }), 3);
  assert.equal(reads, 3);
  await assert.rejects(observe('stuck', async () => 0, value => value > 0, { maxReads: 2, intervalMs: 0 }), /after 2 completed reads/);
  await assert.rejects(observe('resume', async () => 1, () => false, { maxReads: 2, check: value => assert.ok(value >= 4) }), /false == true|falsy/);
});

test('terminal search cleanup is read-only; a live job is canceled once and observed to terminal', async () => {
  const calls: string[] = [];
  await settleSearchJob(async operation => { calls.push(operation); return { state: 'succeeded' }; }, 'done');
  assert.deepEqual(calls, ['search.jobs.status']);
  calls.length = 0;
  const states = ['running', 'running', 'canceled'];
  await settleSearchJob(async operation => { calls.push(operation); return { state: operation.endsWith('status') ? states.shift() : 'running' }; }, 'live');
  assert.deepEqual(calls, ['search.jobs.status', 'search.jobs.cancel', 'search.jobs.status', 'search.jobs.status']);
});
