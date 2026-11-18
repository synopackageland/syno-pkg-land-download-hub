import { test } from '@e2e-dev/web';
import { expect } from 'e2e';
import { randomUUID, createHash } from 'node:crypto';
import { createFolder, data, deleteFixtureTasks, openDownloadHub, removeFromNas, saveFolder, sha256, tag, task, tasks } from './support/download-hub.ts';
import { observe, settleSearchJob } from './support/work-poll.ts';
import { withCleanup } from './support/cleanup.ts';
import { commands, ctl, fixtureURL, prepareSearchCancellation } from './support/real-ctl.ts';
import { tinySHA256, tiny } from './support/fixture-server.ts';

// Missing prerequisites fail visibly. A skipped fixture is not successful ctl
// coverage. The caller provisions/approves the package before this one-shot run.
for (const [operation, command] of Object.entries(commands) as [string, any][]) {
  if (!operation.endsWith('.preview')) continue;
  test(`real CLI preview: ${operation}`, { session: 'admin' }, async ({ app }) => {
    const page = await openDownloadHub(app);
    const before = (await tasks(page)).map(t => t.id).sort();
    const result = await ctl(operation, command.examples[0]);
    expect(result).toMatchObject({ operation: operation.slice(0, -8), confirmationRequired: true, checks: 'execution_required' });
    expect((await tasks(page)).map(t => t.id).sort()).toEqual(before);
  });
}

test('real CLI diagnostics agree with the installed package', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app);
  expect(await ctl('gopeed.session')).toMatchObject({ admin: true, nonroot: true });
  expect(await ctl('binaries.status')).toMatchObject({ ready: true, active: { version: (await data(page, 'binaries.status')).active.version } });
  expect(await ctl('media.tools.status')).toHaveProperty('arch');
  expect((await ctl('search.catalog')).generation).toBe((await data(page, 'search.catalog')).generation);
  expect((await ctl('gopeed.plugins.list')).map((p: any) => p.identity).sort()).toEqual((await data(page, 'gopeed.plugins.list')).map((p: any) => p.identity).sort());
  expect(Array.isArray(await ctl('gopeed.plugins.catalog'))).toBe(true);
});

test('real CLI download grows, pauses, continues from its bytes, and cancels', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), restore = await saveFolder(page);
  const scope = (await data(page, 'delegate.destination', { key: 'save' })).selection.root;
  const folder = await createFolder(page, scope, `${tag}-ctl-${Date.now()}`);
  const key = `ctl-${randomUUID()}`;
  const owned = async () => (await tasks(page)).filter(t => t.publication?.batch === key);
  let id = '';
  await withCleanup(async () => {
    await ctl('gopeed.destination', { path: folder });
    await data(page, 'downloads.folder.set', { subfolder: '' });
    expect(await ctl('downloads.destination')).toMatchObject({ status: 'active' });
    const accepted = await ctl('downloads.add', { key, url: fixtureURL('/slow.bin') });
    expect(accepted).toMatchObject({ job_id: key, accepted: true, completion: 'not_checked' });
    await observe('toBe condition', async () => { id = (await owned()).find(t => !t.id.startsWith('prepare-'))?.id ?? ''; return id; }, value => value !== '');
    expect((await ctl('downloads.status', { job_id: key })).ids).toContain(id);
    await observe('toBeGreaterThan condition', async () => (await ctl('tasks.get', { id })).downloaded, value => value > 0);
    const first = (await ctl('tasks.get', { id })).downloaded;
    await observe('toBeGreaterThan condition', async () => (await ctl('tasks.get', { id })).downloaded, value => value > first);
    await ctl('tasks.pause', { id });
    await observe('toBe condition', async () => (await ctl('tasks.get', { id })).status, value => value === 'pause');
    const paused = (await ctl('tasks.get', { id })).downloaded;
    await page.waitForTimeout(2_000);
    expect((await ctl('tasks.get', { id })).downloaded).toBe(paused);
    await ctl('tasks.resume', { id });
    // Check the first observed running state as well as subsequent growth:
    // merely exceeding the old value later could hide a reset to byte zero.
    await observe('toBeGreaterThan condition', async () => {
      const current = await ctl('tasks.get', { id });
      expect(current.downloaded).toBeGreaterThanOrEqual(paused);
      return current.downloaded;
    }, value => value > paused);
    expect((await ctl('tasks.list')).tasks.some((t: any) => t.id === id)).toBe(true);
    await ctl('tasks.delete', { id });
    await observe('toBe condition', async () => (await ctl('tasks.removal', { id })).state, value => value === 'removed');
    expect((await ctl('tasks.list')).tasks.some((t: any) => t.id === id)).toBe(false);
  }, async step => {
    await step(() => deleteFixtureTasks(page, row => row.publication?.batch === key));
    await step(async () => { await restore(page); });
    await step(async () => { await removeFromNas(page, [folder]); });
  });
});

