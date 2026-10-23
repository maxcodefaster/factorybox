// Actual pinned agent provider-error and incomplete-tool-call component proof.
const fs = require('node:fs'), cp = require('node:child_process'), crypto = require('node:crypto'), http = require('node:http');
const root = '/experiment/project';
fs.mkdirSync(root, {recursive: true});
for (const [name, path] of Object.entries({HOME: '/experiment/home', XDG_DATA_HOME: '/experiment/data',
  XDG_STATE_HOME: '/experiment/state', XDG_CACHE_HOME: '/experiment/cache', XDG_CONFIG_HOME: '/experiment/config'})) {
  fs.mkdirSync(path, {recursive: true}); process.env[name] = path;
}
cp.execFileSync('git', ['init', '--quiet', root]);
const password = crypto.randomBytes(32).toString('hex');
const log = fs.openSync('/experiment/server.log', 'w');
const model = cp.spawn('/usr/local/bin/node', ['/opt/factorybox/opencode-flow/provider-fault-model.cjs'], {stdio: ['ignore', log, log]});
const server = cp.spawn('/opt/factory/opencode/musl/bun', ['/opt/factory/opencode/server/server.mjs'], {
  cwd: root, stdio: ['ignore', log, log], env: {...process.env, OPENCODE_PASSWORD: password,
    OPENCODE_DB: root + '/.git/opencode.db', FACTORY_OPENCODE_HOST: '127.0.0.1',
    FACTORY_OPENCODE_CONFIG: fs.readFileSync('/opt/factorybox/opencode-flow/config.json', 'utf8'),
    OPENCODE_DISABLE_MODELS_FETCH: '1', UV_USE_IO_URING: '0'},
});
const wait = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
function api(path, body) {
  return new Promise((resolve, reject) => {
    let result;
    const request = http.request('http://127.0.0.1:4096' + path, {method: body === undefined ? 'GET' : 'POST', agent: false,
      headers: {authorization: 'Basic ' + Buffer.from('opencode:' + password).toString('base64'), 'content-type': 'application/json', connection: 'close'}}, response => {
      let content = '';
      response.on('data', chunk => {content += chunk; if (content.length > 2 << 20) request.destroy(new Error('bounded fault API response'));});
      response.on('end', () => {result = {status: response.statusCode, content};});
      response.on('error', reject);
    });
    request.on('socket', socket => socket.once('close', () => {
      if (!result || result.status !== 200) return reject(new Error('incomplete or failed fault API response'));
      try {resolve(JSON.parse(result.content));} catch (error) {reject(error);}
    }));
    request.on('error', reject);
    request.setTimeout(3000, () => request.destroy(new Error('bounded fault API timeout')));
    request.end(body === undefined ? undefined : JSON.stringify(body));
  });
}
function noExecution(messages) {
  if (fs.existsSync('/experiment/partial-tool-launches.jsonl')) throw new Error('incomplete provider call executed an actual shell');
  const calls = messages.filter(message => message.type === 'assistant').flatMap(message => message.content || []).filter(part => part.type === 'tool');
  if (calls.some(part => ['running', 'completed'].includes(part.state?.status))) throw new Error('incomplete provider call reached tool execution');
  return calls;
}
async function settle(session, kind, oldIDs = new Set()) {
  for (const deadline = Date.now() + 20000; ; ) {
    const messages = (await api('/api/session/' + session.id + '/message')).data || [];
    fs.writeFileSync('/experiment/' + kind + '-messages.json', JSON.stringify(messages));
    const calls = noExecution(messages);
    const idles = messages.filter(message => message.type === 'idle' && !oldIDs.has(message.id));
    if (idles.length === 1) return {messages, calls, idle: idles[0]};
    if (idles.length > 1 || Date.now() >= deadline) throw new Error(kind + ' did not settle its exact execution');
    await wait(50);
  }
}
async function main() {
  let info;
  for (let deadline = Date.now() + 15000; ; ) {
    if (server.exitCode !== null || model.exitCode !== null) throw new Error('owned fault process exited');
    try {info = await api('/api/info'); break;} catch (error) {if (Date.now() >= deadline) throw error;}
    await wait(100);
  }
  if (info.version !== '2.0.27' || info.pid !== server.pid) throw new Error('fault probe did not reach the pinned actual agent');
  const cases = [];
  for (const kind of ['reject', 'partial']) {
    const session = (await api('/api/session', {title: 'Synthetic provider ' + kind, model: {providerID: 'factorybox', id: 'probe'},
      location: {directory: root}, permissions: [{action: '*', resource: '*', effect: 'allow'}]})).data;
    const token = crypto.randomBytes(16).toString('hex');
    // Each mutation is submitted once. A lost API response is a test failure.
    await api('/api/session/' + session.id + '/prompt', {text: 'FACTORYBOX_PROVIDER_' + kind.toUpperCase() + '_' + token, resume: true});
    const failed = await settle(session, kind);
    const errors = [failed.idle.error, ...failed.messages.filter(message => message.type === 'assistant').map(message => message.error)].filter(Boolean);
    if (failed.idle.outcome !== 'failed' || !errors.some(error => typeof error.type === 'string' && error.type && typeof error.message === 'string' && error.message)) {
      throw new Error(kind + ' lacks its actual durable failed outcome: ' + JSON.stringify(failed));
    }
    if (kind === 'reject' && !errors.some(error => error.message.includes('FACTORYBOX_PROVIDER_REJECTED'))) throw new Error('rejected inference error lost its actual provider message');
    if (kind === 'reject' && failed.calls.length !== 0) throw new Error('rejected inference created a tool call');
    if (kind === 'partial' && (failed.calls.length !== 1 || failed.calls[0].id !== 'call_factorybox_partial' ||
        failed.calls[0].name !== 'shell' || failed.calls[0].executed !== false || failed.calls[0].state?.status !== 'error' ||
        failed.calls[0].state.error.type !== 'provider.transport')) throw new Error('partial call lacks its actual nonexecuted transport error');
    const oldIDs = new Set(failed.messages.map(message => message.id));
    const continuedToken = crypto.randomBytes(16).toString('hex');
    await api('/api/session/' + session.id + '/prompt', {text: 'FACTORYBOX_PROVIDER_CONTINUE_' + continuedToken, resume: true});
    const continued = await settle(session, kind + '-continued', oldIDs);
    const ack = 'FACTORYBOX_PROVIDER_ACK_' + continuedToken;
    const answers = continued.messages.filter(message => !oldIDs.has(message.id) && message.type === 'assistant').flatMap(message => message.content || []);
    if (continued.idle.outcome !== 'succeeded' || !answers.some(part => part.type === 'text' && part.text === ack)) throw new Error('new prompt after ' + kind + ' did not answer successfully');
    const idle = await api('/api/session/' + session.id + '/interrupt?resume=false', {});
    if (idle.interrupted !== false) throw new Error('provider fault continuation did not reach idle');
    const modelEvents = JSON.parse(fs.readFileSync('/experiment/provider-fault-model.json')).observed;
    if (modelEvents.some(item => item.kind === 'unexpected-failure')) throw new Error('unexpected fixture failure');
    const observed = modelEvents.filter(item => item.session === session.id);
    const original = observed.filter(item => item.token === token);
    if (kind === 'reject' && (original.length !== 1 || original[0].kind !== 'expected-rejection')) throw new Error('nonretryable rejection was repeated');
    if (kind === 'partial' && (original.filter(item => item.kind === 'expected-partial' && item.flushed).length !== 1 ||
        original.length > 2 || original.some(item => !['expected-partial', 'expected-partial-retry-rejection'].includes(item.kind)))) throw new Error('partial response lacks its bounded actual delivery');
    cases.push({kind, session_id: session.id, failed_idle: failed.idle, errors, incomplete_parts: failed.calls,
      inference_attempts: original.length, no_tool_execution: true, continuation_ack: ack, continuation_idle: continued.idle,
      continued_same_session: true, idle_repeat: true});
  }
  noExecution([]);
  const result = {passed: true, info, cases, actual_tool_launches: 0,
    scope: 'Synthetic injected provider rejection and incomplete streamed tool call on the actual shipped agent. No external provider, outside-world effect or lifecycle qualification.'};
  fs.writeFileSync('/experiment/result.json', JSON.stringify(result, null, 2));
  console.log(JSON.stringify(result));
}
main().catch(error => {console.error(error.stack); process.exitCode = 1;}).finally(() => {
  if (server.exitCode === null) server.kill('SIGTERM');
  if (model.exitCode === null) model.kill('SIGTERM');
  setTimeout(() => process.exit(process.exitCode || 0), 1000).unref();
});
