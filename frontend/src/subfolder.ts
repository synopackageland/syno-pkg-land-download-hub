export function validSubfolder(value:string){
 if(new TextEncoder().encode(value).length>768)return false
 if(!value)return true
 const parts=value.split('/')
 return parts.length<=16&&parts.every(p=>p!==''&&p!=='.'&&p!=='..'&&new TextEncoder().encode(p).length<=255&&p.trim()===p&&!p.endsWith('.')&&!/[\\:*?"<>|\u0000-\u001f\u007f-\u009f]/.test(p)&&!['@eadir','#recycle','#snapshot'].includes(p.toLowerCase()))
}
