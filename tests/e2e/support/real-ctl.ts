// Invoke the installed CLI as a child process. Never replace transport,
// discovery, credential storage or the executor with a test implementation.
import { spawn } from 'node:child_process';
import { resolve } from 'node:path';
import { readFileSync } from 'node:fs';
import { prepareJSONProcess } from './prepared-json.ts';

export const commands = JSON.parse(readFileSync(new URL('../../../generated/ctl.json', import.meta.url), 'utf8')).commands;

export async function ctl(operation: string, input: unknown = {}, extra: string[] = [], packageId = 'SynoPkgGopeed'): Promise<any> {
  const command = commands[operation];
  if (!command) throw new Error(`Unregistered operation: ${operation}`);
  const host = process.env.E2E_CTL_HOST;
  if (!host) throw new Error('E2E_CTL_HOST must name an approved real NAS host');
  const args = [resolve('node_modules/@synopackageland/cli/dist/bin.js'), 'ctl', packageId,
    ...command.cli, '--host', host, '--input', '-', '--json',
    ...(command.effect === 'read' ? [] : ['--yes']), ...extra];
  const reply = await new Promise<any>((accept, reject) => {
    const child = spawn(process.execPath, args, { stdio: ['pipe', 'pipe', 'pipe'] });
    let out = '';
    // stderr can contain connection/account information; never copy it to reports.
    child.stdout.on('data', chunk => { out += chunk; });
    child.stderr.resume();
    child.on('error', reject);
    child.on('close', () => {
      try { accept(JSON.parse(out)); }
      catch { reject(new Error(`${operation}: CLI did not return JSON`)); }
    });
    child.stdin.end(JSON.stringify(input));
  });
  if (!reply.ok) throw new Error(`${operation}: ${reply.error?.code ?? 'CLI_ERROR'}${reply.error?.message ? ` (${reply.error.message})` : ''}`);
  return reply.data;
}

// Reach the CLI's documented input read after actual discovery before creating
// a short-lived search target. The same real CLI then sends the cancellation.
export async function prepareSearchCancellation() {
  const host = process.env.E2E_CTL_HOST;
  if (!host) throw new Error('E2E_CTL_HOST must name an approved real NAS host');
  const operation = 'search.jobs.cancel', command = commands[operation];
  const prepared = await prepareJSONProcess(input => [resolve('node_modules/@synopackageland/cli/dist/bin.js'),
    'ctl', 'SynoPkgGopeed', ...command.cli, '--host', host, '--input', input, '--json', '--yes']);
  return {
    async cancel(id: string) {
      const reply = await prepared.send({ id });
      if (!reply.ok) throw new Error(`${operation}: ${reply.error?.code ?? 'CLI_ERROR'} (${reply.error?.message ?? ''})`);
      return reply.data;
    },
    close: prepared.close,
  };
}

export function fixtureURL(path: string): string {
  const base = process.env.E2E_FIXTURE_URL;
  if (!base) throw new Error('E2E_FIXTURE_URL must serve the test-created fixture server over public HTTPS');
  return new URL(path, base).href;
}
