import { expect, test, vi } from "vitest";
import { createChunkLoader } from "../src/ui-update";

const moduleURL = "https://app.example/app/version/app.module.js";
function failure(url = "https://app.example/app/version/chunks/search.js") {
  return () => Promise.reject(new TypeError("Failed to fetch dynamically imported module: " + url));
}

test("a removed chunk reloads only after confirming an installed update", async () => {
  const reload = vi.fn(), check = vi.fn().mockResolvedValue(false);
  const chunks = createChunkLoader({ uiUpdate: { check } }, moduleURL, reload);
  try {
    await expect(chunks.load(failure())).rejects.toThrow("Failed to fetch");
    expect(reload).not.toHaveBeenCalled();
    check.mockResolvedValue(true);
    await expect(chunks.load(failure("https://other.example/chunk.js"))).rejects.toThrow();
    expect(check).toHaveBeenCalledTimes(1);
    await expect(chunks.load(failure())).rejects.toThrow();
    expect(reload).toHaveBeenCalledTimes(1);
  } finally { chunks.stop(); }
});

test("unmount cancels a pending reload and further update checks", async () => {
  let resolve!: (updated: boolean) => void;
  const check = vi.fn(() => new Promise<boolean>(done => { resolve = done; }));
  const reload = vi.fn(), chunks = createChunkLoader({ uiUpdate: { check } }, moduleURL, reload);
  const pending = chunks.load(failure());
  await Promise.resolve();
  chunks.stop();
  resolve(true);
  await expect(pending).rejects.toThrow();
  await expect(chunks.load(failure())).rejects.toThrow();
  expect(check).toHaveBeenCalledTimes(1);
  expect(reload).not.toHaveBeenCalled();
});

test("successful lazy loads return their module and ordinary errors do not reload", async () => {
  const check = vi.fn(), reload = vi.fn();
  const chunks = createChunkLoader({ uiUpdate: { check } }, moduleURL, reload);
  const module = { default: {} };
  expect(await chunks.load(async () => module)).toBe(module);
  await expect(chunks.load(async () => { throw new Error("page failed"); })).rejects.toThrow("page failed");
  expect(check).not.toHaveBeenCalled();
  expect(reload).not.toHaveBeenCalled();
  chunks.stop();
});
