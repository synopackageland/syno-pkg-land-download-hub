// Upgrading an extension: the official sample extension is made to look like
// an older version (tests/fixtures/plugin-seed, with the package stopped), so
// the Extensions page offers its current version. A failed update restores the
// previous version; a good one keeps its settings and its enabled state.
// Version 0.9.0 is synthetic: the sample's unchanged code with older metadata.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { execFileSync } from 'node:child_process';
import { join, resolve } from 'node:path';
import type { Page } from 'playwright';
import { changesApp, confirm, createDownload, data, engine, goTo, mediumFile, openDownloadHub, removeFromNas, saveFolder, tag, task, tasks, text, createFolder, fileInfo } from './support/download-hub.ts';
import { withCleanup } from './support/cleanup.ts';
import { whileEngineStarts } from './support/engine-wait.ts';
import { copyToNas, engineDatabase, removeOwnStages, restartPackage, ssh, stages } from './support/nas-shell.ts';

const id = 'gopeed@github-release';
const sample = 'https://github.com/GopeedLab/gopeed-extension-samples#github-release-sample';
const repo = resolve(import.meta.dirname, '../..');
const seed = `/volume1/@tmp/${tag}-plugin-seed`;

/** An extension call, repeated while a package restart (reseed) or an update restarts the engine. */
const plugins = (page: Page, method: string, params: unknown = {}) => whileEngineStarts(() => data(page, `gopeed.plugins.${method}`, params));
const record = async (page: Page) => (await plugins(page, 'list') as any[]).find(plugin => plugin.identity === id);
const ua = (plugin: any) => plugin?.settings?.find((field: any) => field.name === 'ua')?.value;
/** Rewrites the sample's metadata while the package is stopped. */
const reseed = (mode: 'old' | 'broken-source' | 'restore') => restartPackage(`${seed} ${engineDatabase} ${mode}`);

const api = '**/SynoPkgGopeed/cgi/api.cgi*';
/** Makes this page's update checks offer `version`, the way a check before the source changed did. Only this browser sees it. */
async function offerUpdate(page: Page, version: string): Promise<void> {
  await page.route(api, async route => {
    const request = JSON.parse(route.request().postData() || '{}');
    if (request.method !== 'gopeed.plugins.check' || request.params?.id !== id) return route.fallback();
    await route.fulfill({ json: { success: true, data: { newVersion: version } } });
  });
}

/**
 * Waits until this page offers the update, as its own check on opening does.
 * Refresh is pressed while the list is empty and Check extension updates
 * whenever it takes input, as a user would. A timeout names its stage and what
 * the page showed at the last poll: the failure screenshot is taken after the
 * cleanup has reopened the page, so the screen at the timeout is saved too.
 */
async function offered(stage: string, page: Page, row: () => ReturnType<Page['locator']>, update: () => ReturnType<Page['locator']>): Promise<void> {
  const check = page.getByRole('button', { name: text('Check extension updates', '檢查擴充套件更新') });
  const refresh = page.getByRole('button', { name: text('Refresh', '重新整理') });
  let seen: Record<string, unknown> = {};
  const started = Date.now();
  try {
    await expect.poll(async () => {
      seen = {
        seconds: Math.round((Date.now() - started) / 1000),
        rows: await row().count(),
        updates: await update().count(),
        enabled: await update().isEnabled({ timeout: 1_000 }).catch(() => false),
        check: await check.isEnabled({ timeout: 1_000 }).catch(() => false),
        row: await row().first().innerText({ timeout: 1_000 }).catch(() => ''),
        notices: await page.locator('.ui-callout').allInnerTexts().catch(() => []),
      };
      console.log(`[90 ${stage}]`, JSON.stringify(seen));
      if (seen.enabled) return true;
      if (!seen.rows) await refresh.click({ timeout: 2_000 }).catch(() => undefined);
      else if (seen.check) await check.click({ timeout: 2_000 }).catch(() => undefined);
      return false;
    }, { timeout: 180_000, interval: 5_000 }).toBe(true);
  } catch (error) {
    const shot = join(process.env.E2E_RUN_DIR ?? '.cache/e2e', `90-${stage.replace(/\W+/g, '-')}.png`);
    await page.screenshot({ path: shot }).catch(() => undefined);
    throw new Error(`offered() timed out at stage "${stage}" (screen: ${shot}); last poll: ${JSON.stringify(seen)}`, { cause: error });
  }
}

