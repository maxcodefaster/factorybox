// Test workload: private RAM must survive in the actual agent-spawned process.
const fs = require('node:fs');
const net = require('node:net');
const crypto = require('node:crypto');
const root = process.argv[2];
if (!root || !root.startsWith('/') || root.includes('..')) throw new Error('requires private absolute fixture root');
const payload = crypto.randomBytes(16 << 20);
const nonce = crypto.randomBytes(32).toString('hex');
let counter = 0;
let marker = 'armed';
let watchMarker = '';
const watched = root + '/.factorybox-watched';
fs.mkdirSync(watched, {mode: 0o700});
// Reading this marker is only triggered by the actual kernel notification.
// The control socket has no operation that changes watchMarker.
const watcher = fs.watch(watched, () => {
  try { watchMarker = fs.readFileSync(watched + '/signal', 'utf8'); }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
});
function state() {
  return {pid: process.pid, nonce, counter, marker, ram_bytes: payload.length,
    ram_sha256: crypto.createHash('sha256').update(payload).digest('hex'), watch_marker: watchMarker};
}
const socket = root + '/.factorybox-tool.sock';
const server = net.createServer(client => {
  client.setEncoding('utf8');
  let input = '';
  client.on('data', chunk => {
    input += chunk;
    if (input.length > 4096) return client.destroy(new Error('bounded tool control'));
    if (!input.includes('\n')) return;
    try {
      const request = JSON.parse(input.trim());
      if (request.operation === 'advance') {
        if (typeof request.marker !== 'string' || !/^[a-z-]{1,48}$/.test(request.marker)) throw new Error('invalid private marker');
        marker = request.marker;
        const change = crypto.createHash('sha256').update(marker).digest();
        for (let index = 0; index < change.length; index++) payload[index] ^= change[index];
        counter++;
      } else if (request.operation !== 'state') throw new Error('unknown tool control');
      client.end(JSON.stringify(state()) + '\n');
    } catch (error) { client.destroy(error); }
  });
});
server.listen(socket, () => fs.writeFileSync(root + '/.factorybox-tool-armed.json', JSON.stringify(state())));
const timer = setInterval(() => {
  if (!fs.existsSync(root + '/.factorybox-tool-release')) return;
  clearInterval(timer);
  const result = {...state(), exit: 37};
  fs.writeFileSync(root + '/.factorybox-tool-result.json', JSON.stringify(result));
  watcher.close();
  server.close(() => {
    process.stdout.write('FACTORYBOX_TOOL_RESULT ' + JSON.stringify(result) + '\n', () => process.exit(37));
  });
}, 50);
