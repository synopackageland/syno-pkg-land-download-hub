import { spawn, execFileSync } from 'node:child_process';
import { constants, openSync, closeSync, writeSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { observe } from './work-poll.ts';

// The CLI opens its input after real authentication/discovery. A private FIFO
// provides a readiness handshake before admitting a short-lived cancel target.
// No command, transport, descriptor or executor is replaced.
export async function prepareJSONProcess(args: (inputFile: string) => string[]) {
  const dir = mkdtempSync(join(tmpdir(), 'download-hub-ctl-input-'));
  const input = join(dir, 'input');
  execFileSync('mkfifo', ['-m', '600', input]);
  const child = spawn(process.execPath, args(input), { stdio: ['ignore', 'pipe', 'pipe'] });
  let out = '', spawnError: Error | undefined, fd: number | undefined;
  child.stdout.on('data', chunk => { out += chunk; });
  child.stderr.resume();
  const reply = new Promise<any>((accept, reject) => {
    child.on('error', error => { spawnError = error; reject(error); });
    child.on('close', () => {
      try { accept(JSON.parse(out)); }
      catch { reject(new Error('Prepared command did not return JSON')); }
    });
  });
  void reply.catch(() => undefined);
  async function close() {
    if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM');
    if (fd !== undefined) { closeSync(fd); fd = undefined; }
    await reply.catch(() => undefined);
    rmSync(dir, { recursive: true, force: true });
  }
  try {
    fd = await observe('CLI input readiness', async () => {
      if (spawnError) throw spawnError;
      if (child.exitCode !== null || child.signalCode !== null) throw new Error('Prepared CLI exited before reading its input');
      try { return openSync(input, constants.O_WRONLY | constants.O_NONBLOCK); }
      catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENXIO') return undefined; throw error; }
    }, value => value !== undefined, { maxReads: 600 });
  } catch (error) { await close(); throw error; }
  return {
    async send(value: unknown) {
      if (fd === undefined) throw new Error('Prepared CLI input has already been sent');
      writeSync(fd, JSON.stringify(value)); closeSync(fd); fd = undefined;
      return reply;
    },
    close,
  };
}
