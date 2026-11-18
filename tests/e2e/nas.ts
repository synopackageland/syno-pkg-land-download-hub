// pnpm run e2e:nas [-- e2e run options]: runs tests/e2e on test-nas through the
// CLI's SSH tunnel (never a direct connection), then prints each test's time
// and the run's LLM requests. The tunnel masks DSM's secrets in e2e's output
// and, as DSM_E2E_OUTPUT_DIR, makes the run directory private and masks them there.
import { existsSync } from 'node:fs';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';
import { runWithTunnel } from '@synopackageland/cli/e2e';
import { summarize } from './support/summary.ts';

const root = resolve(import.meta.dirname, '../..');
if (existsSync(join(root, '.env'))) process.loadEnvFile(join(root, '.env'));
const secrets = join(homedir(), '.config/download-hub-e2e/accounts');
const runDir = join(root, '.cache/e2e/runs', new Date().toISOString().replace(/[:.]/g, '-'));
const calls = join(runDir, 'llm-calls.jsonl');

await mkdir(runDir, { recursive: true, mode: 0o700 });
const env = {
  ...process.env,
  DSM_E2E_ACCOUNT_ADMIN: process.env.DSM_E2E_ACCOUNT_ADMIN ?? join(secrets, 'test-nas-admin.json'),
  DSM_E2E_ACCOUNT_NORMAL1: process.env.DSM_E2E_ACCOUNT_NORMAL1 ?? join(secrets, 'test-nas-normal-users.json'),
  E2E_LLM_CALLS: calls,
  E2E_RUN_DIR: runDir,
  DSM_E2E_OUTPUT_DIR: runDir,
};
let code = await runWithTunnel([process.execPath, join(root, 'node_modules/e2e/dist/cli/bin.js'), 'run',
  '--config', 'e2e.config.ts', '--output', runDir, ...process.argv.slice(2)], env);
// e2e writes no report when it stops before running anything (a configuration error).
const report = await readFile(join(runDir, 'report.json'), 'utf8').then(JSON.parse, () => null);
if (!report) process.exit(code || 1);
const lines = (await readFile(calls, 'utf8').catch(() => '')).split('\n').filter(Boolean);
const summary = summarize(report, lines.map(line => JSON.parse(line)));
await writeFile(join(runDir, 'summary.json'), JSON.stringify(summary, null, 2) + '\n');
console.table(summary.tests);
console.log('LLM', summary.llm, '\nEvidence:', runDir);
if (summary.llm.withoutMaxEffort) {
  console.error(`${summary.llm.withoutMaxEffort} LLM requests did not carry reasoning effort max`);
  code ||= 1;
}
process.exitCode = code;
