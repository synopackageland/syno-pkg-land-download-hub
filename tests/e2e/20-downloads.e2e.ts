// Downloads: the New download dialog (links, the folder it saves to, a narrow
// window), the grouped list (Downloading, Paused, Failed, Recently completed;
// the name filter; the detail panel; "Waiting to save" until Saved; the
// button that opens File Station; a saved text file's preview; clearing
// records keeps the files), and each user's downloads staying their own.
import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import type { Page } from 'playwright';
import { openDesktop } from '@synopackageland/cli/e2e';
import {
  appId, call, changesApp, confirm, createDownload, data, downloadRows, folderOf, license, openDownloadHub, removeFromNas, revealButton, saveFolder, sha256, shared,
  tag, task, taskPanel, tasks, text, createFolder,
} from './support/download-hub.ts';

const dialog = (page: Page) => page.getByRole('dialog', { name: text('New download', '新增下載') });
const newDownload = (page: Page) => page.getByRole('button', { name: text('New download', '新增下載') });
const row = downloadRows;
/** A small text file the preview reads: Gopeed's README at v1.9.3. */
const previewFile = { url: 'https://raw.githubusercontent.com/GopeedLab/gopeed/v1.9.3/README.md', contains: 'Gopeed' };

/** Resizes Download Hub's DSM window, the way a user drags its corner. */
const resizeWindow = (page: Page, width: number, height: number) => page.evaluate(({ appId, width, height }) =>
  void (window as any).SYNO.SDS.AppMgr.getByAppName(appId)[0].window.setSize(width, height), { appId, width, height });

/** The list's groups in page order, each with the count its header shows. */
const groupCounts = (page: Page) => page.locator('[data-downloads-list] [data-group]').evaluateAll(groups =>
  groups.map(g => [(g as HTMLElement).dataset.group, Number(g.querySelector('.gopeed-dl-count')?.textContent)] as const));

test('the downloads list groups every download by state, in a fixed order', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const count = (await tasks(page)).length;
  test.skip(!count, 'no downloads to list');
  // The headers count every download the engine reports for this user; empty groups are left out.
  await expect.poll(async () => (await groupCounts(page)).reduce((n, [, c]) => n + c, 0), { timeout: 15_000 }).toBe(count);
  const order = ['active', 'paused', 'failed', 'done'];
  const shown = (await groupCounts(page)).map(([key]) => key!);
  expect(shown).toEqual(order.filter(key => shown.includes(key)));
  // A header folds its group away and back.
  const first = page.locator('[data-downloads-list] [data-group]').first();
  const toggle = first.locator('button[aria-expanded]');
  await toggle.click();
  await expect.poll(() => toggle.getAttribute('aria-expanded')).toBe('false');
  await expect.poll(() => first.locator('[data-row-key]').first().isVisible()).toBe(false);
  await toggle.click();
  await expect.poll(() => toggle.getAttribute('aria-expanded')).toBe('true');
});

test('in a narrow window, the New download dialog stays inside the viewport and usable', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDownloadHub(app);
  await resizeWindow(page, 420, 640);
  await newDownload(page).click();
  const box = dialog(page);
  await box.getByRole('textbox', { name: text('Links', '下載連結') }).fill(license.url);
  await expect.poll(() => box.locator('.add-location').innerText()).toMatch(/Save to|存放位置/);
  // Every row of the dialog fits its width: the wrapping rows break instead of overflowing.
  const overflowing = await box.evaluate(dialog => [...dialog.querySelectorAll('.add-links-head, .add-links-foot, .add-location')]
    .filter(row => row.scrollWidth > row.clientWidth + 1).map(row => row.className));
  // A floating dialog may reach past the App's window, as DSM's do, but stays inside the viewport.
  const inner = (await box.boundingBox())!;
  const { width, height } = page.viewportSize()!;
  try {
    const [left, top, right, bottom] = [inner.x, inner.y, inner.x + inner.width, inner.y + inner.height].map(Math.round);
    expect({ dialog: [left, top, right, bottom], viewport: [width, height], inside: left >= 0 && top >= 0 && right <= width && bottom <= height })
      .toMatchObject({ inside: true });
    expect(overflowing).toEqual([]);
    // Its controls take input: Start can be clicked, and the folder can be changed.
    await box.getByRole('button', { name: /^(Add|加入)( \d+ (tasks?|個任務))?$/ }).click({ trial: true });
    await box.getByRole('button', { name: text('Change', '變更') }).click({ trial: true });
  } finally {
    await page.keyboard.press('Escape');
    await expect.poll(() => box.count()).toBe(0);
    await resizeWindow(page, 1000, 680);
  }
});

