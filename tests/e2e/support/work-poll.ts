// Bound completed observations, not elapsed wall time. In particular, never
// abort a real CLI round trip with an expect.poll deadline.
export async function observe<T>(label: string, read: () => Promise<T>, done: (value: T) => boolean,
  options: { maxReads?: number; intervalMs?: number; check?: (value: T) => void } = {}): Promise<T> {
  const maxReads = options.maxReads ?? 60;
  if (!Number.isSafeInteger(maxReads) || maxReads < 1) throw new Error('maxReads must be positive');
  for (let count = 0; count < maxReads; count++) {
    const value = await read();
    options.check?.(value);
    if (done(value)) return value;
    if (count + 1 < maxReads && (options.intervalMs ?? 500) > 0) await new Promise(resolve => setTimeout(resolve, options.intervalMs ?? 500));
  }
  throw new Error(`${label}: observation work limit reached after ${maxReads} completed reads`);
}

export const terminalSearch = (state: string) => ['succeeded', 'failed', 'canceled', 'interrupted'].includes(state);

export async function settleSearchJob(invoke: (operation: string, input: unknown) => Promise<any>, id: string): Promise<void> {
  if (!id) return;
  const status = await invoke('search.jobs.status', { id });
  if (terminalSearch(status.state)) return;
  await invoke('search.jobs.cancel', { id });
  await observe('search cancellation', () => invoke('search.jobs.status', { id }), value => terminalSearch(value.state));
}
