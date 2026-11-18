// Who may use Download Hub, and what each account reaches. DSM Application
// Privileges decide whether normal1 enters at all; inside, ordinary users
// reach only the routes marked authenticated, every other route answers
// admin_required. 20-downloads checks that nobody reaches another user's downloads;
// 99-denied-portal, the portal without Application Privileges.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { readFile } from 'node:fs/promises';
import type { Page } from 'playwright';
import { openDesktop } from '@synopackageland/cli/e2e';
import { call, data, goTo, openDownloadHub, openDownloadHubPortal, text } from './support/download-hub.ts';

const routes = JSON.parse(await readFile(new URL('../../generated/routes.json', import.meta.url), 'utf8'));
const methods = {user: Object.keys(routes).filter(name => routes[name].permission === 'authenticated'), admin: Object.keys(routes).filter(name => routes[name].permission !== 'authenticated')};
const adminPages = [['Extensions', '擴充套件'], ['Core updates', '核心更新']] as const;
const navItems = (page: Page): Promise<string[]> => page.locator('button[data-nav-item]').allInnerTexts();

test('admin sees every page and the backend refuses requests outside a DSM session', { session: 'admin', timeout: 180_000 }, async ({ app }) => {
  const page = await openDownloadHub(app);
  // The administrator pages appear once the session check answers.
  for (const [en, zh] of [['Downloads', '下載'], ...adminPages, ['Settings', '設定']]) {
    await expect.poll(async () => (await navItems(page)).some((item: string) => text(en, zh).test(item.trim()))).toBe(true);
  }
  expect((await data(page, 'gopeed.session')).admin).toBe(true);
  // The engine's own file system and settings endpoints stay closed.
  expect((await call(page, 'gopeed.request', { method: 'GET', path: '/fs', query: {}, data: null })).success).toBe(false);
  expect((await call(page, 'gopeed.request', { method: 'PUT', path: '/api/v1/config', query: {}, data: {} })).success).toBe(false);
  // DSM checks the session cookie: no token, or no cookie, is no session.
  const refusals = await page.evaluate(async () => {
    const invoke = async (headers: Record<string, string>, credentials: RequestCredentials) => (await fetch('/webman/3rdparty/SynoPkgGopeed/cgi/api.cgi',
      { method: 'POST', credentials, headers: { 'Content-Type': 'application/json', ...headers }, body: JSON.stringify({ method: 'gopeed.pending', params: {} }) })).json();
    return [await invoke({}, 'same-origin'), await invoke({ 'X-SYNO-TOKEN': 'invalid' }, 'omit')];
  });
  for (const refusal of refusals) expect(refusal.error?.code).toBe('session_required');
  // A browser cannot forge Origin; this request leaves the page without it,
  // carrying the session's cookie and token so only the origin is wrong.
  const token = await page.evaluate(() => (window as any).SYNO.SDS.Session.SynoToken as string);
  const crossOrigin = await page.context().request.post(new URL('/webman/3rdparty/SynoPkgGopeed/cgi/api.cgi', page.url()).href, {
    headers: { Origin: 'https://foreign.invalid', 'Content-Type': 'application/json', 'X-SYNO-TOKEN': token },
    data: { method: 'gopeed.pending', params: {} },
  });
  expect((await crossOrigin.json()).error?.code).toBe('cross_origin');
});

test('normal1 reaches only the routes for users, and only through Application Privileges', { session: 'normal1', timeout: 300_000 }, async ({ app }) => {
  const page = await openDesktop(app);
  const granted = (await call(page, 'gopeed.session')).success;
  const user = new Set(methods.user);
  for (const method of [...methods.user, ...methods.admin, 'future.unknown']) {
    const reply = await call(page, method, {});
    if (!granted) expect([method, reply]).toEqual([method, { success: false, code: 'app_privilege_denied' }]);
    else if (method === 'future.unknown') expect(reply).toEqual({ success: false, code: 'unknown_method' });
    else if (user.has(method)) expect([method, ['admin_required', 'app_privilege_denied'].includes((reply as any).code)]).toEqual([method, false]);
    else expect([method, reply]).toEqual([method, { success: false, code: 'admin_required' }]);
  }
  if (!granted) return;
  // Inside the App: no administrator pages or controls.
  await openDownloadHub(app);
  const items = (await navItems(page)).map(item => item.trim());
  for (const [en, zh] of adminPages) expect(items.some(item => text(en, zh).test(item))).toBe(false);
  await goTo(page, 'Settings', '設定');
  await expect.poll(() => page.locator('[data-section=core], [data-section=access]').count()).toBe(0);
  await expect.poll(() => page.locator('[data-save-danger]').count()).toBe(0);
  // The portal opens the same App for a permitted user.
  await openDownloadHubPortal(app);
});
