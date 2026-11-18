export type MediaOptions={format:string;subtitleLanguages?:string[];subtitles?:string;audioFormat?:string;embedThumbnail?:boolean;embedMetadata?:boolean;splitChapters?:boolean;sponsorBlock?:string;nfo?:boolean}
export const defaultMediaOptions=():MediaOptions=>({format:'best',subtitles:'off',subtitleLanguages:['en'],audioFormat:'',sponsorBlock:'off',embedThumbnail:false,embedMetadata:false,splitChapters:false,nfo:false})
export type MediaRule={id:string;host?:string;channel?:string;path:string;options:MediaOptions}
export type MediaFollow={id:string;url:string;path:string;intervalHours:number;nextCheck:string;lastCheck?:string;error?:string}
export type MediaPreferences={defaults:MediaOptions;rules:MediaRule[];follows:MediaFollow[]}
export type CookieStatus={domains:{domain:string;count:number}[];importedAt?:string}
