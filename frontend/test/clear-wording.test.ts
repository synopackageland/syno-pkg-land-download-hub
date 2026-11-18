import {expect,test} from 'vitest'
import {readFileSync} from 'node:fs'
import {resolve} from 'node:path'

// Records are cleared, never removed or deleted: the saved file stays.
test('every record text says clear, never remove or delete',()=>{
 const keys=['ui.action.clearRecord','ui.confirm.clearRecord','ui.confirm.clearRecordKeeps','ui.action.clearCompleted','ui.action.clearFailed','ui.confirm.clearCompleted','ui.confirm.clearCompletedKeeps','ui.confirm.clearFailed','ui.confirm.clearFailedKeeps','ui.notice.cleared','delete.fileKept','delete.cancelingHelp','error.task_deleting','download.incomplete','save.failedHelp','save.noRetryStep','error.queue_limit']
 const words:Record<string,RegExp>={en:/\b(remov|delet)/i,'zh-TW':/移除|刪除/,'zh-CN':/移除|删除/}
 for(const locale of ['en','zh-TW','zh-CN','ja','ko','de','fr','es','it']){
  const catalog=JSON.parse(readFileSync(resolve(__dirname,`../../i18n/messages/${locale}.json`),'utf8'))
  for(const key of keys){
   const text=catalog['gopeed.'+key]
   expect(text,`${locale} ${key}`).toBeTruthy()
   // The confirmations may name File Station's own Delete as the way to delete the file.
   if(words[locale]&&!key.endsWith('Keeps'))expect(text,`${locale} ${key}`).not.toMatch(words[locale])
  }
  for(const old of ['ui.action.removeRecord','ui.confirm.removeRecord','ui.confirm.removeRecordKeeps'])expect(catalog['gopeed.'+old],`${locale} ${old}`).toBeUndefined()
 }
})
