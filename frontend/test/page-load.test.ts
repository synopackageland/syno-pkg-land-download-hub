import {afterEach,expect,test,vi} from 'vitest'
import {withDeadline,pageLeft} from '../src/page-load'
afterEach(()=>{vi.useRealTimers()})
test('an answer within the deadline passes through',async()=>{
 await expect(withDeadline(Promise.resolve(7),new AbortController().signal,50)).resolves.toBe(7)
})
test('a read that outlives its deadline fails as a timeout',async()=>{
 vi.useFakeTimers()
 const result=withDeadline(new Promise(()=>{}),new AbortController().signal,50)
 vi.advanceTimersByTime(50)
 await expect(result).rejects.toEqual({code:'request_timeout'})
})
test('leaving the page settles the read and drops a late answer',async()=>{
 const page=new AbortController();let answer!:(v:number)=>void
 const result=withDeadline(new Promise<number>(r=>answer=r),page.signal,50)
 page.abort();answer(1)
 await expect(result).rejects.toBe(pageLeft)
 await expect(withDeadline(Promise.resolve(1),page.signal)).rejects.toBe(pageLeft)
})
