// Extensions (administrators only): the installed list, the official catalog,
// checking for updates, and installing from an address (the InputGroup with
// its trust checkbox), then enabling, disabling and removing that extension,
// each behind its confirmation.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import type { Page } from 'playwright';
import { changesApp, confirm, data, goTo, openDownloadHub, text } from './support/download-hub.ts';

/** GopeedLab's sample extension, the one the old suite installed. */
const sample = 'https://github.com/GopeedLab/gopeed-extension-samples#github-release-sample';

const extensionsPage = async (app: Parameters<typeof openDownloadHub>[0]) => {
  const page = await openDownloadHub(app);
  await goTo(page, 'Extensions', '擴充套件');
  return page;
};
const installed = (page: Page) => page.locator('[data-section=installed] [role=list] > *');
const addressForm = (page: Page) => page.locator('[data-section=install-url]');

test('admin sees the installed extensions and the catalog entries not yet installed', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await extensionsPage(app);
  const plugins: any[] = await data(page, 'gopeed.plugins.list');
  await expect.poll(() => installed(page).count(), { timeout: 30_000 }).toBe(plugins.length);
  const catalog: any[] = await data(page, 'gopeed.plugins.catalog').catch(() => []);
  const ids = new Set(plugins.map(plugin => plugin.identity));
  // A recommended entry sits in Recommended, the others in the catalog list.
  for (const entry of catalog.filter(entry => !ids.has(entry.id))) {
    const listed = page.locator(`[data-catalog-entry="${entry.id}"]`)
      .or(page.locator('[data-recommended]').filter({ has: page.locator(`[title="${entry.title}"]`) }));
    await expect.poll(() => listed.count(), { timeout: 30_000 }).toBe(1);
  }
  if (plugins.length) {
    // Checking for updates asks GitHub and changes nothing.
    await page.getByRole('button', { name: text('Check extension updates', '檢查擴充套件更新') }).click();
    await expect.poll(() => page.locator('[data-section=installed] [aria-busy=true]').count(), { timeout: 60_000 }).toBe(0);
  }
});

test('installing from an address waits for both the address and the trust checkbox', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await extensionsPage(app);
  const form = addressForm(page);
  const install = form.getByRole('button', { name: text('Install extension', '安裝擴充套件') });
  const trust = form.getByRole('checkbox', { name: text('I trust this extension source', '我信任此擴充套件來源') });
  expect(await install.isDisabled()).toBe(true);
  await form.getByRole('textbox', { name: text('Extension URL', '擴充套件網址') }).fill(sample);
  expect(await install.isDisabled()).toBe(true);
  await trust.check();
  await expect.poll(() => install.isEnabled()).toBe(true);
  await trust.uncheck();
  await expect.poll(() => install.isDisabled()).toBe(true);
});

test('admin installs an extension from its address, enables, disables and removes it', { session: 'admin', timeout: 600_000, skip: changesApp }, async ({ app }) => {
  const page = await extensionsPage(app);
  const plugins: any[] = await data(page, 'gopeed.plugins.list');
  test.skip(plugins.some(plugin => plugin.identity === 'gopeed@github-release'), 'the sample extension is already installed, by someone else');
  const before = new Set(plugins.map(plugin => plugin.identity));
  const form = addressForm(page);
  await form.getByRole('textbox', { name: text('Extension URL', '擴充套件網址') }).fill(sample);
  await form.getByRole('checkbox', { name: text('I trust this extension source', '我信任此擴充套件來源') }).check();
  await form.getByRole('button', { name: text('Install extension', '安裝擴充套件') }).click();
  await expect.poll(async () => (await data(page, 'gopeed.plugins.list') as any[]).filter(plugin => !before.has(plugin.identity)).length,
    { timeout: 180_000 }).toBe(1);
  const added = (await data(page, 'gopeed.plugins.list') as any[]).find(plugin => !before.has(plugin.identity));
  const plugin = () => data(page, 'gopeed.plugins.list').then((plugins: any[]) => plugins.find(x => x.identity === added.identity));
  const row = installed(page).filter({ hasText: added.title || added.name });
  try {
    // A new extension starts disabled.
    expect(added.disabled).toBe(true);
    await row.getByRole('button', { name: text('Enable', '啟用') }).click();
    await confirm(page);
    await expect.poll(async () => (await plugin())?.disabled, { timeout: 60_000 }).toBe(false);
    await row.getByRole('button', { name: text('Disable', '停用') }).click();
    await confirm(page);
    await expect.poll(async () => (await plugin())?.disabled, { timeout: 60_000 }).toBe(true);
  } finally {
    await row.getByRole('button', { name: text('Remove', '移除') }).click();
    await confirm(page);
    await expect.poll(async () => await plugin(), { timeout: 60_000 }).toBeUndefined();
  }
});
