import type { E2EConfig } from 'e2e';
import { dsmTarget } from '@synopackageland/cli/e2e';

// npm run e2e:smoke: the App-open smoke only, apart from the full suite.
export default {
  tests: ['tests/smoke/*.e2e.ts'],
  targets: [dsmTarget()],
  workers: 1,
  retries: 0,
  output: '.cache/e2e/smoke',
} satisfies E2EConfig;
