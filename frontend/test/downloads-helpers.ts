import {nextTick} from 'vue'
// The Downloads tabs' list: one row per task, found by its name.
export async function settle(){for(let i=0;i<25;i++){await Promise.resolve();await nextTick()}}
export const rows=(root:ParentNode)=>[...root.querySelectorAll<HTMLElement>('[data-downloads-list] [data-row-key]')]
export const rowOf=(root:ParentNode,name:string)=>rows(root).find(r=>r.querySelector('.gopeed-dl-name')?.textContent?.includes(name))
export const statusOf=(row:Element)=>row.querySelector('[data-row-status]')?.textContent?.trim()
export const metaOf=(row:Element)=>row.querySelector('.gopeed-dl-meta')?.textContent?.trim()
// The rows of one group, each carrying its group as data-group.
export const groupRows=(root:ParentNode,key:string)=>rows(root).filter(r=>r.dataset.group===key)
// The Downloads tabs: their labels, one tab chosen, and its toolbar actions.
export const tabLabels=(root:ParentNode)=>[...root.querySelectorAll<HTMLElement>('[data-downloads] [data-ui-tab-value]')].map(t=>t.textContent?.trim())
export async function openTab(root:ParentNode,key:string){root.querySelector<HTMLElement>(`[data-downloads] [data-ui-tab-value="${key}"]`)!.click();await settle()}
export const tabAction=(root:ParentNode,op:string)=>root.querySelector<HTMLButtonElement>(`[data-downloads] [data-ui-section-tabs-actions] [data-group-action="${op}"]`)
export const panel=(root:ParentNode)=>root.querySelector<HTMLElement>('[data-task-panel]')
// Opens a row's task panel, as a click on the row does.
export async function openPanel(root:ParentNode,name:string){rowOf(root,name)!.click();await settle();return panel(root)!}
export const buttonNamed=(root:ParentNode,name:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===name||b.getAttribute('aria-label')===name)
export async function confirmDialog(){[...document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')].find(b=>b.textContent?.trim()==='Confirm')!.click();await settle()}
