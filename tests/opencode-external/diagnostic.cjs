// A real external-model tool proof, before any lifecycle qualification.
const fs = require('node:fs');
const cp = require('node:child_process');
const crypto = require('node:crypto');
const http = require('node:http');
const net = require('node:net');
const root = '/experiment/project';
const model = JSON.parse(fs.readFileSync('/opt/factorybox/opencode-external/model.json'));
fs.mkdirSync(root, {recursive: true});
for (const [name, path] of Object.entries({HOME: '/experiment/home', XDG_DATA_HOME: '/experiment/data',
  XDG_STATE_HOME: '/experiment/state', XDG_CACHE_HOME: '/experiment/cache', XDG_CONFIG_HOME: '/experiment/config'})) {
  fs.mkdirSync(path, {recursive: true}); process.env[name] = path;
}
cp.execFileSync('git', ['init', '--quiet', root]);
// The public bearer default is the actual provider's published behavior.
const config = {model: 'opencode/' + model.id, update: 'disable', share: 'disabled', snapshots: false, warming: false,
  permissions: [{action: '*', resource: '*', effect: 'allow'}], providers: {opencode: {
    package: '@opencode/ai/providers/openai-compatible', settings: {baseURL: 'http://127.0.0.1:4097/v1', apiKey: 'public'},
    models: {[model.id]: {name: model.label, capabilities: {tools: true, input: ['text'], output: ['text']},
      cost: {input: 0, output: 0}, limit: {context: 32768, output: 4096}}}}}};