/** Opens the Extensions page; prepare runs before it, ahead of the page's own checks. */
async function extensionsPage(app: Parameters<typeof openDownloadHub>[0], prepare?: (page: Page) => Promise<void>): Promise<Page> {
  const page = await openDownloadHub(app);
  await prepare?.(page);
  await goTo(page, 'Extensions', '擴充套件');
  return page;
}

const marker = `Download-Hub-${tag}`;

/** The Extensions page and the sample extension's row on it. */
type Seeded = { page: Page; original: any; row(): ReturnType<Page['locator']>; update(): ReturnType<Page['locator']> };

/**
 * Runs body on a freshly opened Extensions page with the sample extension
 * installed, enabled, carrying this run's marker setting and seeded as 0.9.0;
 * then puts the extension, its settings and its state back. body may reopen
 * the page through seeded.page.
 */
async function withOldExtension(app: Parameters<typeof openDownloadHub>[0], body: (seeded: Seeded) => Promise<void>): Promise<void> {
  const binary = join(repo, '.cache/e2e/plugin-seed');
  execFileSync('go', ['build', '-C', join(repo, 'tests/fixtures/plugin-seed'), '-o', binary, '.'],
    { env: { ...process.env, GOOS: 'linux', GOARCH: 'amd64', CGO_ENABLED: '0' }, stdio: 'inherit' });
  await copyToNas(binary, seed);
  let page = await extensionsPage(app);
  // The engine updates extensions only while no download runs or is queued
  // (downloads_running); a paused one resumes with the new version.
  await expect.poll(async () => (await tasks(page)).filter(t => !['done', 'error', 'pause'].includes(t.status)).length, { timeout: 120_000 }).toBe(0);
  const before = await record(page);
  if (!before) await plugins(page, 'install', { url: sample, consent: true });
  const original = before ?? await record(page);
  const seeded: Seeded = {
    page,
    original,
    row: () => seeded.page.locator('[data-section=installed] [role=list] > *').filter({ hasText: original.title || original.name }),
    update: () => seeded.row().getByRole('button', { name: text('Update', '更新') }),
  };
  await withCleanup(async () => {
    await plugins(page, 'settings', { id, settings: { ua: marker } });
    if (original.disabled) await plugins(page, 'switch', { id, enabled: true, consent: true });
    await reseed('old');
    seeded.page = await extensionsPage(app);
    await body(seeded);
  }, async step => {
    // Every step runs; a failing body is reported before any of them.
    page = seeded.page;
    if ((await record(page).catch(() => undefined))?.version === '0.9.0') {
      await step(() => reseed('restore'));
      await step(async () => { page = await extensionsPage(app); });
      if (before) await step(() => plugins(page, 'update', { id, version: original.version, consent: true }));
    }
    if (!before) await step(() => plugins(page, 'delete', { id, consent: true }));
    else {
      if (ua(before) !== undefined) await step(() => plugins(page, 'settings', { id, settings: { ua: ua(before) } }));
      await step(() => plugins(page, 'switch', { id, enabled: !before.disabled, consent: true }));
    }
    await step(() => ssh(`rm -f ${seed}`));
  });
}

