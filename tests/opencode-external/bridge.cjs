// Test egress only. The real pinned OpenCode supplies all client headers.
const http = require('node:http');
http.createServer((request, response) => {
  const chunks = []; let size = 0;
  request.on('data', chunk => {
    size += chunk.length;
    if (size > 2 << 20) request.destroy(new Error('bounded synthetic request'));
    else chunks.push(chunk);
  });
  request.on('end', () => {
    const headers = {...request.headers, 'content-length': size}; delete headers['transfer-encoding'];
    const upstream = http.request({socketPath: '/relay/provider.sock', path: request.url, method: request.method,
      headers, agent: false}, incoming => {
      response.writeHead(incoming.statusCode, {'content-type': incoming.headers['content-type'] || 'application/json'});
      incoming.pipe(response);
    });
    upstream.setTimeout(50000, () => upstream.destroy(new Error('bounded relay timeout')));
    upstream.on('error', () => { if (!response.headersSent) response.writeHead(502); response.end('consumer relay unavailable'); });
    upstream.end(Buffer.concat(chunks));
  });
}).listen(4097, '127.0.0.1');
