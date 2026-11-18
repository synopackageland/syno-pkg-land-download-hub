import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { randomUUID } from 'node:crypto';
import { confirm, createFolder, data, downloadRows, deleteFixtureTasks, goTo, openDownloadHub, refreshFixture, removeFromNas, saveFolder, sha256, tag, task, tasks, text } from './support/download-hub.ts';
import { fixtureURL } from './support/real-ctl.ts';
import { tinySHA256 } from './support/fixture-server.ts';
import { dismissNotifications } from '@synopackageland/cli/e2e';
import { observe, settleSearchJob } from './support/work-poll.ts';
import { withCleanup } from './support/cleanup.ts';

test('real UI download transfers bytes, pauses, resumes without reset, then removes only its record', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), restore = await saveFolder(page);
  const folder = await createFolder(page, (await data(page, 'delegate.destination', { key: 'save' })).selection.root, `${tag}-ui-${Date.now()}`);
  let id = '';
  await withCleanup(async () => {
    await data(page, 'gopeed.destination', { key: 'save', path: folder });
    await data(page, 'downloads.folder.set', { subfolder: '' });
    await refreshFixture(page, app);
    await page.locator('[data-add-download]').click();
    await page.locator('form.add-download textarea').fill(fixtureURL('/slow.bin'));
    await page.locator('form.add-download').evaluate(form => (form as HTMLFormElement).requestSubmit());
    await observe('toBe condition', async () => { id = (await tasks(page)).find(t => t.publication?.folder === folder && !t.id.startsWith('prepare-'))?.id ?? ''; return id; }, value => value !== '');
    const row = () => page.locator(`[data-downloads-list] [data-row-key="${id}"]`);
    await observe('toBeGreaterThan condition', async () => (await task(page, id)).progress?.downloaded, value => value > 0);
    const first = (await task(page, id)).progress.downloaded;
    await observe('toBeGreaterThan condition', async () => (await task(page, id)).progress.downloaded, value => value > first);
    await row().getByRole('button', { name: text('Pause', '暫停'), exact: true }).click();
    await observe('toBe condition', async () => (await task(page, id)).status, value => value === 'pause');
    const paused = (await task(page, id)).progress.downloaded;
    await page.waitForTimeout(2_000); expect((await task(page, id)).progress.downloaded).toBe(paused);
    await row().getByRole('button', { name: text('Resume', '繼續'), exact: true }).click();
    await observe('toBeGreaterThan condition', async () => { const n = (await task(page, id)).progress.downloaded; expect(n).toBeGreaterThanOrEqual(paused); return n; }, value => value > paused);
    await row().getByRole('button', { name: text('Clear download record', '清除下載紀錄') }).click();
    await confirm(page);
    await observe('download removal receipt', () => data(page, 'tasks.removal', { id }), receipt => receipt.state === 'removed');
    await observe('toBe condition', async () => (await tasks(page)).some(t => t.id === id), value => value === false);
    expect(await row().count()).toBe(0);
  }, async step => {
    await step(() => deleteFixtureTasks(page, row => row.publication?.folder === folder));
    await step(() => restore(page)); await step(() => removeFromNas(page, [folder]));
  });
});

