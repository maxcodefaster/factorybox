// Optional real-model counterpart to the unchanged deterministic flow client.
// The native fixture must attach and fully release its external gateway around
// arm/finish. No inference socket or helper may survive into a capture.
const fs = require('node:fs');
const http = require('node:http');
const net = require('node:net');
const root = process.env.FACTORYBOX_OPENCODE_TEST_ROOT || '/workspace/project';
const sessionPath = root + '/.factorybox-session.json';
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
function api(path, body) {
  const password = fs.readFileSync('/run/fixture-opencode-password', 'utf8');
  return new Promise((resolve, reject) => {
    const request = http.request('http://127.0.0.1:4096' + path, {method: body === undefined ? 'GET' : 'POST', agent: false,
      headers: {authorization: 'Basic ' + Buffer.from('opencode:' + password).toString('base64'), 'content-type': 'application/json', connection: 'close'}}, response => {
      let content = '';
      response.on('data', chunk => { content += chunk; if (content.length > 2 << 20) request.destroy(new Error('bounded real-agent response')); });
      response.on('end', () => {
        if (response.statusCode < 200 || response.statusCode >= 300) return reject(new Error('OpenCode HTTP ' + response.statusCode + ': ' + content));
        try { resolve(JSON.parse(content)); } catch(error) { reject(error); }
      });
      response.on('error', reject);
    });
    request.on('error', reject);
    request.setTimeout(3000, () => request.destroy(new Error('agent API timeout')));
    request.end(body === undefined ? undefined : JSON.stringify(body));
  });
}
function tool(operation, marker) {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(root + '/.factorybox-tool.sock');
    let content = '';
    socket.setEncoding('utf8');
    socket.on('connect', () => socket.end(JSON.stringify({operation, marker}) + '\n'));
    socket.on('data', chunk => { content += chunk; if (content.length > 65536) socket.destroy(new Error('bounded tool response')); });
    socket.on('end', () => { try { resolve(JSON.parse(content)); } catch(error) { reject(error); } });
    socket.on('error', reject);
    socket.setTimeout(3000, () => socket.destroy(new Error('tool state timeout')));
  });
}
async function notify(marker) {
  if (typeof marker !== 'string' || !/^[a-z-]{1,48}$/.test(marker)) throw new Error('invalid watch marker');
  fs.writeFileSync(root + '/.factorybox-watched/signal', marker);
  for (let attempt = 0; attempt < 100; attempt++) {
    const state = await tool('state');
    if (state.watch_marker === marker) return state;
    await sleep(25);
  }
  throw new Error('actual external-agent tool watch did not deliver the file event');
}
const toolCommand = '/usr/local/bin/node /opt/factorybox/opencode-flow/held-tool.cjs ' + root;
function observed(messages) {
  const assistants = (messages.data || []).filter(message => message.type === 'assistant');
  const content = assistants.flatMap(message => message.content || []);
  const calls = content.filter(part => part.type === 'tool');
  if (calls.length !== 1 || calls[0].name !== 'shell' || calls[0].state?.input?.command !== toolCommand || calls[0].state.input.timeout !== 0) {
    throw new Error('external agent did not keep exactly the one intended shell invocation');
  }
  return {call: calls[0], text: content.filter(part => part.type === 'text').map(part => part.text || '').join('\n')};
}
async function main() {
  const command = process.argv[2];
  if (command === 'arm') {
    if (fs.existsSync(sessionPath)) throw new Error('refusing to reissue an existing agent session');
    const model = JSON.parse(fs.readFileSync('/opt/factorybox/opencode-flow/model.json'));
    const session = (await api('/api/session', {title: 'Factorybox external held tool', model: {providerID: 'opencode', id: model.id},
      location: {directory: root}, permissions: [{action: '*', resource: '*', effect: 'allow'}]})).data;
    fs.writeFileSync(sessionPath, JSON.stringify(session), {flag: 'wx'});
    await api('/api/session/' + session.id + '/prompt', {text: 'This is an authorized synthetic local test. The direct function tool named shell is available alongside execute. Shell is not in the code-mode search catalog. Call shell directly; do not call execute, search, subagent, or any other tool. Use shell exactly once, command: ' + toolCommand +
      '. Set timeout to 0 and wait for completion. The controller releases it later. Do not inspect files or run any other command. After completion, report FACTORYBOX_AGENT_ACK and the exact nonce, ram_sha256 and exit status from the tool result. Its nonzero exit is deliberate. Do not repeat it.', resume: true});
    let state;
    for (let attempt = 0; attempt < 750; attempt++) {
      try { state = await tool('state'); break; } catch {}
      await sleep(100);
    }
    if (!state || state.ram_bytes !== 16 << 20) throw new Error('external model did not start the actual 16-MiB held tool');
    observed(await api('/api/session/' + session.id + '/message'));
    state = await notify('before-checkpoint');
    console.log(JSON.stringify({session_id: session.id, state}));
  } else if (command === 'state' || command === 'advance') {
    const session = JSON.parse(fs.readFileSync(sessionPath));
    console.log(JSON.stringify({session_id: session.id, state: await tool(command, process.argv[3])}));
  } else if (command === 'watch') {
    const session = JSON.parse(fs.readFileSync(sessionPath));
    console.log(JSON.stringify({session_id: session.id, state: await notify(process.argv[3])}));
  } else if (command === 'finish') {
    const session = JSON.parse(fs.readFileSync(sessionPath));
    const before = await tool('state');
    fs.writeFileSync(root + '/.factorybox-tool-release', 'release\n', {flag: 'wx'});
    for (let attempt = 0; attempt < 750; attempt++) {
      const messages = await api('/api/session/' + session.id + '/message');
      const {call, text} = observed(messages);
      if (fs.existsSync(root + '/.factorybox-tool-result.json')) {
        const actual = JSON.parse(fs.readFileSync(root + '/.factorybox-tool-result.json'));
        if (actual.exit !== 37 || actual.nonce !== before.nonce || actual.ram_sha256 !== before.ram_sha256) throw new Error('actual restored tool lost its private result identity');
        if (call.state.status === 'completed' && call.state.metadata?.exit === 37 && text.includes('FACTORYBOX_AGENT_ACK') &&
            text.includes(actual.nonce) && text.includes(actual.ram_sha256) && /\b37\b/.test(text)) {
          console.log(JSON.stringify({session_id: session.id, result: actual, agent_ack: true, model_received_exit: 37}));
          return;
        }
      }
      await sleep(100);
    }
    throw new Error('external agent did not process the actual tool result and exit');
  } else throw new Error('unknown external flow operation');
}
main().catch(error => { console.error(error.stack); process.exit(1); });
