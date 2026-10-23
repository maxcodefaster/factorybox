// Cancellation-only launch counter. The ordinary held-tool fixture is unchanged.
const fs = require('node:fs');
const root = process.argv[2];
if (root !== '/workspace/project') throw new Error('requires exact private cancellation fixture');
fs.appendFileSync(root + '/.factorybox-tool-launches.jsonl', JSON.stringify({pid: process.pid}) + '\n', {mode: 0o600});
require('./held-tool.cjs');
