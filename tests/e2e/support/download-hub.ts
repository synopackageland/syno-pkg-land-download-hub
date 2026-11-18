// Download Hub on the NAS: its window, its backend called in the signed-in
// DSM session the way the App's own frontend calls it, and the fixtures the
// suites own. Fixtures are named after E2E_FIXTURE_TAG and removed by the
// suite that made them.
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import type { Page } from 'playwright';
import { expect } from 'e2e';
import { openApp, openPortal, dismissNotifications } from '@synopackageland/cli/e2e';
import { observe } from './work-poll.ts';

type App = { open(path?: string): Promise<unknown> };

export const appId = 'SYNO.SDS.SynoPkgGopeed.Instance';
export const root = '.gopeed-root';
export const portalAlias = '/download-hub/';
export const tag = process.env.E2E_FIXTURE_TAG ?? 'e2e';
/**
 * E2E_KEEP_APP_STATE=1 skips the tests that change what other users of the
 * App see: its settings, extensions, core, background-save approval, the
 * package's running state, and the admin's save folder.
 */
export const changesApp = process.env.E2E_KEEP_APP_STATE === '1' ? 'E2E_KEEP_APP_STATE=1' : false;

/** A known small file: Gopeed's LICENSE at v1.9.3. */
export const license = {
  url: 'https://raw.githubusercontent.com/GopeedLab/gopeed/v1.9.3/LICENSE',
  sha256: '3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986',
};
/** Downloads large enough to pause: the Gopeed web builds for linux. */
export const largeFile = 'https://github.com/GopeedLab/gopeed/releases/download/v1.9.3/gopeed-web-v1.9.3-linux-amd64.zip';
/** A file that is still downloading when the package stops: Debian's 12.7 netinst image, about 630 MB. */
export const mediumFile = 'https://cdimage.debian.org/cdimage/archive/12.7.0/amd64/iso-cd/debian-12.7.0-amd64-netinst.iso';
/** A file too large to finish before a pause arrives: Ubuntu's server image, about 3 GB. */
export const hugeFile = 'https://releases.ubuntu.com/24.04.3/ubuntu-24.04.3-live-server-amd64.iso';
/** DSM's SDK says this for a temporary failure only; never for a refusal or a stopped package. */
export const temporary = /This is usually temporary|這通常是暫時的/;

/** The exact English or Traditional Chinese text: DSM accounts use either. */
export function text(en: string, zh: string): RegExp {
  const quote = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp(`^(${quote(en)}|${quote(zh)})$`);
}

/** Opens Download Hub's DSM window and returns the page once the App has rendered. */
export async function openDownloadHub(app: App): Promise<Page> {
  const window = await openApp(app, appId);
  await window.locator(root).waitFor({ state: 'visible', timeout: 60_000 });
  return window.page();
}

/** Opens Download Hub's Application Portal page as the whole tab. */
export async function openDownloadHubPortal(app: App): Promise<Page> {
  const page = await openPortal(app, portalAlias);
  await page.locator(root).waitFor({ state: 'visible', timeout: 60_000 });
  return page;
}

/**
 * Goes to a sidebar page (Downloads, Extensions, Core updates, Settings). The
 * App ignores the sidebar while it loads a page, so the click repeats until
 * the page changes; while the App is covered (a package starting), each
 * click gives up quickly and repeats.
 */
export async function goTo(page: Page, en: string, zh: string): Promise<void> {
  const item = page.locator('button[data-nav-item]').filter({ hasText: text(en, zh) });
  await expect.poll(async () => {
    const current = () => item.getAttribute('aria-current', { timeout: 2_000 }).catch(() => null);
    if (await current() === 'page') return true;
    await item.click({ timeout: 2_000 }).catch(() => undefined);
    return await current() === 'page';
  }, { timeout: 60_000, interval: 1_000 }).toBe(true);
}

/** Confirms the App's open confirmation box (Cancel / Confirm). */
export async function confirm(page: Page, button: RegExp = text('Confirm', '確認')): Promise<void> {
  await page.getByRole('dialog').last().getByRole('button', { name: button }).click();
}

export type Reply = { success: true; data: any } | { success: false; code: string };

/** One backend method in the page's DSM session; a refusal returns its error code. */
export function call(page: Page, method: string, params: unknown = {}, packageId = 'SynoPkgGopeed'): Promise<Reply> {
  return page.evaluate(async ({ method, params, packageId }) => {
    const response = await fetch(`/webman/3rdparty/${packageId}/cgi/api.cgi`, {
      method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-SYNO-TOKEN': (window as any).SYNO.SDS.Session.SynoToken },
      body: JSON.stringify({ method, params }),
    });
    const body = await response.json();
    return body.success ? { success: true, data: body.data } : { success: false, code: body.error?.code };
  }, { method, params, packageId });
}

/** The data of a method that must succeed. */
export async function data(page: Page, method: string, params: unknown = {}, packageId = 'SynoPkgGopeed'): Promise<any> {
  const reply = await call(page, method, params, packageId);
  if (!reply.success) throw new Error(`${method} refused: ${reply.code}`);
  return reply.data;
}

