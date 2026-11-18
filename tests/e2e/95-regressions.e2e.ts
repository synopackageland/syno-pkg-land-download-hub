// Regressions from a Download Hub device walkthrough (D1, D2, D4,
// D5, D6, D7). Each test states the behaviour the App should have; it fails
// until the App is fixed. D1 and D2 expect SDK 0.10.5: the copy for a
// stopped app and the settings page's final layout while it loads. D6's
// "Save history" list left the settings page with SDK BackgroundSaveSettings
// (0035): saves show on the downloads list.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import type { Page } from 'playwright';
import {
  changesApp, clearRecord, confirm, temporary, createDownload, data, downloadRows, revealButton, saveFolder, savedDownload, engine, goTo, openDownloadHub, openDownloadHubPortal, removeFromNas, tag, tasks, text, createFolder,
} from './support/download-hub.ts';
import { packageName, removeOwnStages, ssh, stages } from './support/nas-shell.ts';

const notice = (page: Page) => page.locator('.executor-notice .ui-callout[data-tone=warning]');
const approvalForm = (page: Page) => page.locator('section.background-save-settings #save-password');
/** The SDK's copy for a stopped or starting package (appStopped, APP_STOPPED, authorization.stopped and their starting forms). */
const appStopped = /\b(app|App|Download Hub) is (stopped|starting)|(App|下載中心) ?(已停止|正在啟動)|This page updates by itself|此頁面.*自動更新/;
const engineUnavailable = text('The download engine is not responding. Try again shortly.', '下載核心沒有回應，請稍後再試。');
/** Stuck-download fixture. */
const slowFile = 'https://proof.ovh.net/files/100Mb.dat';
/** Staging left by a removed download (D5). Only read, never deleted here. */
const removedDownloadStage = '57cb8094d23b22b0ec1ef6c4f6036e12';

test('D1: while the package is stopped the App shows no approval it does not need, and recovers on its own', { session: 'admin', timeout: 300_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHubPortal(app);
  await goTo(page, 'Settings', '設定');
  await expect.poll(() => page.locator('[data-save-row=folder]').count(), { timeout: 30_000 }).toBeGreaterThan(0);
  const seen = { notice: false, form: false, told: false, temporary: false, engineErrorWhileStarting: false };
  /** Milliseconds from the stop's return until the page says the App is stopped. */
  let toldAfter = Infinity;
  let started: Promise<string> | undefined;
  try {
    await ssh(`/usr/syno/bin/synopkg stop ${packageName}`, 180_000);
    const stopped = Date.now();
    for (const end = stopped + 30_000; Date.now() < end; await page.waitForTimeout(1_000)) {
      seen.notice ||= await notice(page).count() > 0;
      seen.form ||= await approvalForm(page).count() > 0;
      if (!seen.told && await page.getByText(appStopped).count() > 0) [seen.told, toldAfter] = [true, Date.now() - stopped];
      seen.temporary ||= await page.getByText(temporary).count() > 0;
    }
    // while the package starts and its engine is not up yet, the
    // page says the App is starting; it never says the engine does not respond.
    let startReturned = Infinity;
    started = ssh(`/usr/syno/bin/synopkg start ${packageName}`, 180_000).finally(() => { startReturned = Date.now(); });
    for (const end = Date.now() + 180_000; Date.now() < Math.min(end, startReturned + 30_000); await page.waitForTimeout(1_000)) {
      const error = await page.getByText(engineUnavailable).count() > 0;
      seen.engineErrorWhileStarting ||= error;
      if (!error && await page.getByText(appStopped).count() === 0 && await page.locator('[data-save-row=folder]').count() > 0) break;
    }
  } finally {
    await (started ?? ssh(`/usr/syno/bin/synopkg start ${packageName}`, 180_000));
  }
  // The executor is approved: a stopped package says it is stopped, and asks for no approval.
  expect.soft(seen).toEqual({ notice: false, form: false, told: true, temporary: false, engineErrorWhileStarting: false });
  // It says so within 10 s of the stop.
  expect.soft({ toldAfter, soon: toldAfter <= 10_000 }).toMatchObject({ soon: true });
  // Without a reload, the page is back within 30 s of the package starting.
  await expect.poll(async () => ({
    error: await page.getByText(engineUnavailable).count(),
    notice: await notice(page).count(),
    form: await approvalForm(page).count(),
    folder: await page.locator('[data-save-row=folder]').count() > 0,
  }), { timeout: 30_000, interval: 1_000 }).toEqual({ error: 0, notice: 0, form: 0, folder: true });
});

test('D2: the content below the settings sections does not move as they load', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDownloadHubPortal(app);
  const operations = await data(page, 'delegate.operations.list', {});
  const jobs = (operations.operations ?? operations).length;
  await goTo(page, 'Settings', '設定');
  // The engine settings are the last section: sample where they start until
  // the page has loaded and nothing has moved for 3 s.
  const below = page.locator('[data-section=core]');
  await expect.poll(() => below.count(), { timeout: 30_000 }).toBe(1);
  const tops: number[] = [];
  const loaded = async () => await page.locator('[data-placeholder-row], [aria-busy=true]').count() === 0
    && await page.locator('#portal-alias').inputValue().catch(() => '') !== '';
  for (let still = 0, end = Date.now() + 20_000; Date.now() < end && !(still >= 3_000 && await loaded()); await page.waitForTimeout(100)) {
    const top = Math.round((await below.boundingBox())?.y ?? 0);
    still = tops.length && tops.at(-1) === top ? still + 100 : 0;
    tops.push(top);
  }
  // SDK 0.10.5: 2 px, or 36 px more for the pager on a first open with more than five saves.
  const allowed = 2 + (jobs > 5 ? 36 : 0);
  const moved = Math.max(...tops) - Math.min(...tops);
  expect({ moved, allowed, still: moved <= allowed }).toMatchObject({ still: true });
});

