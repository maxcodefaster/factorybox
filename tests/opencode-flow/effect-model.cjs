// Require real uncertain output and an independently queried outside receipt.
const fs = require('node:fs');
module.exports = function effectAcknowledgment(root, content, results) {
  const unknown = JSON.parse(fs.readFileSync(root + '/.factorybox-effect-unknown.json'));
  const reconciled = JSON.parse(fs.readFileSync(root + '/.factorybox-effect-reconciled.json'));
  if (results.length !== 1 || results[0].tool_call_id !== 'call_factorybox_held' ||
      !unknown.result_unknown || unknown.submissions !== 1 || unknown.response_status !== 502 ||
      !content.includes('FACTORYBOX_EFFECT_UNCERTAIN_' + unknown.operation) ||
      reconciled.schema !== 1 || reconciled.operation !== unknown.operation || reconciled.applications !== 1 ||
      !/^[a-f0-9]{64}$/.test(reconciled.last_effect_sha256 || '') || reconciled.operator_action !== 'do-not-retry' ||
      reconciled.observer_pid <= 1) throw new Error('agent lacks actual uncertain output and verified outside reconciliation');
  return ' FACTORYBOX_EFFECT_RECONCILED_' + unknown.operation + '_' + reconciled.last_effect_sha256 + ' DO_NOT_RETRY';
};
