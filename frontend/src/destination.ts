import {appCopy} from './app-copy'
import type {Context} from '@synopackageland/sdk'
import {createNasProvider,createBackgroundSaveFacade,type DestinationState,type SelectDestination,type DestinationGrant} from '@synopackageland/sdk'

export type DownloadLocation=DestinationState&{subfolder?:string;reason?:string}
export const ensureDestinationKey=Symbol("ensure-download-destination")
export type EnsureDestination=()=>Promise<string|undefined>

// All destination/scope authority remains in the SDK, including the install
// wizard and PasswordConfirm settings form.
export function createDestinationAdapter(ctx:Context){
 const sdk=createBackgroundSaveFacade(ctx)
 const provider=createNasProvider(ctx)
 const selectDestination=(input:SelectDestination)=>ctx.call<DestinationGrant>('gopeed.destination',{key:input.key,path:input.path})
 return {provider,setupContext:ctx,sdk:{...sdk,selectDestination,destination:(_key:string)=>ctx.call<DownloadLocation>('downloads.location',{})},selectDestination}
}
const copyKeys=['loading','error','active','missing','disabled','reselection-required','revoked','needs-scope-confirmation','appDenied'] as const
export function destinationCopy(locale:string){return Object.fromEntries(copyKeys.map(key=>[key,appCopy(locale,'destination.'+key)])) as Record<typeof copyKeys[number],string>}
// Where saves go now, or nothing while the destination cannot take them.
export function activePath(state:DownloadLocation|null|undefined){if(state?.status!=='active'||!state.destination)return undefined;return state.destination.path+(state.subfolder?'/'+state.subfolder:'')}
