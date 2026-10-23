// Supply the actual independently queried receipt through the real tool result.
// This helper neither queries nor mutates the outside system.
const fs = require('node:fs');
module.exports = function reconciledOutput(root) {
  const unknown = JSON.parse(fs.readFileSync(root + '/.factorybox-effect-unknown.json'));
  const receipt = JSON.parse(fs.readFileSync(root + '/.factorybox-effect-reconciled.json'));
  const launches = fs.readFileSync(root + '/.factorybox-effect-launches.jsonl', 'utf8').trim().split('\n').map(JSON.parse);
  if (unknown.schema !== 1 || !unknown.result_unknown || unknown.submissions !== 1 || unknown.response_status !== 502 ||
      !/^[a-f0-9]{64}$/.test(unknown.operation || '') || receipt.schema !== 1 || receipt.operation !== unknown.operation ||
      receipt.applications !== 1 || !/^[a-f0-9]{64}$/.test(receipt.last_effect_sha256 || '') ||
      receipt.operator_action !== 'do-not-retry' || !Number.isSafeInteger(receipt.observer_pid) || receipt.observer_pid <= 1 ||
      launches.length !== 1 || launches[0].operation !== unknown.operation || launches[0].pid <= 1) {
    throw new Error('real tool completion lacks its original uncertainty and exact outside reconciliation');
  }
  return 'FACTORYBOX_EFFECT_RECONCILIATION ' + JSON.stringify(receipt) + '\n' +
    'FACTORYBOX_EFFECT_RECONCILED_' + receipt.operation + '_' + receipt.last_effect_sha256 + ' DO_NOT_RETRY\n';
};
