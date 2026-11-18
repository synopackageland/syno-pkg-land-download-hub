import type { Context, HostRequestResult } from "@synopackageland/sdk";
// The saved file in its folder: File Station opens the folder with it selected.
export function savedFile(destination: { folder: string; filename: string }) {
  return `${destination.folder.replace(/\/+$/, "")}/${destination.filename}`;
}
// File Station always rechecks DSM permissions. Paths are navigation data.
export function revealSaved(
  ctx: Context,
  id: string,
  file?: string,
  popup?: Window | null,
): HostRequestResult {
  const platform = ctx.host.platform();
  if (platform.host === "dsm" && platform.dsmStandalone !== true) {
    return file ? ctx.host.revealFile(file) : { status: "unsupported" };
  }
  const desktop = new URL("/", window.location.href);
  desktop.searchParams.set("launchApp", "SYNO.SDS.SynoPkgGopeed.Instance");
  desktop.searchParams.set("gopeedReveal", id);
  const w = popup === undefined ? window.open("about:blank", "_blank") : popup;
  if (!w) return { status: "blocked" };
  w.opener = null;
  w.location.href = desktop.href;
  return { status: "requested" };
}
