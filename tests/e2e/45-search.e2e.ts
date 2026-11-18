// Search: the Search page asks every chosen source at once; each source's
// chip shows its state and filters the results; one release found by two
// sources is one row; results sort; a result downloads into the saved folder
// and links to its task; and a source added from an empty Search page
// returns to the query. Search answers are this
// browser's own fixtures (no tracker or external site is contacted); adding
// a source writes the user's own source list and deletes it again.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import type { Page, Route } from 'playwright';
import { changesApp, createFolder, data, deleteFixtureTasks, goTo, openDownloadHub, refreshFixture, removeFromNas, saveFolder, tag, tasks, text } from './support/download-hub.ts';

import { randomUUID } from 'node:crypto';
import { fixtureURL } from './support/real-ctl.ts';
import { observe } from './support/work-poll.ts';
import { withCleanup } from './support/cleanup.ts';

const api = '**/SynoPkgGopeed/cgi/api.cgi*';
const method = (route: Route) => JSON.parse(route.request().postData() || '{}');

/** Answers the search methods in this browser only, from fixtures. */
async function searchFixture(page: Page, submit: () => Promise<any>, sources: unknown[]): Promise<string[][]> {
  const asked: string[][] = [];
  let pages: unknown[] = [];
  await page.route(api, async route => {
    const { method: name, params } = method(route);
    const reply = (data: unknown) => route.fulfill({ json: { success: true, data } });
    if (name === 'search.sources.list') return reply(sources);
    if (name === 'search.jobs.list') return reply({jobs:[]});
    if (name === 'search.jobs.start') {
      asked.push(params.sourceIds);
      // open and mirror both find one release (the same info hash); open also finds a second.
      const hash = 'a'.repeat(40);
      pages=params.sourceIds.map((sourceId: string) => sourceId === 'locked'
        ? {sourceId,page:1,results:[],error:{code:'search_login'}}
        : {sourceId,page:1,results:[
          {resultId:`${sourceId}-1`,sourceId,title:`${params.query} from open`,size:1024,seeders:sourceId === 'mirror' ? 1 : 3,hash},
          ...(sourceId === 'open' ? [{resultId:'open-2',sourceId,title:`${params.query} popular`,size:2048,seeders:9}] : []),
        ]});
      return reply({outcome:'accepted',job:{id:'fixture-job',operation:'search',state:'queued'}});
    }
    if (name === 'search.jobs.status') return reply({id:'fixture-job',operation:'search',state:'succeeded'});
    if (name === 'search.jobs.pages') return reply({items:pages.slice(params.offset,params.offset+params.limit),offset:params.offset,total:pages.length});
    if (name === 'search.jobs.result') return reply({kind:'search',pages:pages.length});
    if (name === 'search.download') return reply(await submit());
    return route.fallback();
  });
  return asked;
}
const source = (id: string, name: string) => ({ id, kind: 'torznab', name, enabled: true, configured: false, supported: true, reasons: [], settings: [] });

const rows = (page: Page) => page.locator('[data-search-results] [data-search-row]');
const titles = (page: Page) => rows(page).locator('[data-search-title]').allInnerTexts();

