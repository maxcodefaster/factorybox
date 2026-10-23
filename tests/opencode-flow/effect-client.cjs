// Add explicit outside reconciliation around the unchanged ordinary client.
const fs = require('node:fs'), cp = require('node:child_process'), http = require('node:http'), crypto = require('node:crypto');
const root = '/workspace/project';
const sha = data => crypto.createHash('sha256').update(data).digest('hex');
function operation() {
  const saved = JSON.parse(fs.readFileSync(root + '/.factorybox-effect-operation.json'));
  if (saved.schema !== 1 || !/^[a-f0-9]{64}$/.test(saved.operation || '')) throw new Error('missing exact outside operation ID');
  return saved.operation;
}
function request(port, path, body, authenticated = false) {
  return new Promise((resolve, reject) => {
    let result;
    const headers = {'content-type': 'application/json', connection: 'close'};
    if (authenticated) headers.authorization = 'Basic ' + Buffer.from('opencode:' + fs.readFileSync('/run/fixture-opencode-password', 'utf8')).toString('base64');
    const req = http.request('http://127.0.0.1:' + port + path, {method: body === undefined ? 'GET' : 'POST', agent: false, headers}, response => {
      let content = '';
      response.on('data', chunk => {content += chunk; if (content.length > 2 << 20) req.destroy(new Error('bounded reconciliation response'));});
      response.on('end', () => {result = {status: response.statusCode, content};});
      response.on('error', reject);
    });
    req.on('socket', socket => socket.once('close', () => {
      if (!result || result.status !== 200) return reject(new Error('outside reconciliation or actual agent response failed'));
      try {resolve(JSON.parse(result.content));} catch (error) {reject(error);}
    }));
    req.on('error', reject);
    req.setTimeout(3000, () => req.destroy(new Error('bounded reconciliation timeout')));
    req.end(body === undefined ? undefined : JSON.stringify(body));
  });
}
function unknown() {
  const bytes = fs.readFileSync(root + '/.factorybox-effect-unknown.json');
  const saved = JSON.parse(bytes);
  const launches = fs.readFileSync(root + '/.factorybox-effect-launches.jsonl', 'utf8').trim().split('\n').map(JSON.parse);
  if (saved.operation !== operation() || !saved.result_unknown || saved.submissions !== 1 || saved.response_status !== 502 ||
      launches.length !== 1 || launches[0].operation !== saved.operation || launches[0].pid <= 1) throw new Error('outside tool absent, repeated or no longer uncertain');
  return {saved, sha256: sha(bytes)};
}
function original() {
  const bytes = cp.execFileSync('/usr/local/bin/node', ['/opt/factorybox/opencode-flow/client.original.cjs', ...process.argv.slice(2)],
    {encoding: 'utf8', timeout: 25000, maxBuffer: 1 << 20, env: process.env});
  return JSON.parse(bytes);
}
async function main() {
  const command = process.argv[2];
  if (command === 'arm') {
    fs.writeFileSync(root + '/.factorybox-effect-operation.json', JSON.stringify({schema: 1, operation: crypto.randomBytes(32).toString('hex')}), {mode: 0o600, flag: 'wx'});
    const before = await request(4098, '/fixture/effects/status', {operation: operation()});
    if (before.operation !== operation() || before.applications !== 0) throw new Error('outside effect existed before the one real tool');
    const result = original();
    unknown();
    if (fs.existsSync(root + '/.factorybox-effect-reconciled.json')) throw new Error('uncertainty was silently reconciled before pause');
    console.log(JSON.stringify(result));
    return;
  }
  const before = unknown();
  if (fs.existsSync(root + '/.factorybox-effect-reconciled.json')) throw new Error('reconciliation leaked or a finished operation was repeated');
  if (command !== 'finish') {
    const result = original();
    if (unknown().sha256 !== before.sha256) throw new Error('restored uncertain state changed');
    console.log(JSON.stringify(result));
    return;
  }
  // Read-only outside lookup, followed by an explicit decision not to retry.
  const status = await request(4098, '/fixture/effects/status', {operation: operation()});
  if (status.schema !== 1 || status.operation !== operation() || status.applications !== 1 ||
      !/^[a-f0-9]{64}$/.test(status.last_effect_sha256 || '') || status.observer_pid <= 1) throw new Error('outside result unresolved or duplicated; keep execution stopped');
  const reconciled = {...status, operator_action: 'do-not-retry'};
  fs.writeFileSync(root + '/.factorybox-effect-reconciled.json', JSON.stringify(reconciled), {mode: 0o600, flag: 'wx'});
  const result = original();
  const messages = (await request(4096, '/api/session/' + result.session_id + '/message', undefined, true)).data || [];
  const parts = messages.filter(message => message.type === 'assistant').flatMap(message => message.content || []);
  const calls = parts.filter(part => part.type === 'tool');
  const ack = 'FACTORYBOX_EFFECT_RECONCILED_' + status.operation + '_' + status.last_effect_sha256 + ' DO_NOT_RETRY';
  const commandLine = '/usr/local/bin/node /opt/factorybox/opencode-flow/effect-held-tool.cjs ' + root;
  if (calls.length !== 1 || calls[0].name !== 'shell' || calls[0].state?.status !== 'completed' ||
      calls[0].state.input.command !== commandLine || calls[0].state.input.timeout !== 0 || calls[0].state.metadata?.exit !== 37 ||
      !parts.some(part => part.type === 'text' && part.text.includes(ack)) || unknown().sha256 !== before.sha256) {
    throw new Error('agent did not ingest uncertainty and its explicit no-retry reconciliation');
  }
  result.external_effect = {schema: 1, operation: status.operation, unknown_sha256: before.sha256,
    result_unknown_before_reconciliation: true, reconciliation: reconciled, agent_received_uncertainty: true,
    agent_received_reconciliation: true, tool_launches: 1};
  console.log(JSON.stringify(result));
}
main().catch(error => {console.error(error.stack); process.exit(1);});