test('admin downloads into a folder chosen in the dialog, and the file is saved there', { session: 'admin', timeout: 600_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const before = await data(page, 'downloads.location');
  const scope: string = before.selection.root;
  const folder = `${tag}-downloads-${Date.now()}`;
  const path = await createFolder(page, scope, folder);
  try {
    await newDownload(page).click();
    const box = dialog(page);
    await box.getByRole('textbox', { name: text('Links', '下載連結') }).fill(license.url);
    await box.getByRole('button', { name: text('Change', '變更') }).click();
    await page.locator('[data-another-folder]').click();
    const picker = page.getByRole('dialog', { name: text('Choose folder', '選擇資料夾') });
    // The picker opens where it was last, or at the top: open the scope to reach the new folder.
    const target = picker.locator(`tr[data-row-key="${path}"]`);
    await expect.poll(async () => {
      if (!await target.count()) await picker.locator(`tr[data-row-key="${scope}"]`).dblclick({ timeout: 2_000 }).catch(() => undefined);
      return target.count();
    }, { timeout: 30_000, interval: 1_000 }).toBe(1);
    await target.click();
    await picker.getByRole('button', { name: text('Confirm selection', '確認選取') }).click();
    await page.locator('[data-download-subfolder] input,input[data-download-subfolder]').fill('');
    await page.locator('[data-confirm-download-folder]').click();
    await expect.poll(() => box.getByText(path).count(), { timeout: 15_000 }).toBeGreaterThan(0);
    // The button counts the links: "Add 1 task".
    await box.getByRole('button', { name: /^(Add|加入)( \d+ (tasks?|個任務))?$/ }).click();
    await expect.poll(() => box.count(), { timeout: 30_000 }).toBe(0);

    // The new download waits to save until its file is in the folder.
    // The submission shows as a preparation (prepare-…) until its engine task exists.
    const engineTask = async () => (await tasks(page)).find(t => folderOf(t) === path && !String(t.id).startsWith('prepare-'))?.id ?? '';
    await expect.poll(engineTask, { timeout: 60_000 }).not.toBe('');
    const id = await engineTask();
    const status = row(page, 'LICENSE').first().locator('[data-row-status]');
    await expect.poll(() => status.textContent(), { timeout: 120_000 }).toMatch(/Saved|已存入/);
    // A saved download sits in Recently completed.
    await expect.poll(() => page.locator('[data-group=done]').locator('[data-row-key]').filter({ hasText: 'LICENSE' }).count()).toBeGreaterThan(0);
    expect((await task(page, id)).publication?.published).toBe(true);
    expect(await sha256(page, `${path}/LICENSE`)).toBe(license.sha256);
    await shared.save('adminDownload', id);

    // The row's folder button opens File Station at the saved file.
    await revealButton(row(page, 'LICENSE').first()).click();
    await expect.poll(() => page.evaluate(() => !!(window as any).SYNO.SDS.AppMgr.getByAppName('SYNO.SDS.App.FileStation3.Instance')?.[0]),
      { timeout: 30_000 }).toBe(true);
  } finally {
    if (before.destination?.path) {
      await call(page, 'gopeed.destination', { key: 'save', path: before.destination.path });
      await call(page, 'downloads.folder.set', { subfolder: before.subfolder ?? '' });
    }
    await shared.save('adminFolder', path);
  }
});

test('normal1 cannot read, save or reveal the admin\'s downloads', { session: 'normal1', timeout: 120_000 }, async ({ app }) => {
  const page = await openDesktop(app);
  test.skip(!(await call(page, 'gopeed.session')).success, 'normal1 has no Application Privileges on Download Hub');
  const id = (await shared.load()).adminDownload;
  test.skip(!id, 'the admin download test did not run');
  expect((await call(page, 'gopeed.request', { method: 'GET', path: `/api/v1/tasks/${id}`, query: {}, data: null })).success).toBe(false);
  expect(await call(page, 'gopeed.publish', { id })).toEqual({ success: false, code: 'task_denied' });
  expect(await call(page, 'gopeed.reveal', { id })).toEqual({ success: false, code: 'task_denied' });
  expect((await tasks(page)).some(t => t.id === id)).toBe(false);
});