test('real UI RSS search creates an actual download and saves the fixture bytes', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), restore = await saveFolder(page);
  const folder = await createFolder(page, (await data(page, 'delegate.destination', { key: 'save' })).selection.root, `${tag}-ui-search-${Date.now()}`);
  let sourceId = '', jobId = '';
  let admission: Promise<any> | undefined;
  await withCleanup(async () => {
    await data(page, 'gopeed.destination', { key: 'save', path: folder }); await data(page, 'downloads.folder.set', { subfolder: '' });
    await refreshFixture(page, app);
    sourceId = (await data(page, 'search.sources.save', { kind: 'rss', name: `${tag} UI RSS`, url: fixtureURL('/feed'), enabled: true })).id;
    await goTo(page, 'Search', '搜尋');
    // Select just our source; existing sources are never queried or modified.
    await page.locator('[data-search-sources] [role=combobox]').click();
    const options = page.getByRole('option');
    for (const option of await options.all()) {
      const ours = (await option.innerText()).includes(`${tag} UI RSS`);
      const selected = await option.getAttribute('aria-selected') === 'true';
      if (ours !== selected) await option.click();
    }
    await page.keyboard.press('Escape');
    await page.locator('[data-search-query] input').fill('Fixture');
    admission = page.waitForResponse(response => {
      try { const input = JSON.parse(response.request().postData() ?? '{}'); return input.method === 'search.jobs.start' && input.params?.sourceIds?.includes(sourceId); }
      catch { return false; }
    }, { timeout: 302_000 }).then(response => response.json());
    void admission.catch(() => undefined);
    await page.locator('[data-search-submit]').click();
    jobId = (await admission).data.job.id;
    const result = page.locator('[data-search-row]').filter({ hasText: 'Fixture release' });
    await observe('toBe condition', () => result.count(), value => value === 1);
    await result.locator('[data-search-download]').click();
    await observe('toBe condition', async () => (await tasks(page)).filter(t => t.publication?.folder === folder && t.publication?.published).length, value => value === 1);
    expect(await sha256(page, `${folder}/tiny.txt`)).toBe(tinySHA256);
    await result.locator('[data-search-view]').click();
    await observe('toBe condition', () => downloadRows(page, 'tiny.txt').getAttribute('aria-current'), value => value === 'true');
  }, async step => {
    await step(async () => { if (!jobId && admission) jobId = (await admission).data?.job?.id ?? ''; await settleSearchJob((method, input) => data(page, method, input), jobId); });
    await step(() => deleteFixtureTasks(page, row => row.publication?.folder === folder));
    await step(async () => { if (sourceId) await data(page, 'search.sources.delete', { id: sourceId }); });
    await step(() => restore(page)); await step(() => removeFromNas(page, [folder]));
  });
});

test('real UI media rules and follows persist relative folders and delete their own records', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app);
  await goTo(page, 'Settings', '設定'); await page.locator('[data-ui-tab-value=media]').click();
  await page.locator('[data-media-settings]').waitFor({ state: 'visible' });
  const marker = `${randomUUID()}.example`, followURL = fixtureURL(`/channel/${randomUUID()}`);
  await withCleanup(async () => {
    await page.locator('[data-media-add-rule]').click();
    let dialog = page.getByRole('dialog').last();
    await dialog.locator('input').first().fill(marker);
    await dialog.locator('[data-rule-subfolder]').fill('Videos/Fixture');
    await dialog.getByRole('button', { name: text('Apply', '套用'), exact: true }).click();
    await observe('toBe condition', async () => (await data(page, 'media.settings.get')).rules.find((r: any) => r.host === marker)?.path, value => value === 'Videos/Fixture');
    await page.locator('[data-media-add-follow]').click(); dialog = page.getByRole('dialog').last();
    await dialog.locator('input[type=url]').fill(followURL);
    await dialog.locator('[data-rule-subfolder]').fill('Videos/Fixture');
    await dialog.getByRole('checkbox').check();
    await dialog.getByRole('button', { name: text('Apply', '套用'), exact: true }).click();
    await observe('toBe condition', async () => (await data(page, 'media.settings.get')).follows.find((f: any) => f.url === followURL)?.intervalHours, value => value === 6);
    await page.locator('[data-media-rule]').filter({ hasText: marker }).getByRole('button').click();
    await page.locator('[data-media-follow]').filter({ hasText: followURL }).getByRole('button').click();
    await observe('rule and follow deletion', () => data(page, 'media.settings.get'), after =>
      !after.rules.some((r: any) => r.host === marker) && !after.follows.some((f: any) => f.url === followURL));
  }, async step => {
    const state = await data(page, 'media.settings.get');
    for (const rule of state.rules.filter((r: any) => r.host === marker)) await step(() => data(page, 'media.rules.delete', { id: rule.id }));
    for (const follow of state.follows.filter((f: any) => f.url === followURL)) await step(() => data(page, 'media.follows.delete', { id: follow.id }));
  });
});

