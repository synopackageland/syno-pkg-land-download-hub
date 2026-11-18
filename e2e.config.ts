import type { E2EConfig } from 'e2e';
import { dsmTarget } from '@synopackageland/cli/e2e';
import { agentOptions } from './tests/e2e/support/llm.ts';

export default {
  tests: ['tests/e2e/**/*.e2e.ts'],
  targets: [dsmTarget()],
  workers: 1,
  retries: 0,
  timeout: 900_000,
  actionTimeout: 15_000,
  output: '.cache/e2e/last',
  agents: { default: { ...agentOptions(), maxModelCalls: 40, maxSteps: 60 } },
} satisfies E2EConfig;
