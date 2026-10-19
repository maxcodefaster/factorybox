import { randomUUID } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';

// This nonce and counter live only in RAM. File reads independently exercise
// the selected writable checkout after capture, fork and resume.
const instance = randomUUID();
let counter = 0;

createServer(async (request, response) => {
  try {
    if (request.url === '/health' && request.method === 'GET') {
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ ready: true }));
      return;
    }
    if (request.url !== '/state' || !['GET', 'POST'].includes(request.method)) {
      response.writeHead(404).end();
      return;
    }
    if (request.method === 'POST') {
      let body = '';
      for await (const part of request) {
        body += part;
        if (body.length > 1024) throw new Error('oversized state request');
      }
      const input = JSON.parse(body);
      if (input.increment !== 1) throw new Error('expected one counter increment');
      if (input.marker !== undefined) {
        if (typeof input.marker !== 'string' || !/^[a-z][a-z-]{0,31}$/.test(input.marker)) {
          throw new Error('invalid workspace marker');
        }
        await writeFile('/workspace/marker.txt', input.marker + '\n');
      }
      counter++;
    }
    const dependency = await fetch('http://dependency:8081/health', {
      signal: AbortSignal.timeout(2000),
    });
    if (!dependency.ok || (await dependency.text()).trim() !== 'dependency-ready') {
      throw new Error('private Compose dependency unavailable');
    }
    const marker = (await readFile('/workspace/marker.txt', 'utf8')).trim();
    const hook = (await readFile('/workspace/.devcontainer-hook-ran', 'utf8')).trim();
    response.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' });
    response.end(JSON.stringify({ instance, counter, marker, hook, dependency: 'ready' }));
  } catch (error) {
    response.writeHead(503, { 'content-type': 'application/json' });
    response.end(JSON.stringify({ error: String(error) }));
  }
}).listen(3000, '0.0.0.0');
