import {appCopy} from './app-copy'
export function saveReasonMessage(reason:unknown,locale='en'){
 if(typeof reason!=='string'||!/^[A-Za-z][A-Za-z0-9_-]{0,127}$/.test(reason))return ''
 return appCopy(locale,'reason.'+reason)||appCopy(locale,'reason.unknown')
}
