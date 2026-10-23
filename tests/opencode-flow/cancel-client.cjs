// Exact shipped OpenCode interruption API; never sends a new prompt or tool call.
const fs = require('node:fs');
const http = require('node:http');
const crypto = require('node:crypto');
const root = '/workspace/project';
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
function api(path, body) {
  const password = fs.readFileSync('/run/fixture-opencode-password', 'utf8');
  return new Promise((resolve, reject) => {
    let result;
    const request = http.request('http://127.0.0.1:4096' + path, {
      method: body === undefined ? 'GET' : 'POST', agent: false,
      headers: {authorization: 'Basic ' + Buffer.from('opencode:' + password).toString('base64'),
        'content-type': 'application/json', connection: 'close'},
    }, response => {
      let content = '';
      response.on('data', chunk => { content += chunk; if (content.length > 2 << 20) request.destroy(new Error('bounded cancel response')); });
      response.on('end', () => { result = {status: response.statusCode, content}; });
      response.on('error', reject);
    });
    request.on('socket', socket => socket.once('close', () => {
      if (!result) return reject(new Error('incomplete cancel response'));
      if (result.status !== 200) return reject(new Error('cancel API HTTP ' + result.status));
      try { resolve(JSON.parse(result.content)); } catch (error) { reject(error); }
    }));
    request.on('error', reject);
    request.setTimeout(3000, () => request.destroy(new Error('cancel API timeout')));
    request.end(body === undefined ? undefined : JSON.stringify(body));
  });
}
function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])]));
  return value;
}
function digest(value) { return crypto.createHash('sha256').update(JSON.stringify(canonical(value))).digest('hex'); }
function launches() {
  const rows = fs.readFileSync(root + '/.factorybox-tool-launches.jsonl', 'utf8').trim().split('\n').map(JSON.parse);
  if (rows.length !== 1 || !Number.isSafeInteger(rows[0].pid) || rows[0].pid <= 1) throw new Error('tool execution repeated or absent');
  return rows;
}
function identity(pid) {
  try {
    const stat = fs.readFileSync('/proc/' + pid + '/stat', 'utf8');
    const fields = stat.slice(stat.lastIndexOf(')') + 2).trim().split(/\s+/);
    return {state: fields[0], birth: fields[19]};
  } catch (error) { if (error.code === 'ENOENT') return undefined; throw error; }
}
function gone(saved) {
  const current = identity(saved.pid);
  return !current || current.birth !== saved.birth || current.state === 'Z' || current.state === 'X';
}
async function call(session) {
  const messages = await api('/api/session/' + session.id + '/message');
  const parts = (messages.data || []).filter(message => message.type === 'assistant').flatMap(message => message.content || []);
  const calls = parts.filter(part => part.type === 'tool');
  const command = '/usr/local/bin/node /opt/factorybox/opencode-flow/cancel-held-tool.cjs ' + root;
  if (calls.length !== 1 || calls[0].name !== 'shell' || calls[0].state?.input?.command !== command || calls[0].state.input.timeout !== 0) {
    throw new Error('cancellation target is not its single exact held shell');
  }
  return {part: calls[0], interrupted: (messages.data || []).some(message => message.type === 'idle' && message.outcome === 'interrupted')};
}
function noCompletion() {
  if (fs.existsSync(root + '/.factorybox-tool-release') || fs.existsSync(root + '/.factorybox-tool-result.json')) {
    throw new Error('canceled tool unexpectedly reached its normal completion');
  }
  launches();
  const model = JSON.parse(fs.readFileSync(root + '/.factorybox-model.json'));
  if (model.observed.filter(item => item.kind === 'tool-call').length !== 1 || model.observed.some(item => item.kind === 'failure')) {
    throw new Error('unexpected inference or tool replay after cancellation');
  }
}
async function main() {
  const session = JSON.parse(fs.readFileSync(root + '/.factorybox-session.json'));
  const path = root + '/.factorybox-canceled.json';
  if (process.argv[2] === 'interrupt') {
    if (fs.existsSync(path)) throw new Error('cancellation already observed');
    launches();
    const armed = JSON.parse(fs.readFileSync(root + '/.factorybox-tool-armed.json'));
    const before = identity(armed.pid);
    if (!before || ['Z', 'X'].includes(before.state) || armed.ram_bytes !== 16 << 20) throw new Error('held tool was not live before interruption');
    const target = {pid: armed.pid, birth: before.birth};
    const first = await api('/api/session/' + session.id + '/interrupt?resume=false', {});
    if (first.interrupted !== true) throw new Error('active execution was not interrupted: ' + JSON.stringify(first));
    let observed;
    for (const deadline = Date.now() + 12000; ; ) {
      observed = await call(session);
      fs.writeFileSync(root + '/.factorybox-interruption-observed.json', JSON.stringify(observed), {mode: 0o600});
      if (observed.part.state.status === 'error' && observed.interrupted && gone(target)) break;
      if (Date.now() >= deadline) throw new Error('interruption did not settle the actual tool: ' + JSON.stringify(observed));
      await sleep(50);
    }
    const error = observed.part.state.error;
    if (!error || typeof error.type !== 'string' || !error.type || typeof error.message !== 'string' || !error.message) {
      throw new Error('missing structured durable tool cancellation error: ' + JSON.stringify(observed));
    }
    const idle = await api('/api/session/' + session.id + '/interrupt?resume=false', {});
    if (idle.interrupted !== false) throw new Error('idle interruption started or retained execution');
    noCompletion();
    const receipt = {session_id: session.id, canceled: true, launches: 1, tool_status: 'error',
      call_sha256: digest(observed.part), tool_error: error, original_target: target,
      original_nonce: armed.nonce, idle_repeat: true, original_process_gone: true, execution_interrupted: true};
    fs.writeFileSync(path, JSON.stringify(receipt), {mode: 0o600, flag: 'wx'});
    console.log(JSON.stringify(receipt));
  } else if (process.argv[2] === 'verify') {
    const saved = JSON.parse(fs.readFileSync(path));
    const observed = await call(session);
    if (saved.session_id !== session.id || !saved.canceled || observed.part.state.status !== 'error' || !observed.interrupted ||
        saved.call_sha256 !== digest(observed.part) || !gone(saved.original_target)) throw new Error('cancellation changed or its tool revived');
    const idle = await api('/api/session/' + session.id + '/interrupt?resume=false', {});
    if (idle.interrupted !== false) throw new Error('canceled session unexpectedly active');
    noCompletion();
    console.log(JSON.stringify(saved));
  } else throw new Error('unknown cancel operation');
}
main().catch(error => { console.error(error.stack); process.exit(1); });
