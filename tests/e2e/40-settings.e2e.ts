// Settings: background saves (the folder and its access, the approval form; no
// saves list or app-wide disable: SDK BackgroundSaveSettings, 0035), the
// Application Portal alias, and the engine's
// settings with their bounds. The approval form and ExecutorNotice show only
// while the executor waits for an administrator; test-nas's is approved, so
// those tests show this browser an executor that awaits approval and
// submit nothing.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import type { Page } from 'playwright';
import { openDesktop } from '@synopackageland/cli/e2e';
import { call, changesApp, data, goTo, openDownloadHub, text } from './support/download-hub.ts';

const settingsPage = async (app: Parameters<typeof openDownloadHub>[0]) => {
  const page = await openDownloadHub(app);
  await goTo(page, 'Settings', '設定');
  return page;
};
const coreField = (page: Page, en: string, zh: string) => page.locator('[data-section=core]').getByRole('spinbutton', { name: text(en, zh) });
/** Waits until the engine settings form shows what the engine runs with and takes input. */
async function coreLoaded(page: Page): Promise<any> {
  const core = await data(page, 'gopeed.settings.get');
  const field = coreField(page, 'Simultaneous downloads', '同時下載數');
  await expect.poll(async () => await field.isEnabled() && await field.inputValue() === String(core.maxRunning), { timeout: 30_000 }).toBe(true);
  return core;
}
const settingsCalls = (page: Page) => {
  const methods: string[] = [];
  page.on('request', request => {
    if (request.url().includes('/SynoPkgGopeed/cgi/api.cgi')) methods.push(JSON.parse(request.postData() || '{}').method);
  });
  return methods;
};

/**
 * Makes this page see an executor that awaits an administrator. The window
 * asks the executor itself first (the App's executor.status): here it answers
 * as an executor that is not the approved, running one. An administrator's
 * window then reads the executor's root tasks from Task Scheduler, where an
 * empty list reads as "needs-admin-approval"; anyone else is told an
 * administrator is needed (ExecutorNotice). The backend's delegate.status
 * answers needs-admin-approval (the settings form). Only this browser sees it.
 */
async function executorAwaitsApproval(page: Page): Promise<void> {
  await page.route('**/webapi/entry.cgi**', async route => {
    const request = new URLSearchParams(new URL(route.request().url()).search + '&' + (route.request().postData() ?? ''));
    if (request.get('api') !== 'SYNO.Core.TaskScheduler' || request.get('method') !== 'list') return route.fallback();
    await route.fulfill({ json: { success: true, data: { tasks: [], total: 0 } } });
  });
  const awaiting: Record<string, (data: any) => any> = {
    'executor.status': data => ({ ...data, ready: false, state: 'needs-admin-approval', tasks: undefined }),
    'delegate.status': data => ({ ...data, state: 'needs-admin-approval' }),
  };
  await page.route('**/SynoPkgGopeed/cgi/api.cgi*', async route => {
    const change = awaiting[JSON.parse(route.request().postData() || '{}').method];
    if (!change) return route.fallback();
    // A test that ends while this request is open disposes the request.
    try {
      const response = await route.fetch();
      const body = await response.json();
      await route.fulfill({ response, json: { ...body, data: change(body.data) } });
    } catch { /* the page is gone */ }
  });
}

test('admin\'s settings show background saves, the portal and the engine settings', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const methods = settingsCalls(await openDesktop(app));
  const page = await settingsPage(app);
  const saves = page.locator('section.background-save-settings');
  await expect.poll(() => saves.locator('[data-save-row=folder]').count(), { timeout: 30_000 }).toBeGreaterThan(0);
  const destination = await data(page, 'delegate.destination', { key: 'save' });
  if (destination.destination?.path) await expect.poll(() => saves.locator('[data-save-row=folder]').innerText()).toContain(destination.destination.path);
  // Saves show on the downloads list; the settings page lists none (95-regressions D6).
  expect(methods).not.toContain('delegate.operations.list');
  // The engine settings show what the engine runs with.
  const core = await data(page, 'gopeed.settings.get');
  await expect.poll(() => coreField(page, 'Simultaneous downloads', '同時下載數').inputValue()).toBe(String(core.maxRunning));
  await expect.poll(() => coreField(page, 'Connections per HTTP download', '每個 HTTP 下載的連線數').inputValue()).toBe(String(core.connections));
  // The Access tab holds the portal, which reads DSM's portal settings and can take a while.
  await page.locator('[data-settings] [data-ui-tab-value="access"]').click();
  await expect.poll(() => page.locator('[data-section=access]').count()).toBe(1);
  await expect.poll(() => page.locator('#portal-alias').inputValue(), { timeout: 30_000 }).toBe('download-hub');
});

