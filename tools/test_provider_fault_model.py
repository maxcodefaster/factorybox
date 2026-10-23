from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


@unittest.skipUnless(shutil.which("node"), "actual provider fault counterpart requires Node")
class ProviderFaultSyntheticContinuationTests(unittest.TestCase):
    def test_actual_http_retry_with_synthetic_user_message_preserves_fault_until_new_prompt(self):
        program = r"""
const fs = require('node:fs'), http = require('node:http'), vm = require('node:vm'), assert = require('node:assert/strict');
let source = fs.readFileSync(process.argv[1], 'utf8');
assert(source.includes("const root = '/experiment';") && source.includes(".listen(4097, '127.0.0.1',"));
source = source.replace("const root = '/experiment';", 'const root = ' + JSON.stringify(process.argv[2]) + ';')
  .replace(".listen(4097, '127.0.0.1',", ".listen(0, '127.0.0.1',");
let server;
const original = 'FACTORYBOX_PROVIDER_PARTIAL_' + 'a'.repeat(32);
const synthetic = 'The previous response was interrupted. Continue from where you left off without repeating completed content.';
function request(messages) {
  return new Promise((resolve, reject) => {
    const req = http.request({host: '127.0.0.1', port: server.address().port, path: '/v1/chat/completions', method: 'POST', agent: false,
      headers: {'content-type': 'application/json', 'x-opencode-session': 'synthetic-session'}}, res => {
      let body = '';
      res.on('data', chunk => {body += chunk;});
      res.on('end', () => resolve({status: res.statusCode, body, aborted: false}));
      res.on('aborted', () => resolve({status: res.statusCode, body, aborted: true}));
      res.on('error', () => {});
    });
    req.on('error', reject);
    req.end(JSON.stringify({stream: true, tools: [{function: {name: 'shell'}}], messages}));
  });
}
vm.runInNewContext(source, {require: name => name === 'node:http' ? {createServer: handler => {
  server = http.createServer(handler);
  server.on('listening', async () => {
    try {
      const first = await request([{role: 'user', content: original}]);
      assert(first.aborted && first.body.includes('call_factorybox_partial'));
      const retry = await request([{role: 'user', content: original}, {role: 'user', content: synthetic}]);
      assert.equal(retry.status, 400, 'synthetic retry must remain the original fault');
      assert(retry.body.includes('FACTORYBOX_PROVIDER_PARTIAL_RETRY_REJECTED'));
      const continued = await request([{role: 'user', content: original}, {role: 'user', content: synthetic},
        {role: 'user', content: 'FACTORYBOX_PROVIDER_CONTINUE_' + 'b'.repeat(32)}]);
      assert.equal(continued.status, 200);
      assert(continued.body.includes('FACTORYBOX_PROVIDER_ACK_' + 'b'.repeat(32)));
    } catch (error) {console.error(error.stack); process.exitCode = 1;}
    finally {server.close(); server.closeAllConnections();}
  });
  return server;
}} : require(name), console: {log() {}}, setTimeout});
"""
        model = Path(__file__).resolve().parents[1] / "tests/opencode-flow/provider-fault-model.cjs"
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(["node", "-e", program, str(model), directory], capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