test('an extension update that fails restores the previous version; a good one keeps its settings and state', { session: 'admin', timeout: 900_000, skip: changesApp }, async ({ app }) => {
  await withOldExtension(app, async seeded => {
    const { original, row, update } = seeded;
    await offered('old 0.9.0 offered', seeded.page, row, update);
    expect(await row().innerText()).toContain(original.version);
    expect(await row().innerText()).toContain('0.9.0');

    // The source disappears after a check offered the update: the update
    // fails and 0.9.0 comes back as it was. A check now finds no source
    // (plugin_source_denied), so this page's checks answer what the earlier
    // one did, and the engine meets the missing source. The package restart
    // closes Download Hub's DSM window: the page opens again, stubbed first.
    await reseed('broken-source');
    let page = seeded.page = await extensionsPage(app, page => offerUpdate(page, original.version));
    await expect.poll(async () => (await record(page).catch(() => undefined))?.version, { timeout: 60_000 }).toBe('0.9.0');
    await offered('broken source, stubbed offer', page, row, update);
    await update().click();
    await confirm(page);
    await page.unroute(api);
    await expect.poll(async () => (await record(page))?.updateError, { timeout: 180_000 }).toBe('plugin_update_failed_previous_restored');
    expect(await record(page)).toMatchObject({ version: '0.9.0', disabled: false });
    expect(ua(await record(page))).toBe(marker);

    // With the source back, the update installs and keeps settings and state.
    await reseed('restore');
    page = seeded.page = await extensionsPage(app);
    await offered('source restored, new page', page, row, update);
    await update().click();
    await confirm(page);
    await expect.poll(async () => (await record(page))?.version, { timeout: 180_000 }).toBe(original.version);
    expect(await record(page)).toMatchObject({ disabled: false });
    expect(ua(await record(page))).toBe(marker);
    expect((await record(page)).updateError || '').toBe('');
    await expect.poll(() => update().count()).toBe(0);
  });
});

// regression fixture decision: a paused download does not hold an extension update, and
// it resumes with the new version. 0.9.0 is the sample's own code with older
// metadata, so "the new version" shows as the engine's extension record when
// the download resumes, not as different behaviour.
test('with a download paused, an extension update succeeds and the download resumes and is saved under the new version', { session: 'admin', timeout: 1_200_000, skip: changesApp }, async ({ app }) => {
  await withOldExtension(app, async seeded => {
    const { original, row, update } = seeded;
    let page = seeded.page;
    const scope: string = (await data(page, 'delegate.destination', { key: 'save' })).selection.root;
    const name = `${tag}-paused-update-${Date.now()}`;
    const folder = await createFolder(page, scope, name);
    const staged = new Set(await stages());
    const restoreFolder = await saveFolder(page);
    let paused = '';
    try {
      paused = await createDownload(page, folder, mediumFile);
      await engine(page, 'PUT', `/api/v1/tasks/${paused}/pause`);
      await expect.poll(async () => (await task(page, paused)).status, { timeout: 30_000 }).toBe('pause');

      await offered('paused download present', page, row, update);
      await update().click();
      await confirm(page);
      await expect.poll(async () => (await record(page))?.version, { timeout: 180_000 }).toBe(original.version);
      expect((await record(page)).updateError || '').toBe('');
      expect((await task(page, paused)).status).toBe('pause');

      page = seeded.page = await extensionsPage(app);
      await engine(page, 'PUT', `/api/v1/tasks/${paused}/continue`);
      expect((await record(page))?.version).toBe(original.version);
      await expect.poll(async () => (await task(page, paused).catch(() => undefined))?.publication?.published === true,
        { timeout: 600_000, interval: 3_000 }).toBe(true);
      expect(await fileInfo(page, `${folder}/${mediumFile.split('/').pop()}`)).toBeTruthy();
    } finally {
      page = seeded.page;
      if (paused) await engine(page, 'DELETE', `/api/v1/tasks/${paused}`).catch(() => undefined);
      await restoreFolder(page);
      await removeFromNas(page, [folder]);
      await removeOwnStages(staged);
    }
  });
});