const password = crypto.randomBytes(32).toString('hex');
const log = fs.openSync('/experiment/server.log', 'w');
const bridge = cp.spawn('/usr/local/bin/node', ['/opt/factorybox/opencode-external/bridge.cjs'], {stdio: ['ignore', log, log]});
const server = cp.spawn('/opt/factory/opencode/musl/bun', ['/opt/factory/opencode/server/server.mjs'], {
  cwd: root, stdio: ['ignore', log, log], env: {...process.env, OPENCODE_PASSWORD: password,
    OPENCODE_DB: root + '/.git/opencode.db', FACTORY_OPENCODE_HOST: '127.0.0.1', FACTORY_OPENCODE_CONFIG: JSON.stringify(config),
    OPENCODE_DISABLE_MODELS_FETCH: '1', UV_USE_IO_URING: '0'},
});
const wait = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
function api(path, body) {
  return new Promise((resolve, reject) => {
    const request = http.request('http://127.0.0.1:4096' + path, {method: body === undefined ? 'GET' : 'POST', agent: false,
      headers: {authorization: 'Basic ' + Buffer.from('opencode:' + password).toString('base64'), 'content-type': 'application/json'}}, response => {
      let content = '';
      response.on('data', chunk => { content += chunk; if (content.length > 2 << 20) request.destroy(new Error('bounded API response')); });
      response.on('end', () => {
        if (response.statusCode < 200 || response.statusCode >= 300) return reject(new Error('OpenCode HTTP ' + response.statusCode + ': ' + content));
        try { resolve(JSON.parse(content)); } catch (error) { reject(error); }
      });
      response.on('error', reject);
    });
    request.setTimeout(3000, () => request.destroy(new Error('bounded API timeout')));
    request.on('error', reject);
    request.end(body === undefined ? undefined : JSON.stringify(body));
  });
}
function tool(operation, marker) {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(root + '/.factorybox-tool.sock');
    let content = '';
    socket.on('connect', () => socket.end(JSON.stringify({operation, marker}) + '\n'));
    socket.on('data', chunk => { content += chunk; if (content.length > 65536) socket.destroy(new Error('bounded tool response')); });
    socket.on('end', () => { try { resolve(JSON.parse(content)); } catch (error) { reject(error); } });
    socket.on('error', reject);
    socket.setTimeout(3000, () => socket.destroy(new Error('bounded tool timeout')));
  });
}
async function main() {
  const help = cp.spawnSync('/usr/local/bin/devcontainer', ['up', '--help'], {encoding: 'utf8', timeout: 10000, maxBuffer: 1 << 20});
  fs.writeFileSync('/experiment/devcontainer-up-help.txt', help.stdout + help.stderr);
  const wrapper = fs.realpathSync('/usr/local/bin/devcontainer');
  const candidate = require('node:path').join(require('node:path').dirname(wrapper), 'dist/spec-node/devContainersSpecCLI.js');
  const cli = fs.existsSync(candidate) ? candidate : wrapper;
  const source = fs.readFileSync(cli, 'utf8');
  const excerpts = [];
  for (const needle of ['Failed to fetch control manifest', 'containers.dev', 'FEATURES_CONTROL', 'CONTROL_MANIFEST']) {
    let start = 0;
    for (let index = 0; index < 5; index++) {
      const at = source.indexOf(needle, start); if (at < 0) break;
      excerpts.push({needle, at, text: source.slice(Math.max(0, at - 600), at + 600)}); start = at + needle.length;
    }
  }
  fs.writeFileSync('/experiment/devcontainer-control-source.json', JSON.stringify({path: cli, bytes: Buffer.byteLength(source), sha256: crypto.createHash('sha256').update(source).digest('hex'), excerpts}, null, 2));
  let ready = false;
  for (let index = 0; index < 150; index++) {
    if (server.exitCode !== null || bridge.exitCode !== null) throw new Error('owned OpenCode/bridge exited');
    try { await api('/api/info'); ready = true; break; } catch {}
    await wait(100);
  }
  if (!ready) throw new Error('OpenCode API startup deadline');
  const session = (await api('/api/session', {title: 'Synthetic Factorybox external tool test', model: {providerID: 'opencode', id: model.id},
    location: {directory: root}, permissions: [{action: '*', resource: '*', effect: 'allow'}]})).data;
  fs.writeFileSync('/experiment/session.json', JSON.stringify(session));
  const command = '/usr/local/bin/node /opt/factorybox/opencode-external/held-tool.cjs ' + root;
  await api('/api/session/' + session.id + '/prompt', {text: 'This is an authorized synthetic test. Use the shell tool exactly once, command: ' + command +
    '. Set timeout to 0 so it waits for completion. The controller releases it later. Do not inspect files or run another command. When it finishes, report FACTORYBOX_AGENT_ACK and its exact nonce, ram_sha256 and exit status. The expected exit is deliberately nonzero. Do not repeat it.', resume: true});
  let armed;
  for (let index = 0; index < 450; index++) {
    try { armed = await tool('state'); break; } catch {}
    if (fs.readFileSync('/experiment/server.log', 'utf8').includes('ModelInitializationError')) break;
    await wait(100);
  }
  if (!armed || armed.ram_bytes !== 16 << 20) {
    fs.writeFileSync('/experiment/messages.json', JSON.stringify(await api('/api/session/' + session.id + '/message')));
    throw new Error('external agent did not start the actual held tool');
  }
  fs.writeFileSync(root + '/.factorybox-watched/signal', 'external-watch');
  let watched;
  for (let index = 0; index < 100; index++) {
    watched = await tool('state');
    if (watched.watch_marker === 'external-watch') break;
    await wait(25);
  }
  if (watched.watch_marker !== 'external-watch') throw new Error('actual tool watch was not delivered');
  const advanced = await tool('advance', 'external-probe');
  if (advanced.counter !== 1 || advanced.nonce !== armed.nonce || advanced.ram_sha256 === armed.ram_sha256) throw new Error('actual private RAM did not advance');
  fs.writeFileSync(root + '/.factorybox-tool-release', 'release\n');
  for (let index = 0; index < 450; index++) {
    const messages = await api('/api/session/' + session.id + '/message');
    fs.writeFileSync('/experiment/messages.json', JSON.stringify(messages));
    const text = JSON.stringify(messages);
    if (text.includes('FACTORYBOX_AGENT_ACK') && fs.existsSync(root + '/.factorybox-tool-result.json')) {
      const actual = JSON.parse(fs.readFileSync(root + '/.factorybox-tool-result.json'));
      // Check the assistant's text separately: a user prompt containing the ACK is not evidence.
      const assistant = messages.data?.filter(message => message.type === 'assistant') || [];
      const acknowledged = assistant.flatMap(message => message.content || []).filter(part => part.type === 'text').map(part => part.text || '').join('\n');
      const calls = assistant.flatMap(message => message.content || []).filter(part => part.type === 'tool');
      if (calls.length > 1) throw new Error('external agent issued an unintended additional tool call');
      if (actual.exit === 37 && actual.nonce === advanced.nonce && actual.ram_sha256 === advanced.ram_sha256 &&
          calls.length === 1 && calls[0].name === 'shell' && calls[0].state?.status === 'completed' && calls[0].state?.metadata?.exit === 37 &&
          calls[0].state.input.command === command && calls[0].state.input.timeout === 0 &&
          acknowledged.includes('FACTORYBOX_AGENT_ACK') && acknowledged.includes(actual.nonce) && acknowledged.includes(actual.ram_sha256) && acknowledged.includes('37')) {
        const result = {passed: true, model, session_id: session.id, armed, advanced, actual, tool_calls: calls.length, assistant_ack: acknowledged};
        fs.writeFileSync('/experiment/result.json', JSON.stringify(result, null, 2));
        console.log(JSON.stringify(result)); return;
      }
    }
    await wait(100);
  }
  throw new Error('external agent did not acknowledge its actual result and exit');
}
main().catch(error => { console.error(error.stack); process.exitCode = 1; }).finally(() => {
  if (server.exitCode === null) server.kill('SIGTERM');
  if (bridge.exitCode === null) bridge.kill('SIGTERM');
  setTimeout(() => process.exit(process.exitCode || 0), 1000).unref();
});
