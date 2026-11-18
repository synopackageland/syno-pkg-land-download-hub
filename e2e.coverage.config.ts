import type { E2EConfig } from 'e2e';
import { dsmTarget } from '@synopackageland/cli/e2e';
import { agentOptions } from './tests/e2e/support/llm.ts';

export default {
  tests: ['tests/coverage/**/*.e2e.ts', 'tests/e2e/7*-real-*.e2e.ts', 'tests/e2e/45-search.e2e.ts'],
  targets: [dsmTarget()], workers: 1, retries: 0,
  // The framework requires a positive attempt deadline. Long-job waits use
  // completed-read limits instead; this is only the timer's maximum range.
  timeout: 2_147_483_647, actionTimeout: 15_000,
  output: '.cache/e2e/coverage',
  agents: { default: { ...agentOptions(), maxModelCalls: 40, maxSteps: 60 } },
} satisfies E2EConfig;
