// Deterministic local counterpart; no inference and no external provider calls.
const fs = require('node:fs');
const http = require('node:http');
const root = process.env.FACTORYBOX_OPENCODE_TEST_ROOT || '/workspace/project';
let issued = false;
let sequence = 0;
const observed = [];
function persist() { fs.writeFileSync(root + '/.factorybox-model.json', JSON.stringify({issued, sequence, observed})); }
http.createServer((request, response) => {
  if (request.method !== 'POST' || request.url !== '/v1/chat/completions') { response.writeHead(404); return response.end(); }
  let body = '';
  request.on('data', chunk => { body += chunk; if (body.length > 2 << 20) request.destroy(new Error('bounded model request')); });
  request.on('end', () => {
    try {
      const input = JSON.parse(body);
      const tools = input.tools || [];
      const results = (input.messages || []).filter(message => message.role === 'tool');
      const shell = tools.some(tool => tool.function?.name === 'shell');
      let delta, finish;
      sequence++;
      if (results.length) {
        const content = results.map(message => typeof message.content === 'string' ? message.content : JSON.stringify(message.content)).join('\n');
        if (!content.includes('FACTORYBOX_TOOL_RESULT') || !content.includes('Exited with code 37')) throw new Error('agent did not ingest the actual tool result and exit status');
        observed.push({sequence, kind: 'tool-result', content});
        delta = {role: 'assistant', content: 'FACTORYBOX_AGENT_ACK ' + content};
        finish = 'stop';
      } else if (shell && !issued) {
        issued = true;
        observed.push({sequence, kind: 'tool-call'});
        delta = {role: 'assistant', tool_calls: [{index: 0, id: 'call_factorybox_held', type: 'function',
          function: {name: 'shell', arguments: JSON.stringify({command: '/usr/local/bin/node /opt/factorybox/opencode-flow/held-tool.cjs ' + root, timeout: 0})}}]};
        finish = 'tool_calls';
      } else if (shell) throw new Error('agent repeated the tool request before its result');
      else { observed.push({sequence, kind: 'auxiliary'}); delta = {role: 'assistant', content: 'Factorybox local tool fixture'}; finish = 'stop'; }
      persist();
      const common = {id: 'chatcmpl-fixture-' + sequence, object: 'chat.completion.chunk', created: 0, model: 'probe'};
      response.setHeader('connection', 'close');
      if (input.stream) {
        response.writeHead(200, {'content-type': 'text/event-stream'});
        response.write('data: ' + JSON.stringify({...common, choices: [{index: 0, delta, finish_reason: null}]}) + '\n\n');
        response.write('data: ' + JSON.stringify({...common, choices: [{index: 0, delta: {}, finish_reason: finish}], usage: {prompt_tokens: 12, completion_tokens: 8, total_tokens: 20}}) + '\n\n');
        response.end('data: [DONE]\n\n');
      } else {
        if (delta.tool_calls) delta.tool_calls = delta.tool_calls.map(({index, ...tool}) => tool);
        response.writeHead(200, {'content-type': 'application/json'});
        response.end(JSON.stringify({...common, object: 'chat.completion', choices: [{index: 0, message: delta, finish_reason: finish}], usage: {prompt_tokens: 12, completion_tokens: 8, total_tokens: 20}}));
      }
    } catch (error) {
      observed.push({sequence, kind: 'failure', error: String(error)}); persist();
      response.writeHead(500, {'content-type': 'application/json'}); response.end(JSON.stringify({error: String(error)}));
    }
  });
}).listen(4097, '127.0.0.1', () => { persist(); console.log('FACTORYBOX_MODEL_READY'); });
