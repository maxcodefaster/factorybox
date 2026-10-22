const fs = require('node:fs');
const http = require('node:http');
const net = require('node:net');
const root = process.env.FACTORYBOX_OPENCODE_TEST_ROOT || '/workspace/project';
const sleep = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
function api(path, body) {
  const password = fs.readFileSync('/run/fixture-opencode-password', 'utf8');
  return new Promise((resolve, reject) => {
    let result;
    const request = http.request('http://127.0.0.1:4096' + path, {method: body === undefined ? 'GET' : 'POST', agent: false,
      headers: {authorization: 'Basic ' + Buffer.from('opencode:' + password).toString('base64'), 'content-type': 'application/json', connection: 'close'}}, response => {
      let content = '';
      response.on('data', chunk => { content += chunk; if (content.length > 2 << 20) request.destroy(new Error('bounded agent response')); });
      response.on('end', () => { result = {status: response.statusCode, content}; });
      response.on('error', reject);
    });
    request.on('socket', socket => socket.once('close', () => {
      if (!result) return reject(new Error('agent response not complete'));
      if (result.status < 200 || result.status >= 300) return reject(new Error('agent HTTP ' + result.status + ': ' + result.content));
      try { resolve(JSON.parse(result.content)); } catch(error) { reject(error); }
    }));
    request.on('error', reject);
    request.setTimeout(3000, () => request.destroy(new Error('agent API timeout')));
    request.end(body === undefined ? undefined : JSON.stringify(body));
  });
}
function tool(operation, marker) {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(root + '/.factorybox-tool.sock');
    let content = '', result;
    socket.setEncoding('utf8');
    socket.on('connect', () => socket.end(JSON.stringify({operation, marker}) + '\n'));
    socket.on('data', chunk => { content += chunk; if (content.length > 65536) socket.destroy(new Error('bounded tool response')); });
    socket.on('end', () => { try { result = JSON.parse(content); } catch(error) { reject(error); } });
    socket.on('close', () => result && resolve(result));
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
  throw new Error('actual tool inotify watch did not deliver the new file content');
}
async function main() {
  const command = process.argv[2];
  if (command === 'arm') {
    const session = (await api('/api/session', {title: 'Factorybox held tool', model: {providerID: 'factorybox', id: 'probe'}, location: {directory: root},
      permissions: [{action: '*', resource: '*', effect: 'allow'}]})).data;
    fs.writeFileSync(root + '/.factorybox-session.json', JSON.stringify(session));
    await api('/api/session/' + session.id + '/prompt', {text: 'Run the held local tool once, wait for its completion and report its result.', resume: true});
    let state;
    for (let attempt = 0; attempt < 150; attempt++) {
      try { state = await tool('state'); break; } catch(error) { if (attempt === 149) throw error; await sleep(100); }
    }
    state = await notify('before-checkpoint');
    console.log(JSON.stringify({session_id: session.id, state}));
  } else if (command === 'state' || command === 'advance') {
    const session = JSON.parse(fs.readFileSync(root + '/.factorybox-session.json'));
    console.log(JSON.stringify({session_id: session.id, state: await tool(command, process.argv[3])}));
  } else if (command === 'watch') {
    const session = JSON.parse(fs.readFileSync(root + '/.factorybox-session.json'));
    console.log(JSON.stringify({session_id: session.id, state: await notify(process.argv[3])}));
  } else if (command === 'finish') {
    const session = JSON.parse(fs.readFileSync(root + '/.factorybox-session.json'));
    const before = await tool('state');
    fs.writeFileSync(root + '/.factorybox-tool-release', 'release\n');
    for (let attempt = 0; attempt < 150; attempt++) {
      const messages = await api('/api/session/' + session.id + '/message');
      const text = JSON.stringify(messages);
      if (text.includes('FACTORYBOX_AGENT_ACK')) {
        const actual = JSON.parse(fs.readFileSync(root + '/.factorybox-tool-result.json'));
        const model = JSON.parse(fs.readFileSync(root + '/.factorybox-model.json'));
        const received = model.observed.find(value => value.kind === 'tool-result');
        if (actual.exit !== 37 || actual.nonce !== before.nonce || actual.ram_sha256 !== before.ram_sha256 ||
          !received?.content.includes(actual.nonce) || !received.content.includes(actual.ram_sha256) ||
          !text.includes(actual.nonce) || !text.includes('Exited with code 37') || model.observed.some(value => value.kind === 'failure')) throw new Error('agent lost actual tool result/exit/RAM identity');
        console.log(JSON.stringify({session_id: session.id, result: actual, agent_ack: true, model_received_exit: 37}));
        return;
      }
      await sleep(100);
    }
    throw new Error('agent did not finish consuming its actual tool result');
  } else throw new Error('unknown flow client operation');
}
main().catch(error => { console.error(error.stack); process.exit(1); });