test('D4: the ED2K fields are spaced like the BT fields', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDownloadHubPortal(app);
  await goTo(page, 'Settings', '設定');
  const core = page.locator('[data-section=core]');
  await expect.poll(() => core.count(), { timeout: 30_000 }).toBe(1);
  const field = (en: string, zh: string) => core.getByRole('spinbutton', { name: text(en, zh) });
  /** The space between each field's box and the next one's. */
  const gaps = async (fields: ReturnType<typeof field>[]) => {
    const boxes = await Promise.all(fields.map(async f => (await f.boundingBox())!));
    return boxes.slice(1).map((box, i) => Math.round(box.y - (boxes[i].y + boxes[i].height)));
  };
  const bt = await gaps([
    field('Stop seeding at share ratio', '達到分享率時停止做種'),
    field('Stop seeding after', '做種時間上限'),
    field('BT listening port', 'BT 監聽連接埠'),
  ]);
  const ed2k = await gaps([field('ED2K TCP port', 'ED2K TCP 連接埠'), field('ED2K UDP port', 'ED2K UDP 連接埠')]);
  expect({ bt, ed2k, alike: Math.abs(Math.min(...bt) - Math.min(...ed2k)) <= 4 }).toMatchObject({ alike: true });
});

test('D5: clearing a download in progress takes its row away at once and leaves nothing staged', { session: 'admin', timeout: 300_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const scope: string = (await data(page, 'delegate.destination', { key: 'save' })).selection.root;
  const name = `${tag}-d5-${Date.now()}`;
  const folder = await createFolder(page, scope, name);
  const before = new Set(await stages());
  let id = '';
  const restoreFolder = await saveFolder(page);
  try {
    id = await createDownload(page, folder, slowFile);
    await expect.poll(async () => (await stages()).filter(stage => !before.has(stage)).length, { timeout: 60_000 }).toBeGreaterThan(0);
    const staged = (await stages()).filter(stage => !before.has(stage));
    const row = downloadRows(page, slowFile.split('/').pop()!);
    await expect.poll(() => row.count(), { timeout: 30_000 }).toBe(1);
    await row.getByRole('button', { name: clearRecord }).click();
    await confirm(page);
    await expect.poll(() => row.count(), { timeout: 2_000, interval: 100 }).toBe(0);
    // The engine removes the task itself in the background.
    await expect.poll(async () => (await tasks(page)).some(t => t.id === id), { timeout: 30_000 }).toBe(false);
    await expect.poll(async () => (await stages()).filter(stage => staged.includes(stage)), { timeout: 30_000 }).toEqual([]);
  } finally {
    if (id) await engine(page, 'DELETE', `/api/v1/tasks/${id}`).catch(() => undefined);
    await restoreFolder(page);
    await removeFromNas(page, [folder]);
    await removeOwnStages(before);
  }
});

test('D5: the stage a removed download left is cleaned up', { session: 'admin', timeout: 60_000 }, async () => {
  // A stage whose download is gone is an orphan, even when it still holds the partial file.
  expect(await stages()).not.toContain(removedDownloadStage);
});

test('D6: the settings page lists no saves; it shows the save folder', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const methods: string[] = [];
  page.on('request', request => {
    if (request.url().includes('/SynoPkgGopeed/cgi/api.cgi')) methods.push(JSON.parse(request.postData() || '{}').method);
  });
  await goTo(page, 'Settings', '設定');
  const saves = page.locator('section.background-save-settings');
  await expect.poll(() => saves.locator('[data-save-row=folder]').count(), { timeout: 30_000 }).toBeGreaterThan(0);
  expect(await saves.getByText(text('Save history', '存檔紀錄')).count()).toBe(0);
  expect(await saves.locator('[data-save-job]').count()).toBe(0);
  expect(methods).not.toContain('delegate.operations.list');
});

test('D7: in the portal, a saved download\'s folder button opens File Station with that file selected', { session: 'admin', timeout: 240_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHubPortal(app);
  const saved = await savedDownload(page);
  try {
    const name = revealButton(downloadRows(page, 'LICENSE').first());
    await expect.poll(() => name.count(), { timeout: 15_000 }).toBe(1);
    const [tab] = await Promise.all([page.context().waitForEvent('page'), name.click()]);
    await expect.poll(() => tab.evaluate(() => !!(window as any).SYNO?.SDS?.AppMgr?.getByAppName('SYNO.SDS.App.FileStation3.Instance')?.[0]).catch(() => false),
      { timeout: 60_000 }).toBe(true);
    await expect.poll(() => tab.locator('.x-grid3-row-selected, [aria-selected=true]').filter({ hasText: 'LICENSE' }).count(), { timeout: 30_000 })
      .toBeGreaterThan(0);
    await tab.close();
  } finally {
    await saved.remove(page);
  }
});
