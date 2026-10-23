// Additional deterministic reply for an explicit prompt after cancellation.
// The ordinary model's successful-result and duplicate-tool assertions remain.
module.exports = function cancellationReply(input, results, sequence) {
  const users = (input.messages || []).filter(message => message.role === 'user');
  const text = JSON.stringify(users.at(-1)?.content);
  const tokens = [...(text || '').matchAll(/FACTORYBOX_CANCEL_CONTINUE_([a-f0-9]{32})/g)];
  if (!tokens.length) return undefined;
  if (tokens.length !== 1 || results.length !== 1 || results[0].tool_call_id !== 'call_factorybox_held') {
    throw new Error('continuation lacks its single original canceled tool result');
  }
  const content = typeof results[0].content === 'string' ? results[0].content : JSON.stringify(results[0].content);
  if (!content.includes('Tool execution interrupted') || content.includes('FACTORYBOX_TOOL_RESULT')) {
    throw new Error('continuation did not ingest the actual cancellation error');
  }
  const token = tokens[0][1];
  return {observation: {sequence, kind: 'cancel-result', token, call_id: results[0].tool_call_id, content},
    delta: {role: 'assistant', content: 'FACTORYBOX_CANCEL_ACK_' + token}, finish: 'stop'};
};
