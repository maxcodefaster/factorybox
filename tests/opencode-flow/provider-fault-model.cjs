// Deliberate provider failures for the actual pinned OpenCode client.
const fs = require('node:fs'), http = require('node:http'), crypto = require('node:crypto');
const root = '/experiment';
const observed = [], partials = new Set();
let sequence = 0;
function persist() { fs.writeFileSync(root + '/provider-fault-model.json', JSON.stringify({observed})); }
function rejected(response, message) {
  response.writeHead(400, {'content-type': 'application/json', connection: 'close'});
  response.end(JSON.stringify({error: {message, type: 'invalid_request_error', code: 'synthetic-provider-rejection'}}));
}
http.createServer((request, response) => {
  if (request.method !== 'POST' || request.url !== '/v1/chat/completions') { response.writeHead(404); return response.end(); }
  let body = '';
  request.on('data', chunk => { body += chunk; if (body.length > 2 << 20) request.destroy(new Error('bounded fault inference')); });
  request.on('end', () => {
    try {
      if (++sequence > 12) throw new Error('bounded fault inference allowance exhausted');
      const input = JSON.parse(body), tools = input.tools || [];
      const shell = tools.some(tool => tool.function?.name === 'shell');
      const users = (input.messages || []).filter(message => message.role === 'user');
      // OpenCode appends a synthetic user message when retrying transport.
      // Keep the latest explicit fixture prompt across that synthetic input.
      const text = users.map(message => JSON.stringify(message.content)).join('\n');
      const marker = [...text.matchAll(/FACTORYBOX_PROVIDER_(REJECT|PARTIAL|CONTINUE)_([a-f0-9]{32})/g)].at(-1);
      const session = request.headers['x-opencode-session'];
      const record = {sequence, session, request_sha256: crypto.createHash('sha256').update(body).digest('hex')};
      const rejectedPrompt = marker?.[1] === 'REJECT' ? marker[2] : undefined;
      const partialPrompt = marker?.[1] === 'PARTIAL' ? marker[2] : undefined;
      const continued = marker?.[1] === 'CONTINUE' ? marker[2] : undefined;
      if (shell && rejectedPrompt) {
        observed.push({...record, kind: 'expected-rejection', token: rejectedPrompt}); persist();
        return rejected(response, 'FACTORYBOX_PROVIDER_REJECTED ' + rejectedPrompt);
      }
      if (shell && partialPrompt) {
        const token = partialPrompt;
        if (partials.has(token)) {
          observed.push({...record, kind: 'expected-partial-retry-rejection', token}); persist();
          return rejected(response, 'FACTORYBOX_PROVIDER_PARTIAL_RETRY_REJECTED ' + token);
        }
        partials.add(token);
        const event = {...record, kind: 'expected-partial', token, flushed: false};
        observed.push(event); persist();
        const command = '/usr/local/bin/node /opt/factorybox/opencode-flow/partial-effect.cjs ' + token;
        const chunk = {id: 'chatcmpl-partial-' + sequence, object: 'chat.completion.chunk', created: 0, model: 'probe',
          choices: [{index: 0, delta: {role: 'assistant', tool_calls: [{index: 0, id: 'call_factorybox_partial', type: 'function',
            function: {name: 'shell', arguments: JSON.stringify({command, timeout: 0}).slice(0, -1)}}]}, finish_reason: null}]};
        response.writeHead(200, {'content-type': 'text/event-stream', connection: 'close'});
        response.write('data: ' + JSON.stringify(chunk) + '\n\n', () => {
          event.flushed = true; persist();
          // Neither a complete JSON argument nor a finish event is delivered.
          setTimeout(() => response.destroy(), 30);
        });
        return;
      }
      const content = continued ? 'FACTORYBOX_PROVIDER_ACK_' + continued : 'Factorybox auxiliary fixture';
      observed.push({...record, kind: continued ? 'continuation' : 'auxiliary', token: continued}); persist();
      const common = {id: 'chatcmpl-fault-' + sequence, object: 'chat.completion.chunk', created: 0, model: 'probe'};
      if (input.stream) {
        response.writeHead(200, {'content-type': 'text/event-stream', connection: 'close'});
        response.write('data: ' + JSON.stringify({...common, choices: [{index: 0, delta: {role: 'assistant', content}, finish_reason: null}]}) + '\n\n');
        response.write('data: ' + JSON.stringify({...common, choices: [{index: 0, delta: {}, finish_reason: 'stop'}]}) + '\n\n');
        response.end('data: [DONE]\n\n');
      } else {
        response.writeHead(200, {'content-type': 'application/json', connection: 'close'});
        response.end(JSON.stringify({...common, object: 'chat.completion', choices: [{index: 0, message: {role: 'assistant', content}, finish_reason: 'stop'}]}));
      }
    } catch (error) {
      observed.push({sequence, kind: 'unexpected-failure', error: String(error)}); persist();
      rejected(response, String(error));
    }
  });
}).listen(4097, '127.0.0.1', () => { persist(); console.log('FACTORYBOX_FAULT_MODEL_READY'); });
