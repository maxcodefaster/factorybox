import 'reflect-metadata';
import '@theia/core/lib/common/message-rpc/index.js';
import { io } from 'socket.io-client';
import { WebSocketChannel } from '@theia/core/lib/common/messaging/web-socket-channel.js';
import { ChannelMultiplexer } from '@theia/core/lib/common/message-rpc/channel.js';
import { RpcProxyFactory } from '@theia/core/lib/common/messaging/proxy-factory.js';
import { createServer } from 'node:http';
import { randomUUID } from 'node:crypto';
import { writeFileSync } from 'node:fs';

// This verification client runs with the IDE, outside captured cgroups. It
// keeps one native RPC connection throughout capture, both restores and fork.
async function main() {
const base = process.argv[2];
const port = Number(process.argv[3]);
if (base !== 'http://127.0.0.1:4400' || port !== 4401) throw new Error('Expected owned local IDE endpoints');
const versions = { theia: __THEIA_VERSION__, socketIoClient: __SOCKET_IO_VERSION__ };
const clientID = randomUUID();
const bounded = (promise, ms = 10000) => new Promise((done, reject) => {
  const timer = setTimeout(() => reject(new Error('Theia RPC timed out')), ms);
  promise.then(value => { clearTimeout(timer); done(value); }, error => { clearTimeout(timer); reject(error); });
});
let response;
const deadline = Date.now() + 30000;
while (!response) {
  try {
    const candidate = await fetch(base, { signal: AbortSignal.timeout(1000) });
    if (!candidate.ok || !(await candidate.text()).includes('<html')) throw new Error('Theia frontend unavailable');
    response = candidate;
  } catch (error) {
    if (Date.now() >= deadline) throw error;
    await new Promise(done => setTimeout(done, 100));
  }
}
const cookie = response.headers.getSetCookie().map(value => value.split(';')[0]).join('; ');
const socket = io(`${base}/services`, { transports: ['websocket'], reconnection: false, autoConnect: false, extraHeaders: { cookie, origin: base } });
const connection = new WebSocketChannel(socket);
const multiplexer = new ChannelMultiplexer(connection);
await bounded(new Promise((done, reject) => {
  socket.once('connect_error', reject);
  socket.once('initialConnection', done);
  socket.once('connect', () => socket.emit('initialConnection', clientID));
  socket.connect();
}));
const channel = await bounded(multiplexer.open('/services/remote-filesystem'));
const factory = new RpcProxyFactory({ onDidChangeCapabilities() {}, onFileChange() {}, onFileStreamData() {}, onFileStreamEnd() {} });
factory.listen(channel);
const files = factory.createProxy();
const roots = {
  'source-sysbox': '/experiment/sysbox/upper/workspace/fixture',
  'copy-sysbox': '/experiment/sysbox-copy/upper/workspace/fixture',
};
const branchRoot = id => Object.hasOwn(roots, id) ? roots[id]
  : typeof id === 'string' && /^branch-[0-9a-f]{32}$/.test(id)
    ? `/experiment/branches/${id}/upper/workspace/fixture` : null;
let busy = false;
const server = createServer(async (request, reply) => {
  const start = process.hrtime.bigint();
  let claimed = false;
  try {
    if (request.method !== 'POST' || !['/ready', '/diverge'].includes(request.url)) throw new Error('Unknown verification operation');
    if (!socket.connected || busy) throw new Error('Native IDE connection unavailable or busy');
    busy = true;
    claimed = true;
    let body = '';
    for await (const chunk of request) {
      body += chunk;
      if (body.length > 16384) throw new Error('Oversized verification request');
    }
    const input = JSON.parse(body);
    if (!Array.isArray(input.branches) || input.branches.length < 1 || input.branches.length > 2 || new Set(input.branches).size !== input.branches.length || input.branches.some(id => !branchRoot(id))) throw new Error('Unknown selected branch');
    const values = {};
    const markers = [];
    for (const [index, id] of input.branches.entries()) {
      const markerPath = `file://${branchRoot(id)}/marker.txt`;
      const marker = Buffer.from(await bounded(files.readFile(markerPath))).toString();
      const expected = request.url === '/diverge' ? `devcontainer-branch-${index}` : input.expected_marker;
      if (typeof expected !== 'string' || marker !== expected) throw new Error(`IDE read wrong saved branch ${id}: ${marker}`);
      if (request.url === '/ready') {
        if (typeof input.phase !== 'string' || input.phase.length > 64 || !input.phase) throw new Error('Missing readiness phase');
        const value = `${input.phase}:${id}`;
        const path = `file://${branchRoot(id)}/theia-readiness.txt`;
        await bounded(files.writeFile(path, new Uint8Array(Buffer.from(value)), { create: true, overwrite: true }));
        const saved = Buffer.from(await bounded(files.readFile(path))).toString();
        if (saved !== value) throw new Error(`IDE write did not reach selected branch ${id}`);
        values[id] = { marker, probe: saved };
      } else {
        const value = `theia-branch-${index}-${randomUUID()}`;
        await bounded(files.writeFile(markerPath, new Uint8Array(Buffer.from(value)), { create: false, overwrite: true }));
        markers.push(value);
      }
    }
    if (request.url === '/diverge') {
      for (const [index, id] of input.branches.entries()) {
        const value = Buffer.from(await bounded(files.readFile(`file://${branchRoot(id)}/marker.txt`))).toString();
        if (value !== markers[index]) throw new Error(`IDE edits crossed selected branches: ${id}`);
      }
    }
    reply.writeHead(200, { 'content-type': 'application/json' });
    reply.end(JSON.stringify({ ok: true, client_id: clientID, pid: process.pid, versions, values, markers,
      rpc_duration_ms: Number(process.hrtime.bigint() - start) / 1e6,
      transport: 'native Theia remote-filesystem RPC', frontend: 'HTTP shell only; browser editing not asserted' }));
  } catch (error) {
    reply.writeHead(503, { 'content-type': 'application/json' });
    reply.end(JSON.stringify({ ok: false, client_id: clientID, pid: process.pid, error: String(error) }));
  } finally {
    if (claimed) busy = false;
  }
});
server.listen(port, '127.0.0.1', () => {
  writeFileSync('/experiment/theia/client.pid', `${process.pid}\n`, { mode: 0o600, flag: 'wx' });
  writeFileSync('/experiment/theia/client-ready', `${clientID}\n`, { mode: 0o600, flag: 'wx' });
  console.log(JSON.stringify({ event: 'native-ide-client-ready', client_id: clientID, pid: process.pid, versions }));
});
process.on('SIGTERM', () => { socket.disconnect(); server.close(() => process.exit()); });
}
main().catch(error => { console.error(error); process.exit(1); });