test('real CLI tiny download saves exact bytes, reveals it, and deletion keeps the file', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), restore = await saveFolder(page);
  const scope = (await data(page, 'delegate.destination', { key: 'save' })).selection.root;
  const folder = await createFolder(page, scope, `${tag}-receipt-${Date.now()}`), key = `ctl-${randomUUID()}`;
  const owned = async () => (await tasks(page)).filter(t => t.publication?.batch === key);
  let id = '';
  await withCleanup(async () => {
    await ctl('gopeed.destination', { path: folder });
    await data(page, 'downloads.folder.set', { subfolder: '' });
    await ctl('downloads.add', { key, url: fixtureURL('/tiny.txt') });
    await observe('toBe condition', async () => { id = (await owned()).find(t => !t.id.startsWith('prepare-'))?.id ?? ''; return id && (await task(page, id)).publication?.published; }, value => value === true);
    expect((await ctl('tasks.get', { id })).published).toBe(true);
    expect(await sha256(page, `${folder}/tiny.txt`)).toBe(tinySHA256);
    expect(await ctl('gopeed.reveal', { id })).toHaveProperty('filename');
    // Already committed publication is queried by its original task. No new
    // admission or replacement job ID may appear after retry-save.
    expect((await ctl('gopeed.publish', { id })).published).toBe(true);
    expect((await owned()).map(t => t.id)).toEqual([id]);
    expect((await ctl('tasks.delete', { id })).fileKept).toBe(true);
    await observe('toBe condition', async () => (await ctl('tasks.removal', { id })).state, value => value === 'removed');
    expect(await sha256(page, `${folder}/tiny.txt`)).toBe(tinySHA256);
  }, async step => {
    await step(() => deleteFixtureTasks(page, row => row.publication?.batch === key));
    await step(async () => { await restore(page); });
    await step(async () => { await removeFromNas(page, [folder]); });
  });
});

test('real CLI source mutations read back only the test-created source', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app);
  let id = '';
  await withCleanup(async () => {
    const source = await ctl('search.sources.save', { kind: 'torznab', name: `${tag} CLI fixture`, url: fixtureURL('/feed'), enabled: true });
    id = source.id; expect(id).toMatch(/^[0-9a-f]{32}$/);
    expect((await ctl('search.sources')).find((s: any) => s.id === id)).toMatchObject({ enabled: true });
    await ctl('search.sources.field', { id, field: 'apikey', value: 'fixture-only' });
    const config = await ctl('search.sources.config', { id });
    expect(config).toMatchObject({ id, kind: 'torznab' });
    expect(JSON.stringify(config)).not.toContain('fixture-only');
    await ctl('search.sources.patch', { id, enabled: false });
    expect((await data(page, 'search.sources.list')).find((s: any) => s.id === id).enabled).toBe(false);
    await ctl('search.sources.enabled', { id, enabled: true });
    expect((await ctl('search.sources')).find((s: any) => s.id === id).enabled).toBe(true);
    await ctl('search.sources.delete', { id });
    expect((await ctl('search.sources')).some((s: any) => s.id === id)).toBe(false); id = '';
  }, async step => {
    await step(async () => { if (id) await data(page, 'search.sources.delete', { id }); });
  });
});

