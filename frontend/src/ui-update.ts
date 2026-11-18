import type { Context } from "@synopackageland/sdk";

// DSM can keep an old module alive after its versioned chunks are removed.
export function createChunkLoader(
  ctx: Pick<Context, "uiUpdate">,
  moduleURL: string,
  reload = () => window.location.reload(),
) {
  let stopped = false;
  let checking = false;
  const base = new URL(".", moduleURL).href;
  const failed = async (error: unknown) => {
    if (stopped || !(error instanceof Error) || !error.message.includes(base) || !ctx.uiUpdate || checking) return;
    checking = true;
    try {
      if (await ctx.uiUpdate.check()) {
        if (!stopped) reload();
      }
    } catch {
      // A network failure does not establish that the installed UI changed.
    } finally {
      checking = false;
    }
  };
  return {
    async load<T>(loader: () => Promise<T>): Promise<T> {
      try {
        return await loader();
      } catch (error) {
        await failed(error);
        throw error;
      }
    },
    stop() { stopped = true; },
  };
}
