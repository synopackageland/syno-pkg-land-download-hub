/** The refusals of a request that reached the App while its engine starts (a package or engine restart). */
const starting = /refused: (engine_busy|engine_unavailable)$/;

/** Repeats read while the engine is still starting; any other refusal, or the timeout, reports the last one. */
export async function whileEngineStarts<T>(read: () => Promise<T>, { timeout = 120_000, interval = 2_000 } = {}): Promise<T> {
  for (const end = Date.now() + timeout; ; await new Promise(wait => setTimeout(wait, interval))) {
    try {
      return await read();
    } catch (error) {
      if (!starting.test((error as Error).message) || Date.now() > end) throw error;
    }
  }
}