test('the list finds a download by name, and a row opens its detail panel', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const all = await tasks(page);
  test.skip(all.length < 2, 'fewer than two downloads to filter');
  const name: string = all[0].publication?.filename ?? all[0].meta?.res?.name ?? all[0].meta?.opts?.name;
  const filter = page.locator('input[data-downloads-filter], [data-downloads-filter] input').first();
  try {
    // The filter keeps the rows naming it, in every group.
    await filter.fill(name);
    await expect.poll(async () => (await downloadRows(page).allInnerTexts()).every(row => row.includes(name)), { timeout: 15_000 }).toBe(true);
    expect(await downloadRows(page, name).count()).toBeGreaterThan(0);

    // A row opens the panel beside the list; Close takes it away.
    await downloadRows(page, name).first().click();
    const panel = taskPanel(page);
    await expect.poll(() => panel.getAttribute('aria-label'), { timeout: 15_000 }).toBe(name);
    await expect.poll(() => downloadRows(page, name).first().getAttribute('aria-current')).toBe('true');
    // The panel sits to the right of the list; below 820 px of Downloads area
    // it covers the list's right side instead. Either way it stays in the area.
    const [list, side] = [await page.locator('[data-downloads-list]').boundingBox(), await panel.boundingBox()];
    if (await panel.evaluate(el => getComputedStyle(el).position) === 'absolute') {
      expect(side!.x).toBeGreaterThanOrEqual(list!.x - 1);
      expect(side!.x + side!.width).toBeLessThanOrEqual(list!.x + list!.width + 1);
    } else {
      expect(side!.x).toBeGreaterThanOrEqual(list!.x + list!.width - 1);
    }
    await panel.getByRole('button', { name: text('Close', '關閉') }).click();
    await expect.poll(() => panel.count()).toBe(0);
  } finally {
    await filter.fill('');
  }
});

// The page fills the window: its header stays in view and only the list scrolls.
test('the downloads list scrolls its rows under a header that stays in view', { session: 'admin', timeout: 120_000 }, async ({ app }) => {
  const page = await openDownloadHub(app);
  test.skip((await tasks(page)).length < 1, 'no downloads to list');
  const place = () => page.evaluate(() => {
    const root = document.querySelector('[data-downloads]')!.closest('.app-page-fill')!.getBoundingClientRect();
    const header = document.querySelector('[data-ui-page-header]')!.getBoundingClientRect();
    return { inView: header.top >= root.top - 1 && header.bottom <= root.bottom + 1, top: Math.round(header.top) };
  });
  const before = await place();
  expect(before.inView).toBe(true);
  // Each row is one name line, a progress bar when it moves, and one meta line.
  const heights = await downloadRows(page).evaluateAll(rows => rows.map(row => Math.round(row.getBoundingClientRect().height)));
  expect(Math.max(...heights)).toBeLessThan(80);
  await downloadRows(page).last().scrollIntoViewIfNeeded();
  expect(await place()).toEqual(before);
});

// Preview reads the saved file through File Station in the user's own DSM
// session; this run's own text file, deleted afterwards.
test('a saved text file previews in the App and opens File Station from the preview', { session: 'admin', timeout: 300_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const restore = await saveFolder(page);
  const scope: string = (await data(page, 'delegate.destination', { key: 'save' })).selection.root;
  const folder = `${tag}-preview-${Date.now()}`;
  const path = await createFolder(page, scope, folder);
  let id = '';
  try {
    id = await createDownload(page, path, previewFile.url);
    await expect.poll(async () => (await task(page, id)).publication?.published, { timeout: 120_000 }).toBe(true);
    const saved = page.locator(`[data-downloads-list] [data-row-key="${id}"]`);
    const reads: string[] = [];
    const listen = (r: any) => {
      if (`${r.url()} ${r.postData() ?? ''}`.includes('SYNO.FileStation.Download')) reads.push(new URL(r.url()).origin + new URL(r.url()).pathname);
    };
    page.on('request', listen);
    await saved.getByRole('button', { name: /^(Preview|預覽) / }).click();
    const preview = page.locator('[data-preview]');
    await expect.poll(() => preview.locator('[data-preview-state]').getAttribute('data-preview-state'), { timeout: 30_000 }).toBe('ready');
    // The bytes are the saved file's, read in this session.
    await expect.poll(() => preview.locator('[data-preview-text]').innerText()).toContain(previewFile.contains);
    // Every read went to DSM's own File Station entry on this origin.
    page.off('request', listen);
    const origin = new URL(page.url()).origin;
    expect(reads.length).toBeGreaterThan(0);
    expect([...new Set(reads)]).toEqual([`${origin}/webapi/entry.cgi`]);
    await preview.locator('[data-preview-reveal]').click();
    await expect.poll(() => preview.count()).toBe(0);
    await expect.poll(() => page.evaluate(() => !!(window as any).SYNO.SDS.AppMgr.getByAppName('SYNO.SDS.App.FileStation3.Instance')?.[0]),
      { timeout: 30_000 }).toBe(true);
  } finally {
    if (id) await call(page, 'gopeed.request', { method: 'DELETE', path: `/api/v1/tasks/${id}`, query: {}, data: null });
    await restore(page);
    await removeFromNas(page, [path]);
  }
});

