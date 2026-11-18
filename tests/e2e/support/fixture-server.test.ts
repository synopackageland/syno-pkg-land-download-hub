import { test } from 'node:test';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import { fixtureServer, tiny } from './fixture-server.ts';

test('fixture range requests reconstruct the exact original download', async () => {
  const server = fixtureServer();
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const url = `http://127.0.0.1:${address.port}/tiny.txt`;
  try {
    const head = await fetch(url, { method: 'HEAD' });
    assert.equal(Number(head.headers.get('content-length')), tiny.length);
    assert.equal(head.headers.get('accept-ranges'), 'bytes');
    const first = await fetch(url, { headers: { Range: 'bytes=0-7' } });
    assert.equal(first.status, 206);
    assert.equal(first.headers.get('content-range'), `bytes 0-7/${tiny.length}`);
    const rest = await fetch(url, { headers: { Range: 'bytes=8-' } });
    assert.equal(rest.status, 206);
    assert.deepEqual(Buffer.concat([Buffer.from(await first.arrayBuffer()), Buffer.from(await rest.arrayBuffer())]), tiny);
    assert.equal((await fetch(url, { headers: { Range: `bytes=${tiny.length}-` } })).status, 416);
    assert.equal((await fetch(new URL('/not-a-fixture', url))).status, 404);
  } finally { server.closeAllConnections(); await new Promise<void>(done => server.close(() => done())); }
});