/** The Gopeed engine API through the App's per-user proxy (gopeed.request). */
export async function engine(page: Page, method: string, path: string, body: unknown = null): Promise<any> {
  return (await data(page, 'gopeed.request', { method, path, query: {}, data: body })).data;
}

/**
 * Starts a download into a folder the way the New download dialog does,
 * which first makes the folder the background-save destination; returns its
 * engine task id. The caller restores the destination (saveFolder). An engine
 * still starting (after a package restart) is waited for.
 */
export async function createDownload(page: Page, path: string, url: string): Promise<string> {
  await data(page, 'gopeed.destination', { key: 'save', path });
  await data(page, 'downloads.folder.set', { subfolder: '' });
  for (const end = Date.now() + 60_000; ; await page.waitForTimeout(2_000)) {
    const key = `e2e-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const [result] = (await data(page, 'downloads.create_many', { path, items: [{ key, url }] })).results ?? [];
    if (result?.status === 'created') return engineTask(page, key, result.ids[0]);
    if (result?.code !== 'engine_unavailable' || Date.now() > end) throw new Error(`${url} was not created: ${result?.code}`);
  }
}

/**
 * The engine task a submission became. create_many answers a preparation id
 * (prepare-…) at once; once the link is resolved the engine task carries the
 * submission's key as its batch, and the preparation id stops answering.
 */
async function engineTask(page: Page, key: string, prepared: string): Promise<string> {
  if (!prepared.startsWith('prepare-')) return prepared;
  let id = '';
  await expect.poll(async () => {
    id = (await tasks(page)).find(t => t.publication?.batch === key && !String(t.id).startsWith('prepare-'))?.id ?? '';
    return id;
  }, { timeout: 60_000, interval: 1_000 }).not.toBe('');
  return id;
}

/** The background-save folder, and a way to put it back. */
export async function saveFolder(page: Page): Promise<(page: Page) => Promise<void>> {
  const before = await data(page, 'downloads.location');
  return async (page: Page) => { if (before.destination?.path) {
    await data(page, 'gopeed.destination', { key: 'save', path: before.destination.path });
    await data(page, 'downloads.folder.set', { subfolder: before.subfolder ?? '' });
  } };
}

/** The rows of the grouped downloads list; `name` picks the rows naming it. */
export const downloadRows = (page: Page, name?: string) => {
  const rows = page.locator('[data-downloads-list] [data-row-key]');
  return name ? rows.filter({ hasText: name }) : rows;
};
/** A row's button that opens File Station at the saved file. */
export const revealButton = (row: ReturnType<Page['locator']>) => row.getByRole('button', { name: text('Open in File Station', '在 File Station 開啟') }).first();
/** The detail panel of the row the user opened. */
export const taskPanel = (page: Page) => page.locator('[data-task-panel]');
/** A row's button that clears its record; the saved file stays on the NAS. */
export const clearRecord = text('Clear download record', '清除下載紀錄');

/** The folder a download saves to. */
export const folderOf = (task: any): string => task.publication?.folder ?? task.meta?.opts?.path;

/**
 * A saved download of this run's own: LICENSE in a new folder of the save
 * scope. `remove` deletes the record and the folder and puts the save
 * folder back.
 */
export async function savedDownload(page: Page): Promise<{ id: string; folder: string; remove(page: Page): Promise<void> }> {
  const restoreFolder = await saveFolder(page);
  const scope: string = (await data(page, 'delegate.destination', { key: 'save' })).selection.root;
  const name = `${tag}-saved-${Date.now()}`;
  const folder = await createFolder(page, scope, name);
  let id = '';
  const remove = async (page: Page) => {
    if (id) await engine(page, 'DELETE', `/api/v1/tasks/${id}`).catch(() => undefined);
    await restoreFolder(page);
    await removeFromNas(page, [folder]);
  };
  try {
    id = await createDownload(page, folder, license.url);
    await expect.poll(async () => (await task(page, id)).publication?.published, { timeout: 120_000 }).toBe(true);
  } catch (error) {
    await remove(page);
    throw error;
  }
  return { id, folder, remove };
}

export const tasks = (page: Page): Promise<any[]> => engine(page, 'GET', '/api/v1/tasks');
export const task = (page: Page, id: string): Promise<any> => engine(page, 'GET', `/api/v1/tasks/${id}`);

/** Refresh browser-owned destination state after API fixture provisioning. */
export async function refreshFixture(page: Page, app: App): Promise<void> {
  await page.reload();
  await openDownloadHub(app);
  await dismissNotifications(page);
  await observe('fixture UI destination', async () => {
    const current = await data(page, 'downloads.location');
    return current.status === 'active' && await page.locator('[data-add-download]').isEnabled();
  }, ready => ready);
}

/** Wait for cancellation/removal before deleting an owned staging folder. */
export async function deleteFixtureTasks(page: Page, owns: (row: any) => boolean): Promise<void> {
  const ids = (await tasks(page)).filter(owns).map(row => row.id);
  for (const id of ids) await engine(page, 'DELETE', `/api/v1/tasks/${id}`);
  for (const id of ids) await observe('fixture removal receipt', () => data(page, 'tasks.removal', { id }), receipt => receipt.state === 'removed');
  await observe('fixture task deletion', async () => (await tasks(page)).some(row => ids.includes(row.id)), present => !present);
}

/**
 * One DSM File Station call in the page's own DSM session. The App's backend
 * no longer reaches File Station (SDK 0.49), so the suite's fixtures go
 * through DSM directly, as the browser SDK does.
 */
export async function fileStation(page: Page, api: string, method: string, params: Record<string, string>, version = '2'): Promise<any> {
  const reply = await page.evaluate(async ({ api, method, params, version }) => {
    const token = (window as any).SYNO.SDS.Session.SynoToken;
    const response = await fetch('/webapi/entry.cgi', {
      method: 'POST', credentials: 'same-origin', headers: { 'X-SYNO-TOKEN': token },
      body: new URLSearchParams({ api, version, method, ...params }),
    });
    return response.json();
  }, { api, method, params, version });
  if (!reply.success) throw new Error(`${api} ${method} refused: ${reply.error?.code}`);
  return reply.data;
}

/** Creates this suite's own folder under `parent`. */
export async function createFolder(page: Page, parent: string, name: string): Promise<string> {
  if (!name.startsWith(tag)) throw new Error(`Only ${tag}* fixtures may be created`);
  await fileStation(page, 'SYNO.FileStation.CreateFolder', 'create', { folder_path: JSON.stringify([parent]), name: JSON.stringify([name]) });
  return `${parent}/${name}`;
}

/** A NAS file's File Station entry; throws when it is missing. */
export async function fileInfo(page: Page, path: string): Promise<any> {
  const entry = (await fileStation(page, 'SYNO.FileStation.List', 'getinfo', { path: JSON.stringify([path]), additional: '["size"]' })).files?.[0];
  if (!entry || entry.code) throw new Error(`${path} is missing: ${entry?.code}`);
  return entry;
}

/** SHA-256 of a NAS file, read with DSM File Station in this session. */
export async function sha256(page: Page, path: string): Promise<string> {
  const bytes = await page.evaluate(async path => {
    const token = (window as any).SYNO.SDS.Session.SynoToken;
    const response = await fetch('/webapi/entry.cgi', {
      method: 'POST', credentials: 'same-origin', headers: { 'X-SYNO-TOKEN': token },
      body: new URLSearchParams({ api: 'SYNO.FileStation.Download', version: '2', method: 'download', path: JSON.stringify([path]), mode: 'download' }),
    });
    if (!response.ok || response.headers.get('Content-Disposition') === null) throw new Error(`download of ${path} refused`);
    return [...new Uint8Array(await response.arrayBuffer())];
  }, path);
  return createHash('sha256').update(Buffer.from(bytes)).digest('hex');
}

/** Deletes this suite's own NAS paths with DSM File Station in the page's session. */
export async function removeFromNas(page: Page, paths: string[]): Promise<void> {
  if (paths.some(path => !path.split('/').pop()!.startsWith(tag))) throw new Error(`Only ${tag}* fixtures may be deleted`);
  await page.evaluate(async paths => {
    const token = (window as any).SYNO.SDS.Session.SynoToken;
    const webapi = async (params: Record<string, string>) => (await fetch('/webapi/entry.cgi', {
      method: 'POST', credentials: 'same-origin', headers: { 'X-SYNO-TOKEN': token },
      body: new URLSearchParams({ api: 'SYNO.FileStation.Delete', version: '2', ...params }),
    })).json();
    const started = await webapi({ method: 'start', path: JSON.stringify(paths), recursive: 'true' });
    if (!started.success) throw new Error(`File Station delete refused: ${started.error?.code}`);
    for (let poll = 0; poll < 60; poll++) {
      const status = await webapi({ method: 'status', taskid: JSON.stringify(started.data.taskid) });
      if (status.data?.finished) return;
      await new Promise(wait => setTimeout(wait, 500));
    }
    throw new Error('File Station delete did not finish');
  }, paths);
}

/** Values one test leaves for a later one in the same run (E2E_RUN_DIR). */
export const shared = {
  file: () => join(process.env.E2E_RUN_DIR ?? '.cache/e2e', 'shared.json'),
  async load(): Promise<Record<string, any>> {
    return JSON.parse(await readFile(this.file(), 'utf8').catch(() => '{}'));
  },
  async save(key: string, value: unknown): Promise<void> {
    const all = await this.load();
    await mkdir(process.env.E2E_RUN_DIR ?? '.cache/e2e', { recursive: true });
    await writeFile(this.file(), JSON.stringify({ ...all, [key]: value }, null, 2), { mode: 0o600 });
  },
};