test('real CLI search returns an owned result and downloads its exact fixture bytes', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), restore = await saveFolder(page);
  const scope = (await data(page, 'delegate.destination', { key: 'save' })).selection.root;
  const folder = await createFolder(page, scope, `${tag}-search-${Date.now()}`);
  let sourceId = '', jobId = '';
  const downloadIds = new Set<string>();
  await withCleanup(async () => {
    await ctl('gopeed.destination', { path: folder });
    await data(page, 'downloads.folder.set', { subfolder: '' });
    sourceId = (await ctl('search.sources.save', { kind: 'rss', name: `${tag} RSS fixture`, url: fixtureURL('/feed'), enabled: true })).id;
    const started = await ctl('search.jobs.start', { key: `search-${randomUUID()}`, sourceIds: [sourceId], query: 'Fixture', page: 1 }, ['--no-wait']);
    expect(started.job.outcome).toBe('accepted'); jobId = started.job.job.id;
    expect((await ctl('search.jobs.list')).jobs.some((j: any) => j.id === jobId)).toBe(true);
    await observe('toBe condition', async () => (await ctl('search.jobs.status', { id: jobId })).state, value => value === 'succeeded');
    expect(await ctl('search.jobs.result', { id: jobId })).toMatchObject({ kind: 'search', pages: 1 });
    const pages = await ctl('search.jobs.pages', { id: jobId, offset: 0, limit: 1 });
    expect(pages.total).toBe(1);
    expect(pages.items[0].results).toHaveLength(1);
    const resultId = pages.items[0].results[0].resultId;
    expect(resultId).toMatch(/^[0-9a-f]{32}$/);
    const waited = await ctl('search.jobs.wait', { id: jobId, browser: false });
    expect(waited.items[0].results[0].resultId).toBe(resultId);
    const admitted = await ctl('search.download', { resultId });
    expect(admitted.results[0].status).toBe('created');
    for (const id of admitted.results[0].ids) downloadIds.add(id);
    // Preparation IDs may become engine IDs; locate only this fixture folder.
    await observe('toBe condition', async () => {
      const rows = (await tasks(page)).filter(t => t.publication?.folder === folder);
      for (const row of rows) downloadIds.add(row.id);
      return rows.some(t => t.publication?.published);
    }, value => value === true);
    expect(await sha256(page, `${folder}/tiny.txt`)).toBe(tinySHA256);
    const probe = await ctl('search.sources.test.start', { key: `probe-${randomUUID()}`, id: sourceId }, ['--no-wait']);
    expect(probe.job.outcome).toBe('accepted'); jobId = probe.job.job.id;
    await observe('toBe condition', async () => (await ctl('search.jobs.status', { id: jobId })).state, value => value === 'succeeded');
    expect(await ctl('search.jobs.result', { id: jobId })).toMatchObject({ kind: 'probe', id: sourceId });
  }, async step => {
    await step(() => settleSearchJob(ctl, jobId));
    await step(() => deleteFixtureTasks(page, row => downloadIds.has(row.id) || row.publication?.folder === folder));
    await step(async () => { if (sourceId) await data(page, 'search.sources.delete', { id: sourceId }); });
    await step(async () => { await restore(page); });
    await step(async () => { await removeFromNas(page, [folder]); });
  });
});

test('real CLI cancel stops a running search on the original job ID', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app);
  let sourceId = '', jobId = '';
  let cancellation: Awaited<ReturnType<typeof prepareSearchCancellation>> | undefined;
  await withCleanup(async () => {
    sourceId = (await ctl('search.sources.save', { kind: 'rss', name: `${tag} slow RSS`, url: fixtureURL('/slow-feed'), enabled: true })).id;
    cancellation = await prepareSearchCancellation();
    const started = await data(page, 'search.jobs.start', { key: `cancel-${randomUUID()}`, sourceIds: [sourceId], query: 'Fixture', page: 1 });
    expect(started.outcome).toBe('accepted'); jobId = started.job.id;
    await observe('search is running', () => data(page, 'search.jobs.status', { id: jobId }), value => value.state === 'running', {
      check: value => { if (['succeeded', 'failed', 'canceled', 'interrupted'].includes(value.state)) throw new Error(`Search finished before cancellation: ${value.state}`); },
    });
    await cancellation.cancel(jobId);
    await observe('toBe condition', async () => (await ctl('search.jobs.status', { id: jobId })).state, value => value === 'canceled');
    expect((await ctl('search.jobs.list')).jobs.filter((j: any) => j.id === jobId)).toHaveLength(1);
  }, async step => {
    await step(async () => { await cancellation?.close(); });
    await step(() => settleSearchJob(ctl, jobId));
    await step(async () => { if (sourceId) await data(page, 'search.sources.delete', { id: sourceId }); });
  });
});