test('a search asks every source at once and downloads a result without leaving Search', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const restore = await saveFolder(page);
  const folder = await createFolder(page, (await data(page, 'delegate.destination', {key:'save'})).selection.root, `${tag}-search-view-${Date.now()}`);
  const key = `view-${randomUUID()}`;
  let prepared = '';
  await withCleanup(async () => {
  await data(page, 'gopeed.destination', {key:'save',path:folder});
  await data(page, 'downloads.folder.set', {subfolder:''});
  await refreshFixture(page, app);
  const asked = await searchFixture(page, async () => {
    const reply = await data(page, 'downloads.create_many', {path:folder,items:[{key,url:fixtureURL('/tiny.txt')}]});
    prepared = reply.results[0].ids[0];
    expect(prepared.startsWith('prepare-')).toBe(true);
    return reply;
  }, [source('open', 'Open tracker'), source('mirror', 'Mirror tracker'), source('locked', 'Locked tracker')]);
  await goTo(page, 'Search', '搜尋');
  await page.locator('[data-search-query] input').fill('ubuntu');
  await page.locator('button[data-search-submit]').click();
  await expect.poll(() => asked).toEqual([['open', 'mirror', 'locked']]);

  // A source that needs a login says so on its chip, with Sign in beside it.
  const locked = page.locator('[data-search-chip=locked]');
  await expect.poll(() => locked.innerText()).toMatch(/Login required|需要登入/);
  expect(await locked.locator('[data-chip-action=signin]').count()).toBe(1);
  await locked.locator('.search-chip-main').click();
  await expect.poll(() => page.locator('[data-search-reason]').count()).toBe(1);

  // The release both trackers found is one row naming two sources, with the better seeder count.
  await expect.poll(() => rows(page).count()).toBe(2);
  const merged = rows(page).filter({ hasText: 'ubuntu from open' });
  await expect.poll(() => merged.locator('[data-search-source]').innerText()).toMatch(/^(2 sources|2 個來源)$/);
  expect(await merged.locator('[data-search-peers] b').getAttribute('data-tone')).toBe('warning');

  // Most seeders puts the popular result first.
  await page.locator('[data-search-sort] [role=combobox]').click();
  await page.getByRole('option', { name: text('Most seeders', '做種最多') }).click();
  await expect.poll(() => titles(page)).toEqual(['ubuntu popular', 'ubuntu from open']);

  // A finished source's chip filters the results to it, and back.
  const mirror = page.locator('[data-search-chip=mirror] .search-chip-main');
  await mirror.click();
  await expect.poll(() => mirror.getAttribute('aria-pressed')).toBe('true');
  await expect.poll(() => titles(page)).toEqual(['ubuntu from open']);
  await mirror.click();
  await expect.poll(() => rows(page).count()).toBe(2);

  // Download stays on Search, then turns into Added · View, which opens the task.
  await merged.locator('button[data-search-download]').click();
  const view = merged.locator('button[data-search-view]');
  await expect.poll(() => view.innerText()).toMatch(/^(Added · View|已加入 · 檢視)$/);
  expect(await page.locator('[data-search-results]').count()).toBe(1);
  await view.click();
  await observe('View follows its admitted preparation', async () => {
    const row=(await tasks(page)).find(t=>t.publication?.batch===key&&!t.id.startsWith('prepare-'));
    return row && row.id !== prepared && await page.locator(`[data-downloads-list] [data-row-key="${row.id}"]`).getAttribute('aria-current');
  }, value=>value==='true');
  await goTo(page, 'Search', '搜尋');
  expect(await page.locator('[data-search-query] input').inputValue()).toBe('ubuntu');
  }, async step => {
    await step(()=>deleteFixtureTasks(page,row=>row.publication?.batch===key));
    await step(()=>restore(page));await step(()=>removeFromNas(page,[folder]));
  });
});

test('a source added from an empty Search page returns to the query', { session: 'admin', timeout: 180_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const before: any[] = await data(page, 'search.sources.list');
  test.skip(before.length > 0, 'this user already has search sources');
  await goTo(page, 'Search', '搜尋');
  await page.getByRole('button', { name: text('Choose sources', '選擇來源') }).click();
  const section = page.locator('[data-section="search-sources"]');
  await section.locator('button[data-add-feed]').click();
  const dialog = page.getByRole('dialog');
  await dialog.locator('[data-source-kind=ytsearch]').click();
  await dialog.getByLabel(text('Name', '名稱')).fill(`e2e YouTube ${Date.now()}`);
  await dialog.getByRole('button', { name: text('Save', '儲存') }).click();
  try {
    await expect.poll(() => page.locator('[data-search-query]').count(), { timeout: 30_000 }).toBe(1);
  } finally {
    for (const x of await data(page, 'search.sources.list')) if (!before.some(y => y.id === x.id)) await data(page, 'search.sources.delete', { id: x.id });
  }
});
