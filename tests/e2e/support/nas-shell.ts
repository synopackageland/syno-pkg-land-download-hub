// Commands on the NAS itself, over the same SSH login the tunnel uses: the
// package's stop and start, and fixtures that only work with it stopped.
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const run = promisify(execFile);
const host = () => process.env.DSM_E2E_SSH || 'root@nas.example.invalid';
const options = ['-o', 'BatchMode=yes', '-o', 'LogLevel=ERROR'];

export const packageName = 'SynoPkgGopeed';
/** Download Hub's engine database (Gopeed's bbolt file). */
export const engineDatabase = `/volume1/@appdata/${packageName}/gopeed/storage/gopeed.db`;

/** Runs a shell command on the NAS and returns its output. */
export async function ssh(command: string, timeout = 120_000): Promise<string> {
  return (await run('ssh', [...options, host(), command], { encoding: 'utf8', timeout })).stdout;
}

/** Copies a local file to the NAS. */
export async function copyToNas(local: string, remote: string): Promise<void> {
  // -O: DSM closes SFTP sessions unless File Services enables SFTP.
  await run('scp', [...options, '-O', '-q', local, `${host()}:${remote}`], { timeout: 120_000 });
}

/** Stops Download Hub, runs `whileStopped` on the NAS, and starts it again. */
export async function restartPackage(whileStopped = 'true'): Promise<string> {
  return ssh(`/usr/syno/bin/synopkg stop ${packageName} && ${whileStopped} && /usr/syno/bin/synopkg start ${packageName}`, 300_000);
}

/** Where the App stages downloads before the executor saves them. */
export const stagingDir = `/volume1/@appdata/${packageName}/executor-staging`;

/** The stage folders now in the staging directory (read only). */
export async function stages(): Promise<string[]> {
  return (await ssh(`ls -1 ${stagingDir} 2>/dev/null || true`)).split('\n').filter(Boolean);
}

/**
 * Removes the stages a test's own downloads left once their records were
 * deleted: those not in `before`, taken when the test started.
 */
export async function removeOwnStages(before: Set<string>): Promise<void> {
  const left = (await stages()).filter(stage => !before.has(stage) && /^[0-9a-f]{32}$/.test(stage));
  if (left.length) await ssh(`rm -rf ${left.map(stage => `${stagingDir}/${stage}`).join(' ')}`);
}
