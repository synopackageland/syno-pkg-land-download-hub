import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp,settingsTab} from './mount-app'
import {mockHost} from './mock-host'
import {GITHUB_TOKEN_URL,githubFailure} from '../src/github'
import en from '../../i18n/messages/en.json'
const clean:(()=>void)[]=[]
afterEach(()=>clean.splice(0).forEach(f=>f()))
async function settle(){for(let i=0;i<20;i++){await Promise.resolve();await nextTick()}}
// Settings loads the search section on demand.
async function settleImports(){await vi.dynamicImportSettled();await settle()}
const buttons=(root:ParentNode,label:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].filter(b=>b.textContent?.trim()===label)
const token='github_pat_'+'A'.repeat(36)+'wxyz'
async function mount(admin=true,answers:Record<string,(p:any)=>any>={}){
 const host=mockHost(admin),original=host.ctx.call
 host.ctx.call=async(method:string,p:any={})=>{if(answers[method]){host.calls.push({method,params:p});return answers[method](p)}return original(method,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 return {host,root}
}
async function settings(admin=true){
 const m=await mount(admin)
 buttons(m.root,'Settings')[0].click();await settle()
 await settingsTab(m.root,'github',settle)
 return {...m,section:m.root.querySelector<HTMLElement>('[data-section="github"]')}
}
async function type(input:HTMLInputElement,value:string){input.value=value;input.dispatchEvent(new Event('input'));await settle()}

test('the token link opens GitHub’s documented fine-grained form, prefilled, with no permissions',()=>{
 const u=new URL(GITHUB_TOKEN_URL)
 expect(u.origin+u.pathname).toBe('https://github.com/settings/personal-access-tokens/new')
 expect(u.searchParams.get('name')).toBe('Download Hub on Synology')
 expect(u.searchParams.get('name')!.length).toBeLessThanOrEqual(40)
 expect(u.searchParams.get('expires_in')).toBe('365')
 expect([...u.searchParams.keys()].sort()).toEqual(['description','expires_in','name'])
})

test('an administrator saves a token once and then sees only its last four characters',async()=>{
 const {host,section}=await settings()
 expect(section!.textContent).toContain('GitHub token (optional)')
 expect(section!.textContent).toContain('Raises the GitHub limit for update checks and downloads')
 const link=section!.querySelector<HTMLAnchorElement>('a[href^="https://github.com/settings/personal-access-tokens/new"]')!
 expect(link.textContent).toContain('Create a token on GitHub')
 const input=section!.querySelector<HTMLInputElement>('input[data-github-token]')!
 expect(input.type).toBe('password')
 await type(input,token)
 section!.querySelector('form')!.dispatchEvent(new Event('submit'));await settle()
 expect(host.calls.filter(c=>c.method==='gopeed.github.set')).toEqual([{method:'gopeed.github.set',params:{token}}])
 expect(section!.querySelector('[data-github-saved]')!.textContent).toBe('Saved · …wxyz')
 expect(section!.textContent).toContain('GitHub limit: 5,000 requests per hour')
 expect(section!.querySelector('input[data-github-token]')).toBeNull()
 expect(section!.innerHTML).not.toContain(token)
 buttons(section!,'Remove')[0].click();await settle()
 expect(host.calls.some(c=>c.method==='gopeed.github.remove')).toBe(true)
 expect(section!.querySelector('input[data-github-token]')).not.toBeNull()
})

test('a rejected token says so and keeps the field',async()=>{
 const {host,root}=await mount(true,{'gopeed.github.set':()=>{throw {code:'github_token_invalid'}}})
 buttons(root,'Settings')[0].click();await settle()
 await settingsTab(root,'github',settle)
 const section=root.querySelector<HTMLElement>('[data-section="github"]')!
 await type(section.querySelector<HTMLInputElement>('input[data-github-token]')!,token)
 section.querySelector('form')!.dispatchEvent(new Event('submit'));await settle()
 expect(section.textContent).toContain('The GitHub token is invalid or expired')
 expect(host.calls.some(c=>c.method==='gopeed.github.set')).toBe(true)
})

test('ordinary users never see the token setting',async()=>{
 const {host,section}=await settings(false)
 expect(section).toBeNull()
 expect(host.calls.some(c=>c.method.startsWith('gopeed.github.'))).toBe(false)
})

test('a GitHub limit on a core check names the reset and opens Settings at the token field',async()=>{
 const reset='2026-10-14T02:03:02Z',d=new Date(reset),hhmm=`${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`
 const {root}=await mount(true,{'binaries.check':()=>{throw {code:'tool_source_rate_limited',message:`GitHub rate limit reached; it resets at ${reset}. Add a GitHub token in Settings > GitHub to raise the limit`}}})
 buttons(root,'Core updates')[0].click();await settle()
 const core=root.querySelector<HTMLElement>('[data-core]')!
 buttons(core,'Check for updates')[0].click();await settle()
 expect(core.textContent).toContain(`GitHub rate limit reached. It resets at ${hhmm}`)
 buttons(core,'Add a GitHub token')[0].click();await settle()
 const field=root.querySelector<HTMLInputElement>('[data-section="github"] input[data-github-token]')
 expect(field).not.toBeNull()
 expect(document.activeElement).toBe(field)
})

test('a tool download the daemon refused for a limit shows the same step',async()=>{
 const tools={state:{tools:{'yt-dlp':{}},tool:'yt-dlp',error:'tool_source_rate_limited',resetAt:'2026-10-14T02:03:02Z'},pending:false,arch:'amd64'}
 const {root}=await mount(true,{'media.tools.status':()=>tools})
 buttons(root,'Core updates')[0].click();await settle()
 const tool=root.querySelector<HTMLElement>('[data-media-tool="yt-dlp"]')!
 expect(tool.textContent).toContain('GitHub rate limit reached. It resets at')
 expect(tool.textContent).not.toContain('tool_source_rate_limited')
 expect(buttons(tool,'Add a GitHub token')).toHaveLength(1)
})

test('a definitions update refused for a limit or a bad token offers the token step',async()=>{
 for(const [error,text,action] of [[{code:'search_update_rate_limited',message:'GitHub rate limit reached; it resets at 2100-01-01T00:00:00Z.'},'GitHub rate limit reached. It resets at','Add a GitHub token'],[{code:'github_token_invalid'},'The GitHub token is invalid or expired','Replace the GitHub token']] as const){
  const {root}=await mount(true,{'search.definitions.update':()=>{throw error}})
  buttons(root,'Settings')[0].click();await settleImports()
  await settingsTab(root,'search',settleImports)
  const sources=root.querySelector<HTMLElement>('[data-section="search-sources"]')!
  buttons(sources,'Update site list')[0].click();await settle()
  expect(sources.textContent).toContain(text)
  expect(buttons(sources,action)).toHaveLength(1)
  clean.splice(0).forEach(f=>f())
 }
})

test('the backend codes map to a GitHub failure and every locale has the copy',()=>{
 expect(githubFailure({code:'search_update_rate_limited',message:'x 2100-01-01T00:00:00Z y'})).toEqual({kind:'limited',reset:'2100-01-01T00:00:00Z'})
 expect(githubFailure({code:'tool_source_rate_limited'})).toEqual({kind:'limited'})
 expect(githubFailure({code:'github_token_invalid'})).toEqual({kind:'invalid'})
 expect(githubFailure({code:'tool_source_unavailable'})).toBeUndefined()
 const keys=Object.keys(en).filter(k=>k.startsWith('gopeed.github.'))
 expect(keys.length).toBeGreaterThan(10)
 for(const value of keys.map(k=>(en as Record<string,string>)[k]))expect(value.endsWith('.')).toBe(false)
})
