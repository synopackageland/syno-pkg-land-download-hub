import { createApp } from "vue";
import App from "./App.vue";
import "@synopackageland/ui/themes/dsm7-embedded.css";
import "@synopackageland/sdk/vue/style.css";
import "./style.css";
import type { Context } from "@synopackageland/sdk";
export function mount(root: HTMLElement, ctx: Context) {
  const id = crypto.getRandomValues(new Uint32Array(4)).join("-");
  const app = createApp(App, { ctx });
  app.config.idPrefix = "gopeed-" + id;
  root.classList.add("gopeed-root");
  const instance = app.mount(root) as any;
  const host = window as any;
  host.SynoGopeedBridge = host.SynoGopeedBridge || Object.create(null);
  host.SynoGopeedBridge[id] = { pickFolder: () => instance.pick() };
  return {
    unmount() {
      app.unmount();
      root.classList.remove("gopeed-root");
      delete host.SynoGopeedBridge[id];
    },
  };
}
