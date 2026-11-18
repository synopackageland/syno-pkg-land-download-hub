import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { ctl } from './support/real-ctl.ts';
import { confirm, data, goTo, openDownloadHub, text } from './support/download-hub.ts';
import { observe } from './support/work-poll.ts';
import { withCleanup } from './support/cleanup.ts';
import { openApp } from '@synopackageland/cli/e2e';
import type { Page } from 'playwright';

const fixturePackage = 'DownloadHubFixture';
const fixtureData = (page: Page, method: string, params = {}) => data(page, method, params, fixturePackage);
const fixtureCtl = (method: string, params = {}) => ctl(method, params, [], fixturePackage);
async function openOwnedPackage(app: Parameters<typeof openDownloadHub>[0]) {
  const window = await openApp(app, `SYNO.SDS.${fixturePackage}.Instance`);
  await window.locator('.gopeed-root').waitFor({ state: 'visible' });
  return window.page();
}

test('real CLI manages a test-owned extension installed from a local NAS folder', {
  session: 'admin', skip: process.env.E2E_OWN_PACKAGE === '1' ? false : 'Install the isolated fixture package first',
}, async ({ app }) => {
  const page = await openOwnedPackage(app), id = 'download-hub-fixture@coverage', marker = `fixture-${randomUUID()}`;
  const installed = () => fixtureData(page, 'gopeed.plugins.list').then((rows: any[]) => rows.find(x => x.identity === id));
  expect(await installed()).toBeUndefined();
  await withCleanup(async () => {
    expect((await fixtureData(page, 'fixture.extension.install')).id).toBe(id);
    await fixtureCtl('gopeed.plugins.settings', { id, settings: { userAgent: marker } });
    expect((await installed()).settings.find((field: any) => field.name === 'userAgent').value).toBe(marker);
    await fixtureCtl('gopeed.plugins.switch', { id, consent: true, enabled: false });
    expect((await installed()).disabled).toBe(true);
    await fixtureCtl('gopeed.plugins.switch', { id, consent: true, enabled: true });
    expect((await installed()).disabled).toBe(false);
    await fixtureCtl('gopeed.plugins.delete', { id, consent: true });
    await observe('extension deletion', installed, value => value === undefined);
  }, async step => {
    await step(async () => { if (await installed()) await fixtureCtl('gopeed.plugins.delete', { id, consent: true }); });
  });
});

test('real CLI extension settings, enable, disable and delete read back an owned fixture', {
  session: 'admin',
  skip: process.env.E2E_EXTENSION_FIXTURE_URL && process.env.E2E_EXTENSION_FIXTURE_ID ? false : 'GitHub-only install rejects local URLs; no external repository is created; local management is covered separately',
}, async ({ app }) => {
  const page = await openDownloadHub(app), id = process.env.E2E_EXTENSION_FIXTURE_ID!;
  const setting = process.env.E2E_EXTENSION_FIXTURE_SETTING ?? 'userAgent', marker = `fixture-${randomUUID()}`;
  const installed = () => data(page, 'gopeed.plugins.list').then((rows: any[]) => rows.find(x => x.identity === id));
  expect(await installed()).toBeUndefined(); // Never mutate somebody else's extension.
  await withCleanup(async () => {
    await data(page, 'gopeed.plugins.install', { url: process.env.E2E_EXTENSION_FIXTURE_URL, consent: true });
    await observe('toBeTruthy condition', installed, value => Boolean(value));
    await ctl('gopeed.plugins.settings', { id, settings: { [setting]: marker } });
    const settings = (await installed()).settings;
    expect(settings.find((field: any) => field.name === setting)?.value).toBe(marker);
    await ctl('gopeed.plugins.switch', { id, consent: true, enabled: true });
    expect((await installed()).disabled).toBe(false);
    await ctl('gopeed.plugins.switch', { id, consent: true, enabled: false });
    expect((await installed()).disabled).toBe(true);
    await ctl('gopeed.plugins.delete', { id, consent: true });
    expect(await installed()).toBeUndefined();
  }, async step => {
    await step(async () => { if (await installed()) await data(page, 'gopeed.plugins.delete', { id, consent: true }); });
  });
});

test('real CLI definition rollback swaps verified generations and restores the original', {
  session: 'admin',
  skip: process.env.E2E_DEFINITIONS_PREVIOUS ? false : 'Supply an isolated verified previous catalog generation',
}, async ({ app }) => {
  const page = await openDownloadHub(app), before = await data(page, 'search.catalog');
  expect(before.rollbackAvailable).toBe(true);
  expect(before.generation).not.toBe(process.env.E2E_DEFINITIONS_PREVIOUS);
  let swapped = false;
  await withCleanup(async () => {
    await ctl('search.definitions.rollback'); swapped = true;
    expect((await data(page, 'search.catalog')).generation).toBe(process.env.E2E_DEFINITIONS_PREVIOUS);
  }, async step => {
    await step(async () => { if (swapped) await ctl('search.definitions.rollback'); });
    await step(async () => { expect((await data(page, 'search.catalog')).generation).toBe(before.generation); });
  });
});

