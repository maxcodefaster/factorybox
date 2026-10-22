// Short native API/tool proof before adding any checkpoint/restore claim.
const fs = require('node:fs');
const cp = require('node:child_process');
const crypto = require('node:crypto');
const root = '/experiment/project';
fs.mkdirSync(root, {recursive: true});
for (const [name, path] of Object.entries({HOME: '/experiment/home', XDG_DATA_HOME: '/experiment/data',
  XDG_STATE_HOME: '/experiment/state', XDG_CACHE_HOME: '/experiment/cache', XDG_CONFIG_HOME: '/experiment/config'})) {
  fs.mkdirSync(path, {recursive: true}); process.env[name] = path;
}
cp.execFileSync('git', ['init', '--quiet', root]);
process.env.FACTORYBOX_OPENCODE_TEST_ROOT = root;
const config = fs.readFileSync('/opt/factorybox/opencode-flow/config.json', 'utf8');
const password = crypto.randomBytes(32).toString('hex');
fs.writeFileSync('/run/fixture-opencode-password', password, {mode: 0o600});
const log = fs.openSync('/experiment/server.log', 'w');
const model = cp.spawn('/usr/local/bin/node', ['/opt/factorybox/opencode-flow/model.cjs'], {stdio: ['ignore', log, log]});
const server = cp.spawn('/opt/factory/opencode/musl/bun', ['/opt/factory/opencode/server/server.mjs'], {
  cwd: root, stdio: ['ignore', log, log], env: {...process.env, OPENCODE_PASSWORD: password,
    OPENCODE_DB: root + '/.git/opencode.db', FACTORY_OPENCODE_HOST: '127.0.0.1', FACTORY_OPENCODE_CONFIG: config,
    OPENCODE_DISABLE_MODELS_FETCH: '1', UV_USE_IO_URING: '0'},
});
const wait = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
async function run(operation, ...args) {
  return new Promise((resolve, reject) => {
    cp.execFile('/usr/local/bin/node', ['/opt/factorybox/opencode-flow/client.cjs', operation, ...args], {env: process.env, timeout: 25000, maxBuffer: 1 << 20}, (error, stdout, stderr) => {
      if (error) return reject(new Error(operation + ': ' + error + ': ' + stderr + '\n' + fs.readFileSync('/experiment/server.log', 'utf8').slice(-16000)));
      try { resolve(JSON.parse(stdout)); } catch(error) { reject(error); }
    });
  });
}
async function main() {
  let ready = false;
  for (let index = 0; index < 150; index++) {
    if (server.exitCode !== null || model.exitCode !== null) throw new Error('owned OpenCode/model process exited: ' + fs.readFileSync('/experiment/server.log', 'utf8'));
    try {
      const response = await fetch('http://127.0.0.1:4096/api/info', {headers: {authorization: 'Basic ' + Buffer.from('opencode:' + password).toString('base64')}, signal: AbortSignal.timeout(1000)});
      if (response.ok) { await response.text(); ready = true; break; }
      await response.text();
    } catch {}
    await wait(100);
  }
  if (!ready) throw new Error('owned OpenCode API startup deadline');
  const armed = await run('arm');
  const advanced = await run('advance', 'diagnostic-tool');
  if (armed.state.ram_bytes !== 16 << 20 || advanced.state.counter !== 1 || advanced.state.nonce !== armed.state.nonce || advanced.state.ram_sha256 === armed.state.ram_sha256) throw new Error('actual tool RAM did not advance');
  const result = await run('finish');
  fs.writeFileSync('/experiment/result.json', JSON.stringify({passed: true, armed, advanced, result}));
  console.log(JSON.stringify({passed: true, armed, advanced, result}));
}
main().catch(error => { console.error(error.stack); process.exitCode = 1; }).finally(() => {
  if (server.exitCode === null) server.kill('SIGTERM');
  if (model.exitCode === null) model.kill('SIGTERM');
  setTimeout(() => process.exit(process.exitCode || 0), 1000).unref();
});
