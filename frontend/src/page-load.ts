import {clientTimeout} from '@synopackageland/sdk'

// A page's reads end with the page; use the SDK's shared request allowance
// and drop an answer that outlives the page.
export const pageLoadTimeout = clientTimeout()
export const pageLeft = {code: 'page_left'}
export function withDeadline<T>(work: Promise<T>, signal: AbortSignal, ms = pageLoadTimeout): Promise<T> {
  if (signal.aborted) return Promise.reject(pageLeft)
  return new Promise<T>((resolve, reject) => {
    const leave = () => finish(() => reject(pageLeft))
    const timer = setTimeout(() => finish(() => reject({code: 'request_timeout'})), ms)
    function finish(settle: () => void) {
      clearTimeout(timer)
      signal.removeEventListener('abort', leave)
      settle()
    }
    signal.addEventListener('abort', leave)
    work.then(value => finish(() => signal.aborted ? reject(pageLeft) : resolve(value)), e => finish(() => reject(signal.aborted ? pageLeft : e)))
  })
}