test('real destination relative folder saves under the grant without replacing it', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), restore = await saveFolder(page);
  const folder = await createFolder(page, (await data(page, 'delegate.destination', { key: 'save' })).selection.root, `${tag}-relative-${Date.now()}`);
  const key = `relative-${randomUUID()}`;
  await withCleanup(async () => {
    await data(page, 'gopeed.destination', { key: 'save', path: folder });
    const original = await data(page, 'downloads.location');
    await data(page, 'downloads.folder.set', { subfolder: 'Pictures/Fixture' });
    const current = await data(page, 'downloads.location');
    expect(current.destination.id).toBe(original.destination.id);
    expect(current.subfolder).toBe('Pictures/Fixture');
    const admitted = await data(page, 'downloads.create_many', { path: folder, items: [{ key, url: fixtureURL('/tiny.txt') }] });
    expect(admitted.results[0].status).toBe('created');
    await observe('toBe condition', async () => (await tasks(page)).find(t => t.publication?.batch === key)?.publication?.published, value => value === true);
    expect(await sha256(page, `${folder}/Pictures/Fixture/tiny.txt`)).toBe(tinySHA256);
  }, async step => {
    await step(() => deleteFixtureTasks(page, row => row.publication?.batch === key));
    await step(() => restore(page)); await step(() => removeFromNas(page, [folder]));
  });
});

test('real UI creates, probes, edits, disables, enables and deletes its RSS source', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), name = `${tag} source ${randomUUID()}`;
  let id = '';
  await withCleanup(async () => {
    await goTo(page, 'Settings', '設定'); await page.locator('[data-ui-tab-value=search]').click();
    await dismissNotifications(page);
    await page.locator('[data-add-feed]').click();
    const dialog = page.getByRole('dialog').last();
    await dialog.getByLabel(text('Name', '名稱')).fill(name);
    await dialog.locator('input[type=url]').fill(fixtureURL('/feed'));
    await dialog.getByRole('button', { name: text('Save', '儲存'), exact: true }).click();
    await observe('toBe condition', async () => { id = (await data(page, 'search.sources.list')).find((s: any) => s.name === name)?.id ?? ''; return id; }, value => value !== '');
    const row = page.locator('[data-source]').filter({ hasText: name });
    await row.getByRole('switch').click();
    await observe('toBe condition', async () => (await data(page, 'search.sources.list')).find((s: any) => s.id === id).enabled, value => value === false);
    await row.getByRole('switch').click();
    await observe('toBe condition', async () => (await data(page, 'search.sources.list')).find((s: any) => s.id === id).enabled, value => value === true);
    await row.locator('[data-source-menu]').click();
    await page.getByRole('menuitem', { name: text('Test', '測試'), exact: true }).click();
    await observe('toBe condition', async () => {
      const jobs = (await data(page, 'search.jobs.list')).jobs;
      return jobs.find((j: any) => j.operation === 'source-test' && j.sourceId === id)?.state;
    }, value => value === 'succeeded');
    await row.locator('[data-source-menu]').click();
    await page.getByRole('menuitem', { name: text('Edit', '編輯'), exact: true }).click();
    await page.getByRole('dialog').last().getByLabel(text('Name', '名稱')).fill(`${name} edited`);
    await page.getByRole('dialog').last().getByRole('button', { name: text('Save', '儲存'), exact: true }).click();
    await observe('toBe condition', async () => (await data(page, 'search.sources.list')).find((s: any) => s.id === id).name, value => value === `${name} edited`);
    await row.locator('[data-source-menu]').click();
    await page.getByRole('menuitem', { name: text('Delete', '刪除'), exact: true }).click();
    await page.getByRole('dialog').last().getByRole('button', { name: text('Delete', '刪除'), exact: true }).click();
    await observe('toBe condition', async () => (await data(page, 'search.sources.list')).some((s: any) => s.id === id), value => value === false); id = '';
  }, async step => {
    await step(async () => {
      for (const job of (await data(page, 'search.jobs.list')).jobs) if (job.sourceId === id)
        await settleSearchJob((method, input) => data(page, method, input), job.id);
    });
    await step(async () => { for (const source of await data(page, 'search.sources.list')) if (source.id === id || source.name === name || source.name === `${name} edited`) await data(page, 'search.sources.delete', { id: source.id }); });
  });
});
