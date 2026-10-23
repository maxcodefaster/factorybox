// If an incomplete provider call ever executes, leave an actual launch record.
const fs = require('node:fs');
const token = process.argv[2];
if (!/^[a-f0-9]{32}$/.test(token || '')) throw new Error('requires synthetic partial-call token');
fs.appendFileSync('/experiment/partial-tool-launches.jsonl', JSON.stringify({token, pid: process.pid}) + '\n', {mode: 0o600});
console.log('FACTORYBOX_UNEXPECTED_PARTIAL_TOOL_EXECUTION ' + token);