test('the engine settings form refuses out-of-range values before sending them', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await settingsPage(app);
  await coreLoaded(page);
  const methods = settingsCalls(page);
  const field = coreField(page, 'Simultaneous downloads', '同時下載數');
  await field.fill('17');
  await page.getByRole('button', { name: text('Apply', '套用') }).click();
  expect(await field.evaluate((input: HTMLInputElement) => input.validity.rangeOverflow)).toBe(true);
  await page.waitForTimeout(1_000);
  expect(methods).not.toContain('gopeed.settings.set');
  expect((await data(page, 'gopeed.settings.get')).maxRunning).not.toBe(17);
});

test('background saves have no app-wide disable action, and stay on', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await settingsPage(app);
  const methods = settingsCalls(page);
  const saves = page.locator('section.background-save-settings');
  await expect.poll(() => saves.locator('[data-save-row=folder]').count(), { timeout: 30_000 }).toBeGreaterThan(0);
  // An administrator revokes one destination at a time; nothing turns every user's saves off.
  expect(await page.getByRole('button', { name: text('Disable background saves', '停用背景存檔') }).count()).toBe(0);
  expect(await page.locator('[data-save-danger]').count()).toBe(0);
  expect(methods).not.toContain('delegate.disable');
  expect((await data(page, 'delegate.status')).disabled).not.toBe(true);
});

test('while the executor awaits approval, admin sees the notice, its approval dialog and the approval form', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDesktop(app);
  await executorAwaitsApproval(page);
  await openDownloadHub(app);
  const notice = page.locator('.executor-notice .ui-callout[data-tone=warning] .executor-notice-body');
  await expect.poll(() => notice.count(), { timeout: 30_000 }).toBe(1);
  await notice.getByRole('button', { name: text('Approve', '核准') }).click();
  const approval = page.getByRole('dialog').filter({ has: page.locator('#delegate-password') });
  await expect.poll(() => approval.count()).toBe(1);
  await page.keyboard.press('Escape');
  await expect.poll(() => approval.count()).toBe(0);
  // The settings page asks for the scope and the administrator's password.
  await goTo(page, 'Settings', '設定');
  const form = page.locator('section.background-save-settings form');
  await expect.poll(() => form.locator('#save-password').count(), { timeout: 15_000 }).toBe(1);
  await expect.poll(() => form.getByRole('button', { name: text('Approve', '核准') }).isDisabled()).toBe(true);
});

test('while the executor awaits approval, normal1 is told to ask an administrator', { session: 'normal1', timeout: 120_000 }, async ({ app }) => {
  const page = await openDesktop(app);
  test.skip(!(await call(page, 'gopeed.session')).success, 'normal1 has no Application Privileges on Download Hub');
  await executorAwaitsApproval(page);
  await openDownloadHub(app);
  const notice = page.locator('.executor-notice .ui-callout[data-tone=warning] .executor-notice-body');
  await expect.poll(() => notice.innerText(), { timeout: 30_000 }).toMatch(text(
    'Some features are unavailable until an administrator approves system access for this app.',
    '管理員核准此 App 的系統存取權限前，部分功能無法使用。'));
  expect(await notice.getByRole('button').count()).toBe(0);
});

test('admin changes the engine settings, the engine applies them, and they are restored', { session: 'admin', timeout: 180_000, skip: changesApp }, async ({ app }) => {
  const page = await settingsPage(app);
  const before = await coreLoaded(page);
  try {
    await coreField(page, 'Simultaneous downloads', '同時下載數').fill(String(before.maxRunning === 3 ? 4 : 3));
    await coreField(page, 'Connections per HTTP download', '每個 HTTP 下載的連線數').fill(String(before.connections === 8 ? 6 : 8));
    await page.getByRole('button', { name: text('Apply', '套用') }).click();
    await expect.poll(async () => (await data(page, 'gopeed.settings.get')).maxRunning, { timeout: 30_000 }).toBe(before.maxRunning === 3 ? 4 : 3);
    expect((await data(page, 'gopeed.settings.get')).connections).toBe(before.connections === 8 ? 6 : 8);
    // The backend checks the bounds too.
    for (const bad of [{ maxRunning: 0 }, { maxRunning: 17 }, { connections: 33 }, { btPort: 80 }]) {
      expect((await call(page, 'gopeed.settings.set', { ...before, ...bad })).success).toBe(false);
    }
  } finally {
    const { maxRunning, connections, seedRatio, seedMinutes, btPort, ed2kPort, ed2kUdpPort } = before;
    await data(page, 'gopeed.settings.set', { maxRunning, connections, seedRatio, seedMinutes, btPort, ed2kPort, ed2kUdpPort });
    expect(await data(page, 'gopeed.settings.get')).toMatchObject({ maxRunning, connections, btPort });
  }
});
