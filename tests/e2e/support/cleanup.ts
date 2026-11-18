/** One cleanup step: it runs even after an earlier step failed. */
export type Step = (run: () => Promise<unknown>) => Promise<void>;

/**
 * Runs body, then every step of cleanup. A failing body is reported as itself
 * (cleanup failures are logged), so a cleanup that fails after the body did
 * cannot hide why the test failed; otherwise the first cleanup failure is.
 */
export async function withCleanup(body: () => Promise<void>, cleanup: (step: Step) => Promise<void>): Promise<void> {
  let failed: { error: unknown } | undefined;
  try {
    await body();
  } catch (error) {
    failed = { error };
  }
  const failures: unknown[] = [];
  await cleanup(async run => { try { await run(); } catch (error) { failures.push(error); } });
  if (failed) {
    for (const failure of failures) console.error('cleanup also failed:', failure);
    throw failed.error;
  }
  if (failures.length) throw failures[0];
}
