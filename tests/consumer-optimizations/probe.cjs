// Execute the actual pinned original/patched feature-control functions.
// Provider/cache and Docker are stubs; feature matching and denial stay upstream.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const crypto = require('node:crypto');
const patch = JSON.parse(fs.readFileSync('/input/patch.json'));
function load(path, expected) {
  const source = fs.readFileSync(path, 'utf8');
  assert.equal(crypto.createHash('sha256').update(source).digest('hex'), expected);
  const start = source.indexOf('async function vQ(');
  const end = source.indexOf('async function qG(', start);
  assert.ok(start > 0 && end > start);
  return new Function('state', `
    const uQ = async () => {state.effects.push('fetch-control'); return {disallowedFeatures: [{featureIdPrefix:'ghcr.io/blocked',documentationURL:'https://example.invalid/advisory'}]}};
    const Ht = async () => '/cache';
    const Tr = async () => ({State: {Status: 'running'}, Id: 'owned-fixture'});
    const Oe = async (...args) => state.effects.push(args.slice(1));
    class kA extends Error {constructor(input) {super(input.description); this.data = input.data;}}
    ${source.slice(start, end)}
    return vQ;
  `);
}
const original = load('/input/before.js', patch.before_sha256);
const candidate = load('/input/after.js', patch.after_sha256);
async function run(factory, features, additional, running) {
  const state = {effects: [], error: null};
  try {await factory(state)({cliHost: {}, output: {}}, {features}, additional, running);} catch(error) {state.error = {message: error.message, data: error.data};}
  return state;
}
async function main() {
  const results = [];
  for (const [name, configured, additional, running, empty, blocked] of [
    ['absent', undefined, undefined, undefined, true, false],
    ['empty', {}, {}, undefined, true, false],
    ['allowed-configured', {'ghcr.io/allowed/feature:1': {}}, {}, undefined, false, false],
    ['allowed-additional', {}, {'ghcr.io/allowed/feature:1': {}}, undefined, false, false],
    ['blocked-configured', {'ghcr.io/blocked/feature:1': {}}, {}, undefined, false, true],
    ['blocked-additional', {}, {'ghcr.io/blocked/feature:1': {}}, undefined, false, true],
    ['blocked-running', {'ghcr.io/blocked/feature:1': {}}, {}, 'owned', false, true],
    ['boundary-is-allowed', {'ghcr.io/blocked-other:1': {}}, {}, undefined, false, false],
    ['blocked-union', {'ghcr.io/allowed/feature:1': {}}, {'ghcr.io/blocked/feature:1': {}}, 'owned', false, true],
  ]) {
    const before = await run(original, configured, additional, running);
    const after = await run(candidate, configured, additional, running);
    assert.equal(before.effects[0], 'fetch-control');
    assert.equal(Boolean(before.error), blocked);
    if (empty) {
      assert.deepEqual(after, {effects: [], error: null});
    } else {
      assert.deepEqual(after, before);
      if (blocked && running) {
        assert.deepEqual(after.effects[1], ['stop', '-t', '0', 'owned-fixture']);
        assert.equal(after.error.data.didStopContainer, true);
      }
    }
    results.push({name, before, after, passed: true});
  }
  console.log(JSON.stringify({passed: true, cases: results, scope: 'Actual published feature-control functions; no network or lifecycle proof.'}));
}
main().catch(error => {console.error(error.stack); process.exit(1);});