// A test-owned package lets the runner remove newly installed tools at teardown.
// Without that contract, installing a missing tool cannot restore the prior
// state through the product API; do not silently leave it installed.
for (const tool of ['yt-dlp', 'ffmpeg', 'deno', 'gallery-dl']) {
  test(`real UI ${tool} install finishes its durable job and leaves other cards usable`, {
    session: 'admin',
    skip: process.env.E2E_OWN_PACKAGE === '1' ? false : 'Requires a test-owned package with teardown authorization',
  }, async ({ app }) => {
    const page = await openOwnedPackage(app);
    await goTo(page, 'Core updates', '核心更新');
    const card = page.locator(`[data-media-tool="${tool}"]`);
    const receipts = process.env.E2E_TOOL_RECEIPTS ? JSON.parse(readFileSync(process.env.E2E_TOOL_RECEIPTS, 'utf8')) : {};
    let request = receipts[tool] ?? '', version = '';
    if (request) {
      // Continue observing an exact receipt left by a prior work-limit failure.
      // Never re-admit, reinstall, reset or switch its pinned release.
      const original = (await fixtureData(page, 'media.tools.status')).state.tools[tool].job;
      expect(original.request).toBe(request);
      version = original.release.version;
    } else {
      await card.getByRole('button', { name: text('Check for updates', '檢查更新'), exact: true }).click();
      version = (await fixtureData(page, 'media.tools.check', { tool })).latest.version;
      await card.getByRole('button', { name: /^(Install|Update|安裝|更新)$/ }).click();
      await confirm(page);
    }
    const other = page.locator(`[data-media-tool]:not([data-media-tool="${tool}"])`).first();
    expect(await other.getByRole('button', { name: text('Check for updates', '檢查更新'), exact: true }).isEnabled()).toBe(true);
    await observe(`${tool} original install receipt`, async () => (await fixtureData(page, 'media.tools.status')).state.tools[tool].job,
      job => job?.status === 'succeeded', {
        maxReads: 600,
        check: job => {
          if (!job) return;
          request ||= job.request;
          expect(job.request).toBe(request);
          if (job.status === 'failed') throw new Error(`${tool} install failed: ${job.error}`);
        },
      });
    const state = await fixtureData(page, 'media.tools.status');
    expect(state.state.tools[tool].active.version).toBe(version);
    await observe(`${tool} card completion`, () => card.getByRole('button', { name: /^(Install|Update|安裝|更新)$/ }).isVisible(), visible => !visible);
  });
}

test('real UI GitHub token saves only its suffix, replaces and removes a test credential', {
  session: 'admin',
  skip: process.env.E2E_GITHUB_TEST_TOKEN ? false : 'Supply a dedicated scoped GitHub test token, never a publishing token',
}, async ({ app }) => {
  const page = await openDownloadHub(app), before = await data(page, 'gopeed.github.status');
  test.skip(before.saved, 'Do not replace another administrator\'s stored credential');
  const token = process.env.E2E_GITHUB_TEST_TOKEN!;
  await withCleanup(async () => {
    await goTo(page, 'Settings', '設定'); await page.locator('[data-ui-tab-value=github]').click();
    await page.locator('[data-github-token]').fill(token);
    await page.locator('[data-section=github] form').evaluate(form => (form as HTMLFormElement).requestSubmit());
    await observe('toBe condition', async () => (await data(page, 'gopeed.github.status')).last4, value => value === token.slice(-4));
    expect(await page.locator('[data-section=github]').innerText()).not.toContain(token);
    await page.locator('[data-section=github]').getByRole('button', { name: text('Replace', '更換'), exact: true }).click();
    await page.locator('[data-github-token]').fill(token);
    await page.locator('[data-section=github] form').evaluate(form => (form as HTMLFormElement).requestSubmit());
    await observe('toBe condition', async () => (await data(page, 'gopeed.github.status')).saved, value => value === true);
    await page.locator('[data-section=github]').getByRole('button', { name: text('Remove', '移除'), exact: true }).click();
    await observe('toBe condition', async () => (await data(page, 'gopeed.github.status')).saved, value => value === false);
  }, async step => { await step(() => data(page, 'gopeed.github.remove')); });
});
