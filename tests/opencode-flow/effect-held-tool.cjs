// One real outside-world request before the unchanged private-RAM held tool.
const fs = require('node:fs'), http = require('node:http'), crypto = require('node:crypto');
const root = process.argv[2];
if (root !== '/workspace/project') throw new Error('requires exact owned Factory project');
const saved = JSON.parse(fs.readFileSync(root + '/.factorybox-effect-operation.json'));
if (saved.schema !== 1 || !/^[a-f0-9]{64}$/.test(saved.operation || '')) throw new Error('missing exact outside operation');
fs.appendFileSync(root + '/.factorybox-effect-launches.jsonl', JSON.stringify({operation: saved.operation, pid: process.pid}) + '\n', {mode: 0o600});
let result;
// A single submission. Neither transport errors nor an HTTP error are retried.
const request = http.request('http://127.0.0.1:4098/fixture/effects/apply', {
  method: 'POST', agent: false, headers: {'content-type': 'application/json', connection: 'close'},
}, response => {
  let body = '';
  response.on('data', chunk => {body += chunk; if (body.length > 4096) request.destroy(new Error('bounded outside response'));});
  response.on('end', () => {result = {status: response.statusCode, body};});
  response.on('error', error => {throw error;});
});
request.on('socket', socket => socket.once('close', () => {
    if (!result || result.status !== 502 || result.body.includes(saved.operation)) throw new Error('outside request did not lose its actual acknowledgment');
    const unknown = {schema: 1, operation: saved.operation, result_unknown: true, submissions: 1, response_status: 502,
      response_sha256: crypto.createHash('sha256').update(result.body).digest('hex')};
    fs.writeFileSync(root + '/.factorybox-effect-unknown.json', JSON.stringify(unknown), {mode: 0o600, flag: 'wx'});
    process.stdout.write('FACTORYBOX_EFFECT_UNCERTAIN_' + saved.operation + '\n', () => require('./held-tool.cjs'));
}));
request.on('error', error => {throw error;});
request.setTimeout(3000, () => request.destroy(new Error('outside result uncertain after timeout')));
request.end(JSON.stringify({operation: saved.operation}));
