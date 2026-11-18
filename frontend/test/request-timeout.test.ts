import {afterEach,expect,test} from 'vitest'
import {mockHost} from './mock-host'
import {mountApp} from './mount-app'
const disposers:(()=>void)[]=[]
afterEach(()=>disposers.splice(0).forEach(fn=>fn()))
// The DSM window sets ctx.requestTimeout from backend.timeout_seconds, the
// backend's CGI deadline; the App keeps it so ctx.call waits as long.
test('the app keeps the window request deadline',()=>{
 const {ctx}=mockHost()
 ctx.requestTimeout=61_000
 mountApp(ctx,disposers)
 expect(ctx.requestTimeout).toBe(61_000)
})
