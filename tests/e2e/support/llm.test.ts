import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { generateText } from 'ai';
import { agentOptions, magpie } from './llm.ts';

function upstream(requests: unknown[]) {
  return async (_url: unknown, init?: RequestInit) => {
    requests.push(JSON.parse(String(init?.body)));
    return Response.json({
      id: 'r', object: 'chat.completion', created: 0, model: 'codex/gpt-6-luna',
      choices: [{ index: 0, message: { role: 'assistant', content: 'ok' }, finish_reason: 'stop' }],
      usage: { prompt_tokens: 120, completion_tokens: 7, total_tokens: 127, prompt_tokens_details: { cached_tokens: 100 } },
    });
  };
}

test('the agent sends luna at max reasoning effort and records each call', async () => {
  const requests: any[] = [];
  const log = join(await mkdtemp(join(tmpdir(), 'llm-')), 'llm-calls.jsonl');
  const model = magpie({ E2E_LLM_CALLS: log }, upstream(requests));
  await generateText({ model, prompt: 'hi', providerOptions: agentOptions({}).providerOptions });
  assert.equal(requests[0].model, 'codex/gpt-6-luna');
  assert.equal(requests[0].reasoning_effort, 'max');
  const [call] = (await readFile(log, 'utf8')).trim().split('\n').map(line => JSON.parse(line));
  assert.deepEqual({ ...call, time: undefined },
    { model: 'codex/gpt-6-luna', reasoningEffort: 'max', inputTokens: 120, cachedTokens: 100, outputTokens: 7, time: undefined });
});

test('the model and effort follow the environment', () => {
  assert.deepEqual(agentOptions({ E2E_LLM_REASONING_EFFORT: 'high' }).providerOptions, { openai: { reasoningEffort: 'high' } });
});
