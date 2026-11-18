import { test } from 'node:test';
import assert from 'node:assert/strict';
import { withCleanup } from './cleanup.ts';

const fail = (message: string) => async () => { throw new Error(message); };

test('every cleanup step runs, and a failing body reports its own error, not a cleanup one', async () => {
  const ran: string[] = [];
  await assert.rejects(withCleanup(fail('body'), async step => {
    await step(async () => { ran.push('first'); throw new Error('cleanup'); });
    await step(async () => { ran.push('second'); });
  }), { message: 'body' });
  assert.deepEqual(ran, ['first', 'second']);
});

test('after a passing body, the first failing cleanup step is reported', async () => {
  await assert.rejects(withCleanup(async () => {}, async step => {
    await step(fail('first'));
    await step(fail('second'));
  }), { message: 'first' });
});

test('a passing body with clean steps resolves', async () => {
  await withCleanup(async () => {}, async step => { await step(async () => {}); });
});
