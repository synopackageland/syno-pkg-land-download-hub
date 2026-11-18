// The package restarting (DSM stopping and starting it, as on an update or a
// reboot): downloads the user paused stay paused, an interrupted one resumes
// and is saved, and the package stays registered to start at boot.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { changesApp, createDownload, data, saveFolder, engine, mediumFile, hugeFile, openDownloadHub, removeFromNas, tag, task, createFolder, fileInfo } from './support/download-hub.ts';
import { packageName, removeOwnStages, restartPackage, ssh, stages } from './support/nas-shell.ts';

test('after the package restarts, a paused download stays paused and an interrupted one finishes and is saved', { session: 'admin', timeout: 900_000, skip: changesApp }, async ({ app }) => {
  let page = await openDownloadHub(app);
  const scope: string = (await data(page, 'delegate.destination', { key: 'save' })).selection.root;
  const name = `${tag}-restart-${Date.now()}`;
  const folder = await createFolder(page, scope, name);
  const ids: string[] = [];
  const before = new Set(await stages());
  const restoreFolder = await saveFolder(page);
  try {
    // The huge file is still downloading when the pause arrives.
    const paused = await createDownload(page, folder, hugeFile);
    ids.push(paused);
    await engine(page, 'PUT', `/api/v1/tasks/${paused}/pause`);
    const interrupted = await createDownload(page, folder, mediumFile);
    ids.push(interrupted);
    await expect.poll(async () => (await task(page, paused)).status, { timeout: 30_000 }).toBe('pause');
    // The restart has to meet the download itself, not the save after it.
    expect((await task(page, interrupted)).status).not.toBe('done');

    // The window closes; DSM stops and starts the package.
    await page.goto('about:blank');
    await restartPackage();
    expect(await ssh(`systemctl is-enabled pkgctl-${packageName}.service`)).toContain('enabled');

    page = await openDownloadHub(app);
    await expect.poll(async () => {
      const now = await task(page, interrupted).catch(() => undefined);
      return { status: now?.status, state: now?.publication?.state, published: now?.publication?.published === true };
    }, { timeout: 600_000, interval: 3_000 }).toMatchObject({ published: true });
    expect((await task(page, paused)).status).toBe('pause');
    expect((await fileInfo(page, `${folder}/${mediumFile.split('/').pop()}`))).toBeTruthy();
  } finally {
    for (const id of ids) await engine(page, 'DELETE', `/api/v1/tasks/${id}`).catch(() => undefined);
    await restoreFolder(page);
    await removeFromNas(page, [folder]);
    await removeOwnStages(before);
  }
});
