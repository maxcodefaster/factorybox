// Real-provider counterpart. The deterministic cancellation client is unchanged.
const fs = require('node:fs');
const http = require('node:http');
const crypto = require('node:crypto');
const root = '/workspace/project';
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
const hash = text => crypto.createHash('sha256').update(text).digest('hex');
function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])]));
  return value;
}
const digest = value => hash(JSON.stringify(canonical(value)));
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
      response.on('data', chunk => { content += chunk; if (content.length > 2 << 20) request.destroy(new Error('bounded external cancel response')); });
      response.on('end', () => { result = {status: response.statusCode, content}; });
      response.on('error', reject);
    });
    request.on('socket', socket => socket.once('close', () => {
      if (!result || result.status !== 200) return reject(new Error('incomplete or failed external cancel API'));
      try { resolve(JSON.parse(result.content)); } catch (error) { reject(error); }
    }));
    request.on('error', reject);
    request.setTimeout(3000, () => request.destroy(new Error('external cancel API timeout')));
    request.end(body === undefined ? undefined : JSON.stringify(body));
  });
}
function identity(pid) {
  try {
    const stat = fs.readFileSync('/proc/' + pid + '/stat', 'utf8');
    const fields = stat.slice(stat.lastIndexOf(')') + 2).trim().split(/\s+/);
    return {state: fields[0], birth: fields[19]};
  } catch (error) { if (error.code === 'ENOENT' || error.code === 'ESRCH') return undefined; throw error; }
}
function gone(saved) {
  const current = identity(saved.pid);
  return !current || current.birth !== saved.birth || ['Z', 'X'].includes(current.state);
}
function noCompletion() {
  if (fs.existsSync(root + '/.factorybox-tool-release') || fs.existsSync(root + '/.factorybox-tool-result.json')) {
    throw new Error('canceled tool unexpectedly reached normal completion');
  }
  const launches = fs.readFileSync(root + '/.factorybox-tool-launches.jsonl', 'utf8').trim().split('\n').map(JSON.parse);
  if (launches.length !== 1 || !Number.isSafeInteger(launches[0].pid) || launches[0].pid <= 1) throw new Error('tool launch replay or absence');
}
async function call(session) {
  const messages = (await api('/api/session/' + session.id + '/message')).data || [];
  const calls = messages.filter(message => message.type === 'assistant').flatMap(message => message.content || []).filter(part => part.type === 'tool');
  const command = '/usr/local/bin/node /opt/factorybox/opencode-flow/cancel-held-tool.cjs ' + root;
  if (calls.length !== 1 || calls[0].name !== 'shell' || calls[0].state?.input?.command !== command ||
      calls[0].state.input.timeout !== 0 || typeof calls[0].id !== 'string' || !calls[0].id || calls[0].id.length > 256) {
    throw new Error('external cancellation lost its single exact actual shell');
  }
  return {part: calls[0], messages, interrupted: messages.some(message => message.type === 'idle' && message.outcome === 'interrupted')};
}
function unchanged(saved, observed, session) {
  if (saved.session_id !== session.id || !saved.canceled || observed.part.id !== saved.tool_call_id ||
      observed.part.state.status !== 'error' || !observed.interrupted || digest(observed.part) !== saved.call_sha256 ||
      !gone(saved.original_target)) throw new Error('external cancellation changed or tool revived');
  noCompletion();
}
function continuationMessages(observed, saved, token, oldIDs) {
  const ack = 'FACTORYBOX_CANCEL_ACK_' + token;
  const messages = observed.messages.filter(message => !oldIDs || !oldIDs.has(message.id));
  const users = messages.filter(message => message.type === 'user' && (message.text || '').includes('FACTORYBOX_CANCEL_CONTINUE_' + token));
  const assistants = messages.filter(message => message.type === 'assistant' &&
    (message.content || []).some(part => part.type === 'text' && part.text.includes(ack) && part.text.includes(saved.tool_error.message)));
  const idles = messages.filter(message => message.type === 'idle' && message.outcome === 'succeeded');
  return {users, assistants, idles, ack, messages};
}
async function main() {
  const session = JSON.parse(fs.readFileSync(root + '/.factorybox-session.json'));
  const path = root + '/.factorybox-canceled.json';
  const operation = process.argv[2];
  if (operation === 'interrupt') {
    if (fs.existsSync(path)) throw new Error('cancellation already observed');
    noCompletion();
    const armed = JSON.parse(fs.readFileSync(root + '/.factorybox-tool-armed.json'));
    const before = identity(armed.pid);
    if (!before || ['Z', 'X'].includes(before.state) || armed.ram_bytes !== 16 << 20) throw new Error('actual tool not live before interrupt');
    const target = {pid: armed.pid, birth: before.birth};
    const first = await api('/api/session/' + session.id + '/interrupt?resume=false', {});
    if (first.interrupted !== true) throw new Error('actual active execution not interrupted');
    let observed;
    for (const deadline = Date.now() + 12000; ; ) {
      observed = await call(session);
      fs.writeFileSync(root + '/.factorybox-interruption-observed.json', JSON.stringify(observed), {mode: 0o600});
      if (observed.part.state.status === 'error' && observed.interrupted && gone(target)) break;
      if (Date.now() >= deadline) throw new Error('actual external cancellation did not settle');
      await sleep(50);
    }
    const error = observed.part.state.error;
    if (!error || typeof error.type !== 'string' || !error.type || typeof error.message !== 'string' || !error.message) throw new Error('missing durable structured tool error');
    const idle = await api('/api/session/' + session.id + '/interrupt?resume=false', {});
    if (idle.interrupted !== false) throw new Error('idle interrupt retained execution');
    noCompletion();
    const receipt = {session_id: session.id, canceled: true, launches: 1, tool_status: 'error',
      call_sha256: digest(observed.part), tool_call_id: observed.part.id, tool_error: error, original_target: target,
      original_nonce: armed.nonce, idle_repeat: true, original_process_gone: true, execution_interrupted: true};
    fs.writeFileSync(path, JSON.stringify(receipt), {mode: 0o600, flag: 'wx'});
    console.log(JSON.stringify(receipt));
    return;
  }
  if (!['verify', 'continue'].includes(operation)) throw new Error('unknown external cancel operation');
  const saved = JSON.parse(fs.readFileSync(path));
  const before = await call(session);
  unchanged(saved, before, session);
  if ((await api('/api/session/' + session.id + '/interrupt?resume=false', {})).interrupted !== false) throw new Error('canceled session active');
  const continuationPath = root + '/.factorybox-cancel-continuation.json';
  if (operation === 'verify') {
    const users = before.messages.filter(message => message.type === 'user' && /FACTORYBOX_CANCEL_CONTINUE_[a-f0-9]{32}/.test(message.text || ''));
    if (fs.existsSync(continuationPath)) {
      const continued = JSON.parse(fs.readFileSync(continuationPath));
      const proof = continuationMessages(before, saved, continued.prompt_token);
      if (users.length !== 1 || proof.users.length !== 1 || proof.assistants.length !== 1 || proof.idles.length !== 1) throw new Error('external continuation lost its actual response');
    } else if (users.length !== 0) throw new Error('external prompt leaked between independent descendants');
    console.log(JSON.stringify(saved));
    return;
  }
  if (fs.existsSync(continuationPath)) throw new Error('refusing to repeat an external continuation');
  const token = crypto.randomBytes(16).toString('hex');
  const prompt = 'FACTORYBOX_CANCEL_CONTINUE_' + token + ' Reply with FACTORYBOX_CANCEL_ACK_' + token +
    ' and quote the exact error message of the preceding canceled shell tool. Do not inspect files, run any tool, or retry that tool.';
  const oldIDs = new Set(before.messages.map(message => message.id));
  // Never retry a mutating prompt after an uncertain response.
  await api('/api/session/' + session.id + '/prompt', {text: prompt, resume: true});
  for (const deadline = Date.now() + 75000; ; ) {
    const observed = await call(session);
    fs.writeFileSync(root + '/.factorybox-interruption-observed.json', JSON.stringify(observed), {mode: 0o600});
    unchanged(saved, observed, session);
    const proof = continuationMessages(observed, saved, token, oldIDs);
    if (proof.users.length === 1 && proof.assistants.length === 1 && proof.idles.length === 1) {
      if ((await api('/api/session/' + session.id + '/interrupt?resume=false', {})).interrupted !== false) throw new Error('external continuation did not settle');
      const receipt = {session_id: session.id, call_sha256: saved.call_sha256, prompt_token: token, ack: proof.ack,
        completed: true, tool_error_consumed: true, launches: 1, tool_call_id: saved.tool_call_id,
        error_sha256: hash(saved.tool_error.message), user_message_id: proof.users[0].id,
        assistant_message_id: proof.assistants[0].id, idle_message_id: proof.idles[0].id};
      fs.writeFileSync(continuationPath, JSON.stringify(receipt), {mode: 0o600, flag: 'wx'});
      console.log(JSON.stringify(receipt));
      return;
    }
    if (proof.messages.some(message => message.type === 'idle' && message.outcome !== 'succeeded')) throw new Error('actual external continuation failed');
    if (Date.now() >= deadline) throw new Error('actual external continuation timed out');
    await sleep(100);
  }
}
main().catch(error => { console.error(error.stack); process.exit(1); });