test('Clear completed clears the saved records and keeps their files', { session: 'admin', timeout: 120_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const { adminDownload: id, adminFolder: path } = await shared.load();
  test.skip(!path, 'the admin download test did not run');
  try {
    if (id && (await tasks(page)).some(t => t.id === id)) {
      const saved = (await tasks(page)).filter(t => t.status === 'done' && t.publication?.published && !t.publication?.deleting);
      // The Recently completed header clears them.
      const clear = page.locator('[data-group=done] [data-group-action=clear]');
      await expect.poll(() => clear.innerText()).toMatch(new RegExp(`^(Clear completed|清除已完成) \\(${saved.length}\\)$`));
      await clear.click();
      const box = page.getByRole('dialog').last();
      // The dialog says the saved files stay, within its longer sentence.
      await expect.poll(() => box.innerText()).toMatch(/Files already saved to the NAS stay there|已存入 NAS 的檔案會保留/);
      await confirm(page);
      await expect.poll(async () => (await tasks(page)).filter(t => saved.some(s => s.id === t.id)).length, { timeout: 60_000 }).toBe(0);
      expect(await sha256(page, `${path}/LICENSE`)).toBe(license.sha256);
    }
  } finally {
    await removeFromNas(page, [path]);
  }
});

// No media probe or peer metadata is needed to admit a source. These unique
// magnets have no fixture peers: pause/resume exercises preparing tasks safely.
test('new links appear immediately and preparing tasks pause and resume from their rows', { session: 'admin', timeout: 120_000, skip: changesApp }, async ({ app }) => {
  const page = await openDownloadHub(app);
  const destination = await data(page, 'delegate.destination', { key: 'save' });
  test.skip(destination.status !== 'active', 'choose a background destination first');
  const nonce = Date.now().toString(16).padStart(40, '0');
  const items = [0, 1].map(n => ({ key: `${tag}-instant-${Date.now()}-${n}`, url: `magnet:?xt=urn:btih:${nonce.slice(0, -1)}${n}` }));
  const ids: string[] = [];
  try {
    const start = Date.now();
    const result = await data(page, 'downloads.create_many', { path: destination.destination.path, items });
    expect(result.results.every((r: any) => r.status === 'created')).toBe(true);
    ids.push(...result.results.flatMap((r: any) => r.ids));
    // Includes fresh DSM destination/ACL checks; excludes source metadata.
    expect(Date.now() - start).toBeLessThan(10_000);
    for (const id of ids) await data(page, 'gopeed.request', { method: 'PUT', path: `/api/v1/tasks/${id}/pause`, query: {}, data: null });
    await expect.poll(async () => (await tasks(page)).filter(t => ids.includes(t.id) && t.status === 'pause').length).toBe(2);
    await page.getByRole('button', { name: text('Refresh', '重新整理'), exact: true }).click();
    // A paused task sits in Paused with Resume; a resumed one moves to Downloading with Pause.
    const own = (id: string) => page.locator(`[data-downloads-list] [data-row-key="${id}"]`);
    for (const id of ids) {
      await expect.poll(() => own(id).evaluate(row => row.closest('[data-group]')?.getAttribute('data-group')), { timeout: 15_000 }).toBe('paused');
      await own(id).getByRole('button', { name: text('Resume', '繼續') }).click();
    }
    await expect.poll(async () => (await tasks(page)).filter(t => ids.includes(t.id) && t.status === 'preparing').length).toBe(2);
    for (const id of ids) {
      await expect.poll(() => own(id).evaluate(row => row.closest('[data-group]')?.getAttribute('data-group')), { timeout: 15_000 }).toBe('active');
      await own(id).getByRole('button', { name: text('Pause', '暫停') }).click();
    }
    await expect.poll(async () => (await tasks(page)).filter(t => ids.includes(t.id) && t.status === 'pause').length).toBe(2);
  } finally {
    for (const id of ids) await call(page, 'gopeed.request', { method: 'DELETE', path: `/api/v1/tasks/${id}`, query: {}, data: null });
    await expect.poll(async () => (await tasks(page)).filter(t => ids.includes(t.id)).length, { timeout: 30_000 }).toBe(0);
  }
});
