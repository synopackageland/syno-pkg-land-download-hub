// Phone: the Application Portal on a phone-sized touch screen (390×844,
// coarse pointer). The bottom tab bar reaches Downloads, Search and Settings;
// one pasted link starts a download into the saved folder and leads the
// list; a row's one action and its details sheet; a deep link that adds a
// link only after a tap, or opens the task list on a filter; and Search
// leaves out the sources that need a computer's browser. A desktop browser
// of the same session keeps the sidebar and no tab bar.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import type { Browser, Page, Route } from 'playwright';
import {
  changesApp, createFolder, data, downloadRows, engine, license, openDownloadHubPortal, removeFromNas, saveFolder, tag, tasks, text,
} from './support/download-hub.ts';

const phoneDevice = { viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, isMobile: true, hasTouch: true };

/** The portal page again in a phone-sized touch browser of the same DSM session. */
async function openOnPhone(desktop: Page, query = ''): Promise<Page> {
  const browser = desktop.context().browser() as Browser;
  const context = await browser.newContext({ ...phoneDevice, storageState: await desktop.context().storageState() });
  const page = await context.newPage();
  const url = new URL(desktop.url());
  url.search = query;
  await page.goto(url.toString());
  await page.locator('[data-phone-tabs]').waitFor({ state: 'visible', timeout: 60_000 });
  return page;
}
const tab = (page: Page, key: string) => page.locator(`[data-phone-tabs] [data-ui-tab-bar-item="${key}"]`);

test('a phone gets the bottom tab bar; the desktop page keeps its sidebar', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const desktop = await openDownloadHubPortal(app);
  expect(await desktop.locator('[data-phone-tabs]').count()).toBe(0);
  const phone = await openOnPhone(desktop);
  try {
    for (const key of ['downloads', 'search', 'settings']) await expect.poll(() => tab(phone, key).count()).toBe(1);
    expect(await tab(phone, 'downloads').getAttribute('aria-current')).toBe('page');
    await expect.poll(() => phone.locator('[data-phone-link]').isVisible()).toBe(true);
    // Every tap target in the tab bar and the Add line is at least 44px tall.
    const heights = await phone.locator('[data-ui-tab-bar-item], [data-phone-paste], [data-phone-start]').evaluateAll(all => all.map(e => e.getBoundingClientRect().height));
    expect(Math.min(...heights)).toBeGreaterThanOrEqual(44);
    // Nothing is wider than the phone.
    expect(await phone.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
    await tab(phone, 'settings').tap();
    await expect.poll(() => tab(phone, 'settings').getAttribute('aria-current')).toBe('page');
    await tab(phone, 'search').tap();
    await expect.poll(() => phone.locator('[data-search-page]').count()).toBe(1);
  } finally {
    await phone.context().close();
  }
});

test('one pasted link downloads into the saved folder and leads the list', { session: 'admin', timeout: 240_000, skip: changesApp }, async ({ app }) => {
  const desktop = await openDownloadHubPortal(app);
  const restoreFolder = await saveFolder(desktop);
  const scope: string = (await data(desktop, 'delegate.destination', { key: 'save' })).selection.root;
  const folder = await createFolder(desktop, scope, `${tag}-phone-${Date.now()}`);
  await data(desktop, 'gopeed.destination', { key: 'save', path: folder });
  await data(desktop, 'downloads.folder.set', { subfolder: '' });
  const before = new Set((await tasks(desktop)).map(t => t.id));
  const phone = await openOnPhone(desktop);
  let id = '';
  try {
    await expect.poll(() => phone.locator('[data-phone-options]').innerText(), { timeout: 30_000 }).toContain(folder.split('/').pop()!);
    await phone.locator('[data-phone-link] input, input[data-phone-link]').first().fill(`Copied text ${license.url} more words`);
    await phone.locator('[data-phone-start]').tap();
    // The field empties at once and the new download is the first row.
    await expect.poll(() => phone.locator('input[data-phone-link], [data-phone-link] input').first().inputValue()).toBe('');
    await expect.poll(async () => (await tasks(desktop)).find(t => !before.has(t.id) && !String(t.id).startsWith('prepare-'))?.id ?? '', { timeout: 60_000 }).not.toBe('');
    id = (await tasks(desktop)).find(t => !before.has(t.id) && !String(t.id).startsWith('prepare-'))!.id;
    await expect.poll(() => downloadRows(phone).first().getAttribute('data-row-key'), { timeout: 30_000 }).toBe(id);
    // Tapping the row opens its details in a bottom sheet with one main action.
    await downloadRows(phone).first().locator('.gopeed-dl-main').tap();
    const sheet = phone.locator('[data-task-sheet], [role=dialog]').filter({ has: phone.locator('[data-task-panel]') });
    await expect.poll(() => sheet.count()).toBe(1);
    await expect.poll(() => sheet.locator('[data-task-panel] footer button').count()).toBeGreaterThan(0);
  } finally {
    if (id) await engine(desktop, 'DELETE', `/api/v1/tasks/${id}`).catch(() => undefined);
    await phone.context().close();
    await restoreFolder(desktop);
    await removeFromNas(desktop, [folder]);
  }
});

