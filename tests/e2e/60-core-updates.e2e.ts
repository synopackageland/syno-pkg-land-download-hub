// Core updates (administrators only): the engine card and the media tools,
// checking for a release, the confirmation before installing one, a refused
// update that keeps the running engine, and reinstalling the official core
// then restoring the previous one.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import type { Page } from 'playwright';
import { call, changesApp, confirm, data, goTo, openDownloadHub, text } from './support/download-hub.ts';

const corePage = async (app: Parameters<typeof openDownloadHub>[0]) => {
  const page = await openDownloadHub(app);
  await goTo(page, 'Core updates', '核心更新');
  return page;
};
const card = (page: Page) => page.locator('[data-core]');
/**
 * The latest core release. GitHub limits how often the NAS may ask; a check
 * it refuses skips the test rather than failing it.
 */
async function latestRelease(page: Page): Promise<any> {
  const reply = await call(page, 'binaries.check');
  test.skip(!reply.success && reply.code === 'tool_source_rate_limited', 'GitHub is rate-limiting this NAS\'s release checks');
  if (!reply.success) throw new Error(`binaries.check refused: ${reply.code}`);
  return reply.data;
}
const install = (page: Page) => card(page).getByRole('button', { name: /^(Update core|更新核心|Reinstall official version|重新安裝官方版本)$/ });

test('the core page shows the running engine, the media tools, and the release a check finds', { session: 'admin', timeout: 180_000 }, async ({ app }) => {
  const page = await corePage(app);
  const status = await data(page, 'binaries.status');
  await expect.poll(() => card(page).innerText(), { timeout: 30_000 }).toContain(status.active.version);
  await expect.poll(() => card(page).innerText()).toMatch(status.ready ? /Running|正常運作/ : /./);
  const tools: any = await data(page, 'media.tools.status');
  expect(await page.locator('[data-media-tool]').count()).toBeGreaterThan(0);
  expect(tools).toBeTruthy();
  // Checking reads the latest release and changes nothing.
  await card(page).getByRole('button', { name: text('Check for updates', '檢查更新') }).click();
  const release = await latestRelease(page);
  await expect.poll(() => card(page).innerText(), { timeout: 60_000 }).toContain(release.version);
  // Installing asks first; Cancel leaves the engine as it was.
  await install(page).click();
  const box = page.getByRole('dialog', { name: text('Install official core?', '安裝官方核心？') });
  await expect.poll(() => box.count()).toBe(1);
  await box.getByRole('button', { name: text('Cancel', '取消') }).click();
  await expect.poll(() => box.count()).toBe(0);
  expect((await data(page, 'binaries.status')).active).toEqual(status.active);
});

test('an update whose checksum does not match the release is refused and the engine keeps running', { session: 'admin', timeout: 180_000, skip: changesApp }, async ({ app }) => {
  const page = await corePage(app);
  const before = await data(page, 'binaries.status');
  const release = await latestRelease(page);
  expect(await call(page, 'binaries.update', { version: release.version, sha256: '0'.repeat(64), consent: true }))
    .toEqual({ success: false, code: 'release_changed' });
  const after = await data(page, 'binaries.status');
  expect(after.ready).toBe(true);
  expect(after.active).toEqual(before.active);
});

test('admin installs the official core and restores the previous one', { session: 'admin', timeout: 600_000, skip: changesApp }, async ({ app }) => {
  const page = await corePage(app);
  const before = await data(page, 'binaries.status');
  await card(page).getByRole('button', { name: text('Check for updates', '檢查更新') }).click();
  const release = await latestRelease(page);
  test.skip(!release.compatible, `the latest core ${release.version} needs an App update first`);
  await install(page).click();
  await confirm(page);
  await expect.poll(async () => {
    const status = await data(page, 'binaries.status');
    return status.ready && status.active.path.includes('/managed/') && status.active.version === release.version;
  }, { timeout: 300_000, interval: 2_000 }).toBe(true);
  const restore = card(page).getByRole('button', { name: text('Restore previous version', '回復上一版') });
  if (before.active.version === release.version) {
    // Reinstalling the running version offers no restore of the same version;
    // the previous core is still kept, and the backend puts it back.
    await expect.poll(() => restore.count()).toBe(0);
    await data(page, 'binaries.rollback', { consent: true });
  } else {
    // The previous core is one click (and a confirmation) away.
    await restore.click();
    await confirm(page);
  }
  await expect.poll(async () => {
    const status = await data(page, 'binaries.status');
    return status.ready && status.active.path === before.active.path;
  }, { timeout: 180_000, interval: 2_000 }).toBe(true);
});