test('real CLI preferences change and restore with engine readback', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), before = await data(page, 'gopeed.settings.get');
  const input = Object.fromEntries(['maxRunning', 'connections', 'seedRatio', 'seedMinutes', 'btPort', 'ed2kPort', 'ed2kUdpPort', 'trackers'].map(k => [k, before[k]]));
  if (input.trackers === null) delete input.trackers;
  await withCleanup(async () => {
    expect((await ctl('gopeed.settings.get')).maxRunning).toBe(before.maxRunning);
    const changed = before.maxRunning === 3 ? 2 : 3;
    expect((await ctl('gopeed.settings.set', { ...input, maxRunning: changed })).maxRunning).toBe(changed);
    expect((await data(page, 'gopeed.settings.get')).maxRunning).toBe(changed);
  }, async step => {
    await step(async () => { await ctl('gopeed.settings.set', input); });
    await step(async () => { expect((await data(page, 'gopeed.settings.get')).maxRunning).toBe(before.maxRunning); });
  });
});

test('real CLI solver setting restores the exact original configuration', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), before = await data(page, 'search.solver.get');
  await withCleanup(async () => {
    expect(await ctl('search.solver.get')).toHaveProperty('configured');
    expect(await ctl('search.solver.set', { url: fixtureURL('/') })).toMatchObject({ configured: true });
    expect((await data(page, 'search.solver.get')).url).toBe(fixtureURL('/v1'));
  }, async step => {
    await step(async () => { await ctl('search.solver.set', { url: before.url ?? '' }); });
  });
});

test('real CLI uploaded torrent publishes test-created webseed bytes', { session: 'admin' }, async ({ app }) => {
  const page = await openDownloadHub(app), restore = await saveFolder(page);
  const folder = await createFolder(page, (await data(page, 'delegate.destination', { key: 'save' })).selection.root, `${tag}-torrent-${Date.now()}`);
  const key = `torrent-${randomUUID()}`;
  const encode = (value: any): Buffer => {
    if (Buffer.isBuffer(value) || typeof value === 'string') { const bytes = Buffer.from(value); return Buffer.concat([Buffer.from(`${bytes.length}:`), bytes]); }
    if (typeof value === 'number') return Buffer.from(`i${value}e`);
    return Buffer.concat([Buffer.from('d'), ...Object.keys(value).sort().flatMap(k => [encode(k), encode(value[k])]), Buffer.from('e')]);
  };
  const torrent = encode({ info: { length: tiny.length, name: 'tiny.txt', 'piece length': 16384, pieces: createHash('sha1').update(tiny).digest() }, 'url-list': fixtureURL('/tiny.txt') }).toString('base64');
  await withCleanup(async () => {
    await ctl('gopeed.destination', { path: folder }); await data(page, 'downloads.folder.set', { subfolder: '' });
    expect(await ctl('downloads.add_torrent', { key, torrent })).toMatchObject({ accepted: true, job_id: key });
    await observe('toBe condition', async () => (await tasks(page)).find(t => t.publication?.batch === key)?.publication?.published, value => value === true);
    expect(await sha256(page, `${folder}/tiny.txt`)).toBe(tinySHA256);
  }, async step => {
    await step(() => deleteFixtureTasks(page, row => row.publication?.batch === key));
    await step(async () => { await restore(page); });
    await step(async () => { await removeFromNas(page, [folder]); });
  });
});
