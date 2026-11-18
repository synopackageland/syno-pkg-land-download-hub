// Test-created bytes only. Expose this server using an ephemeral HTTPS
// tunnel; the App still performs its production public-address checks.
// E2E_FIXTURE_URL is the public tunnel URL, never a product allowlist override.
import { createServer } from 'node:http';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';

export const tiny = Buffer.from('Download Hub acceptance fixture\n');
export const tinySHA256 = createHash('sha256').update(tiny).digest('hex');
const slowSize = 4 * 1024 * 1024;

export function fixtureServer() {
  return createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://fixture.invalid');
    response.setHeader('Cache-Control', 'no-store');
    if (url.pathname === '/feed' || url.pathname === '/slow-feed') {
      const send = () => {
        const base = process.env.E2E_FIXTURE_URL;
        if (!base) { response.writeHead(503); response.end(); return; }
        response.setHeader('Content-Type', 'application/rss+xml');
        response.end(`<?xml version="1.0"?><rss version="2.0"><channel><title>Fixture</title><link>${base}</link><description>Owned fixture</description><item><title>Fixture release</title><guid>fixture-release</guid><enclosure url="${new URL('/tiny.txt', base)}" type="application/octet-stream" length="${tiny.length}"/><link>${new URL('/tiny.txt', base)}</link></item></channel></rss>`);
      };
      if (url.pathname === '/slow-feed') {
        response.setHeader('Content-Type', 'application/rss+xml');
        response.flushHeaders(); // Stay in body transfer, beyond the helper's header deadline.
        const timer = setTimeout(send, 24_000);
        response.on('close', () => clearTimeout(timer));
      } else send();
      return;
    }
    let bytes: Buffer;
    if (url.pathname === '/tiny.txt') bytes = tiny;
    else if (url.pathname === '/slow.bin') bytes = Buffer.alloc(slowSize, 0x5a);
    else if (url.pathname === '/clip.mp4' && process.env.E2E_FIXTURE_VIDEO) bytes = readFileSync(process.env.E2E_FIXTURE_VIDEO);
    else { response.writeHead(404); response.end(); return; }
    const range = /^bytes=(\d+)-(\d*)$/.exec(request.headers.range ?? '');
    const start = range ? Number(range[1]) : 0;
    const end = range?.[2] ? Math.min(Number(range[2]), bytes.length - 1) : bytes.length - 1;
    if (start > end || start >= bytes.length) { response.writeHead(416); response.end(); return; }
    response.statusCode = range ? 206 : 200;
    response.setHeader('Accept-Ranges', 'bytes');
    response.setHeader('Content-Length', end - start + 1);
    response.setHeader('Content-Type', url.pathname.endsWith('.mp4') ? 'video/mp4' : 'application/octet-stream');
    if (range) response.setHeader('Content-Range', `bytes ${start}-${end}/${bytes.length}`);
    if (request.method === 'HEAD') { response.end(); return; }
    if (url.pathname !== '/slow.bin') { response.end(bytes.subarray(start, end + 1)); return; }
    let offset = start;
    // Keep the long download in flight regardless of CLI latency. Short range
    // probes finish; real transfer requests stall halfway through their range.
    // Pause/resume starts a new Range at the retained byte position.
    const ceiling = end - start < 8192 ? end + 1 : start + Math.floor((end - start + 1) / 2);
    const timer = setInterval(() => {
      const next = Math.min(offset + 4096, ceiling);
      response.write(bytes.subarray(offset, next)); offset = next;
      if (offset > end) { clearInterval(timer); response.end(); }
      else if (offset >= ceiling) clearInterval(timer);
    }, 1000);
    response.on('close', () => clearInterval(timer));
  });
}

if (process.argv[1] === import.meta.filename) {
  const server = fixtureServer();
  server.listen(Number(process.env.E2E_FIXTURE_PORT ?? 18743), '127.0.0.1');
  for (const signal of ['SIGINT', 'SIGTERM'] as const) process.on(signal, () => server.close(() => process.exit()));
}
