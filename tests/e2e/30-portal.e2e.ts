// The Application Portal (/download-hub/) runs the same App as a whole page,
// and a saved download's folder button opens File Station: in the DSM window directly,
// from the portal through a DSM desktop tab that reveals the download.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import type { Page } from 'playwright';
import { appId, changesApp, downloadRows, openDownloadHub, openDownloadHubPortal, revealButton, savedDownload, tasks, text } from './support/download-hub.ts';

const fileStation = 'SYNO.SDS.App.FileStation3.Instance';
const fileStationOpen = (page: Page) => page.evaluate(id => !!(window as any).SYNO?.SDS?.AppMgr?.getByAppName(id)?.[0], fileStation);
/** The folder button of the newest saved download: this test's own. */
const savedName = (page: Page) => revealButton(downloadRows(page, 'LICENSE').first());

test('admin uses Download Hub as a whole page in the Application Portal', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDownloadHubPortal(app);
  expect(new URL(page.url()).pathname).toBe('/download-hub/');
  for (const [en, zh] of [['Downloads', '下載'], ['Extensions', '擴充套件'], ['Core updates', '核心更新'], ['Settings', '設定']]) {
    await expect.poll(() => page.locator('button[data-nav-item]').filter({ hasText: text(en, zh) }).count(), { timeout: 15_000 }).toBe(1);
  }
  // The group headers count every download the engine reports for this user.
  const count = (await tasks(page)).length;
  const shown = () => page.locator('[data-downloads-list] .gopeed-dl-count').allInnerTexts().then(all => all.reduce((n, c) => n + Number(c), 0));
  await expect.poll(shown, { timeout: 15_000 }).toBe(count);
});

test('in the DSM window, a saved download\'s folder button opens File Station', { session: 'admin', timeout: 240_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const saved = await savedDownload(page);
  try {
    await expect.poll(() => savedName(page).count(), { timeout: 15_000 }).toBe(1);
    await savedName(page).click();
    await expect.poll(() => fileStationOpen(page), { timeout: 30_000 }).toBe(true);
  } finally {
    await saved.remove(page);
  }
});

test('in the portal, a saved download\'s folder button opens a DSM tab that reveals it in File Station', { session: 'admin', timeout: 240_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHubPortal(app);
  const saved = await savedDownload(page);
  try {
    await expect.poll(() => savedName(page).count(), { timeout: 15_000 }).toBe(1);
    const [tab] = await Promise.all([page.context().waitForEvent('page'), savedName(page).click()]);
    await expect.poll(() => tab.url(), { timeout: 30_000 }).toContain('gopeedReveal=');
    const url = new URL(tab.url());
    expect(url.searchParams.get('launchApp')).toBe(appId);
    expect(url.searchParams.get('gopeedReveal')).toBe(saved.id);
    await expect.poll(() => fileStationOpen(tab).catch(() => false), { timeout: 60_000 }).toBe(true);
    await tab.close();
  } finally {
    await saved.remove(page);
  }
});
