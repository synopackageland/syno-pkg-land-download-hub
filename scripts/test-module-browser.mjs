// Exercise the built module with a local host fixture, without a DSM session.
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve, join, extname } from "node:path";
import { createServer } from "../frontend/node_modules/vite/dist/node/index.js";
import { chromium } from "playwright";

const ui = resolve("ui");
const out = resolve(process.env.BROWSER_ARTIFACTS ?? ".cache/module-browser");
await mkdir(out, { recursive: true });
const html = `<!doctype html><html><head><link rel="stylesheet" href="/bundle/app.css">
<style>html,body,#app{height:100%;margin:0}</style></head><body><div id="app"></div>
<script type="module">
import {mount} from '/bundle/app.module.js';
import {mockHost} from '/test/mock-host.ts';
const fixture=mockHost();
fixture.ctx.uiUpdate={check:async()=>{
 sessionStorage.setItem('updateChecks',String(Number(sessionStorage.getItem('updateChecks')||0)+1));
 return new URL(location.href).searchParams.has('stale');
}};
window.fixture=fixture;window.mounted=mount(document.getElementById('app'),fixture.ctx);
</script></body></html>`;
const server = await createServer({
  root: resolve("frontend"), configFile: resolve("frontend/vite.config.ts"),
  server: { host: "127.0.0.1", port: 18765, strictPort: true },
  plugins: [{
    name: "built-module-fixture",
    configureServer(server) {
      server.middlewares.use(async (request, response, next) => {
        const path = new URL(request.url, "http://fixture.example").pathname;
        if (path === "/module-fixture") {
          response.setHeader("Content-Type", "text/html");
          response.end(html);
        } else if (path.startsWith("/bundle/")) {
          const file = resolve(ui, path.slice("/bundle/".length));
          if (!file.startsWith(ui + "/")) { response.statusCode = 404; response.end(); return; }
          try {
            const bytes = await readFile(file);
            response.setHeader("Content-Type", extname(file) === ".css" ? "text/css" : "text/javascript");
            response.end(bytes);
          } catch { response.statusCode = 404; response.end(); }
        } else next();
      });
    },
  }],
});
await server.listen();
const browser = await chromium.launch({
  executablePath: process.env.CHROME ?? "/usr/bin/chromium",
  args: ["--no-sandbox", "--disable-dev-shm-usage"],
});
try {
  const page = await browser.newPage({ viewport: { width: 700, height: 800 } });
  const requests = [], errors = [];
  page.on("request", request => requests.push(request.url()));
  page.on("pageerror", error => errors.push(String(error)));
  await page.goto("http://127.0.0.1:18765/module-fixture");
  await page.getByRole("button", { name: "Search", exact: true }).first().waitFor();
  assert.equal(requests.some(url => /\/(?:SearchPage|SourceSettings)-/.test(url)), false);
  await page.screenshot({ path: join(out, "downloads-700.png") });
  await page.getByRole("button", { name: "Search", exact: true }).first().click();
  await page.locator("[data-search-query] input").waitFor();
  assert.equal(requests.filter(url => /\/SearchPage-/.test(url)).length, 1);
  await page.screenshot({ path: join(out, "search-700.png") });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("button", { name: "Settings", exact: true }).first().click();
  await page.locator('[data-section="search-sources"]').waitFor();
  assert.equal(requests.filter(url => /\/SourceSettings-/.test(url)).length, 1);
  await page.screenshot({ path: join(out, "settings-1440.png") });
  assert.deepEqual(errors, []);
  let missing = 0;
  await page.route("**/bundle/chunks/SearchPage-*.js", route => {
    missing++;
    return route.fulfill({ status: 404, contentType: "text/javascript", body: "" });
  });
  await page.goto("http://127.0.0.1:18765/module-fixture?stale");
  const reload = page.waitForEvent("framenavigated", frame => frame === page.mainFrame());
  await page.getByRole("button", { name: "Search", exact: true }).first().click();
  await reload;
  await page.getByRole("button", { name: "Search", exact: true }).first().waitFor();
  assert.equal(missing, 1);
  assert.ok(Number(await page.evaluate(() => sessionStorage.getItem("updateChecks"))) >= 1);
  const evidence = { ok: true, viewports: [700, 1440], lazyPages: ["SearchPage", "SourceSettings"], removedChunkStatus: 404, reloads: 1 };
  await writeFile(join(out, "browser.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log(JSON.stringify(evidence));
} finally {
  await browser.close();
  await server.close();
}
