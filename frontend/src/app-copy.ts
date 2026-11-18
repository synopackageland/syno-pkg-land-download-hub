import en from '../../i18n/messages/en.json'
import zhTW from '../../i18n/messages/zh-TW.json'
import zhCN from '../../i18n/messages/zh-CN.json'
import ja from '../../i18n/messages/ja.json'
import ko from '../../i18n/messages/ko.json'
import de from '../../i18n/messages/de.json'
import fr from '../../i18n/messages/fr.json'
import it from '../../i18n/messages/it.json'
import es from '../../i18n/messages/es.json'

const catalogs:Record<string,Record<string,string>>={en,'zh-TW':zhTW,'zh-CN':zhCN,ja,ko,de,fr,it,es}
export function appCopy(locale:string,key:string){
 const lang=locale.replace('_','-').toLowerCase()
 const exact=Object.keys(catalogs).find(k=>k.toLowerCase()===lang)
 const fallback=lang.startsWith('zh')?'zh-TW':lang.split('-')[0]
 return (catalogs[exact??fallback]??en)['gopeed.'+key]??(en as Record<string,string>)['gopeed.'+key]??''
}