test('a deep link adds its link only after the user taps Download', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const desktop = await openDownloadHubPortal(app);
  const count = (await tasks(desktop)).length;
  const phone = await openOnPhone(desktop, `?add=${encodeURIComponent(license.url)}`);
  try {
    const confirm = phone.locator('[role=dialog]').filter({ hasText: text('Download this link?', '要下載此連結嗎？') });
    await expect.poll(() => confirm.count(), { timeout: 30_000 }).toBe(1);
    await expect.poll(() => confirm.innerText()).toContain(license.url);
    // The address loses the link, so a reload does not ask again.
    expect(new URL(phone.url()).searchParams.has('add')).toBe(false);
    await confirm.getByRole('button', { name: text('Cancel', '取消') }).tap();
    await expect.poll(() => confirm.count()).toBe(0);
    expect((await tasks(desktop)).length).toBe(count);
    // A link that is not one http(s), magnet or ed2k link opens nothing.
    await phone.goto(new URL(`?add=${encodeURIComponent('javascript:alert(1)')}`, phone.url()).toString());
    await phone.locator('[data-phone-tabs]').waitFor();
    await phone.waitForTimeout(2_000);
    expect(await phone.locator('[role=dialog]').count()).toBe(0);
  } finally {
    await phone.context().close();
  }
});

test('a deep link opens the task list on a filter', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const desktop = await openDownloadHubPortal(app);
  const phone = await openOnPhone(desktop, '?view=downloads&filter=failed');
  try {
    await expect.poll(() => phone.locator('[data-phone-filter] [aria-selected=true], [data-phone-filter] [data-state=active]').innerText(), { timeout: 30_000 }).toMatch(/^(Failed|失敗)/);
    expect(new URL(phone.url()).searchParams.has('view')).toBe(false);
  } finally {
    await phone.context().close();
  }
});

const api = '**/SynoPkgGopeed/cgi/api.cgi*';
const source = (id: string, name: string, extra: Record<string, unknown> = {}) => ({ id, kind: 'builtin', name, enabled: true, configured: false, supported: true, reasons: [], settings: [], ...extra });

test('Search on a phone leaves out the sources that need a computer\'s browser', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const desktop = await openDownloadHubPortal(app);
  const phone = await openOnPhone(desktop);
  const asked: string[][] = [];
  // This browser's own fixtures: no tracker or external site is contacted.
  await phone.route(api, async (route: Route) => {
    const { method: name, params } = JSON.parse(route.request().postData() || '{}');
    const reply = (data: unknown) => route.fulfill({ json: { success: true, data } });
    if (name === 'search.sources.list') return reply([source('open', 'Open tracker'), source('cf', 'Browser tracker', { cloudflare: true })]);
    if (name === 'search.solver.get') return reply({ configured: false });
    if (name === 'search.jobs.list') return reply({ jobs: [] });
    if (name === 'search.jobs.start') { asked.push(params.sourceIds); return reply({ outcome: 'accepted', job: { id: 'phone-job', operation: 'search', state: 'queued' } }); }
    if (name === 'search.jobs.status') return reply({ id: 'phone-job', operation: 'search', state: 'succeeded' });
    if (name === 'search.jobs.pages') return reply({ items: [{ sourceId: 'open', page: 1, results: [{ resultId: 'open-1', sourceId: 'open', title: `${params?.query ?? 'ubuntu'} result`, size: 1024, seeders: 3 }] }], offset: 0, total: 1 });
    if (name === 'search.jobs.result') return reply({ kind: 'search', pages: 1 });
    return route.fallback();
  });
  try {
    await tab(phone, 'search').tap();
    // One field: category, sources, sort and the Search button wait in the options sheet.
    expect(await phone.locator('[data-search-submit], [data-search-category], [data-search-sources], [data-search-sort]').count()).toBe(0);
    await phone.locator('[data-search-query] input').fill('ubuntu');
    await phone.locator('[data-search-query] input').press('Enter');
    await expect.poll(() => asked).toEqual([['open']]);
    // No browser guide or browser state; one short line names what is left out.
    expect(await phone.locator('[data-browser-guide], [data-assist-notice], [data-assist-off]').count()).toBe(0);
    // One status line counts the sources and results, the left-out source muted beside it.
    await expect.poll(() => phone.locator('[data-search-status]').innerText()).toMatch(/1 source · 1 result|1 個來源 · 1 筆結果/);
    await expect.poll(() => phone.locator('[data-search-desktop-only]').innerText()).toMatch(/1 source works only on a computer|1 個來源僅能在電腦上使用/);
    await expect.poll(() => phone.locator('[data-search-row]').count()).toBe(1);
    expect(await phone.locator('[data-search-row] [data-search-download]').count()).toBe(1);
    // No query chips, and no results filter for a short list.
    expect(await phone.locator('[data-search-tabs], [data-search-filter]').count()).toBe(0);
    // Per-source states live in the options sheet; the browser-only source is not among them.
    await phone.locator('[data-search-tune]').tap();
    const sheet = phone.locator('[data-search-options]');
    await expect.poll(() => sheet.locator('[data-search-chip=open]').count()).toBe(1);
    expect(await sheet.locator('[data-search-chip=cf]').count()).toBe(0);
    // Default options leave the options button unmarked.
    expect(await phone.locator('[data-search-tuned]').count()).toBe(0);
  } finally {
    await phone.context().close();
  }
});
