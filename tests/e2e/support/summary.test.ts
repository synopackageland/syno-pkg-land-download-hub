import { test } from 'node:test';
import assert from 'node:assert/strict';
import { summarize } from './summary.ts';

const step = (api: string, modelCalls?: number) => ({ api, ...(modelCalls === undefined ? {} : { metrics: { modelCalls } }) });
const report = { run: { results: [
  { titlePath: ['admin signs in to DSM'], file: 'tests/e2e/auth.setup.e2e.ts', status: 'passed',
    attempts: [{ durationMs: 2000, steps: [step('app.open')] }] },
  { titlePath: ['downloads', 'a link downloads'], file: 'tests/e2e/downloads.e2e.ts', status: 'failed',
    attempts: [{ durationMs: 61000, steps: [step('agent.act', 5), step('agent.assert', 1), step('expect')] }] },
] } };
const calls = [
  { reasoningEffort: 'max', inputTokens: 100, cachedTokens: 60, outputTokens: 10 },
  { reasoningEffort: null, inputTokens: 50, cachedTokens: null, outputTokens: 5 },
];

test('each test reports its time and agent calls; the run totals the LLM requests', () => {
  assert.deepEqual(summarize(report, calls), {
    tests: [
      { file: 'auth.setup.e2e.ts', title: 'admin signs in to DSM', status: 'passed', seconds: 2, agentCalls: 0 },
      { file: 'downloads.e2e.ts', title: 'downloads › a link downloads', status: 'failed', seconds: 61, agentCalls: 6 },
    ],
    llm: { requests: 2, withoutMaxEffort: 1, inputTokens: 150, cachedTokens: 60, outputTokens: 15 },
  });
});
