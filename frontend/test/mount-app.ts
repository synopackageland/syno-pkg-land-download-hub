import {mount} from '../src/main'
// Mounts the app in a stand-in DSM desktop: root > z-indexed window > app.
// A dialog that leaves the app (dialog-bounds="viewport") renders right after
// the window, as in DSM, so it stays inside root with the app it belongs to.
export function mountApp(ctx:Parameters<typeof mount>[1],disposers:(()=>void)[],style=''){
 const root=document.createElement('div'),window=document.createElement('div'),element=document.createElement('div')
 window.style.cssText='position:absolute;z-index:10';element.style.cssText=style
 window.append(element);root.append(window);document.body.append(root)
 const app=mount(element,ctx);disposers.push(()=>{app.unmount();root.remove()});return {root,app}
}
// Settings is a strip of tabs, one SettingsPage each: open a tab, return its page.
export async function settingsTab(root:ParentNode,value:string,settle:()=>Promise<void>){
 root.querySelector<HTMLButtonElement>(`[data-settings] [data-ui-tab-value="${value}"]`)?.click();await settle()
 return root.querySelector<HTMLElement>(`[data-settings-panel="${value}"]`)!
}
