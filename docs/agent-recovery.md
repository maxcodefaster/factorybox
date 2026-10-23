# Agent recovery and uncertain outside effects

This describes the controlled local release's recovery boundary and its test
procedure. Production qualification is still false. The supported recovery
target is the same host and boot with an intact owned store and a completely
confirmed pause. Customers or trust domains use separate workers.

A checkpoint preserves local processes, files and agent state. It cannot undo
an outside service's accepted request. Independent local forks still share that
outside world. A lost response does not establish whether the effect happened.

Before continuing an uncertain operation:

1. Retain the original operation ID, target, request and observed failure. Keep
   the uncertain job stopped while investigating. Do not resubmit a mutating
   request to find out whether the first one worked.
2. Query the actual outside system by that operation ID or its stable business
   reference. Use a read-only lookup. Check the scope/account and the resulting
   object, not just an HTTP transport status.
3. If the effect is confirmed applied, record that evidence and the decision
   not to repeat it, then let the agent consume the original result together
   with the reconciliation. Use the same decision in every recovered fork.
4. If the result is unresolved, keep the job held for an operator or the
   outside system's own recovery procedure. Missing evidence is insufficient
   to authorize a fresh mutation. A confirmed nonapplication still needs that
   system's documented safe retry policy.

There is no generic `factorybox reconcile` command. The lookup and decision
belong to the application or agent integration that understands the operation.
Factorybox does not supply a general exactly-once guarantee or a universal
external-effect detector.

The deterministic `agent-effect` test performs one synthetic outside mutation
through a separate optional gateway. The host controller fsyncs the effect
before dropping its reply. The held tool receives HTTP 502 and retains the
uncertain outcome, its private RAM and filesystem watch. Both the connection
and gateway exit before the confirmed pause. The outside ledger and controller
are not captured or rolled back with the worker.

After replacement, the unchanged complete Factory consumer test recovers the
pause, checks recovery replay, removes the old saved source files, mutates local
state and creates an independent checkpoint/fork. Before finishing the held
tool in each descendant, the test queries the outside ledger and records
`do-not-retry`. The actual agent must ingest both its saved uncertain output
and the reconciled operation/commit ID. The fork does this after deletion of
its recovered source. The outside count must stay at one through all phases.
The oracle deliberately counts a duplicate application instead of suppressing
it, so a repeated tool execution cannot hide behind deduplication.

The test uses a local deterministic model. Its gateway, ledger and client
wrapper are test fixtures, not installed lifecycle services or application
features. The normal client and all existing native assertions still run.
The separate provider-fault component proof checks that an incomplete streamed
tool call is recorded as not executed, that an inference retry is distinct from
a tool execution, and that explicit new prompts work after a failed inference.
Final package, external model and other release checks remain required.

The separate real-provider cancellation case uses `--probe agent-cancel`, an
explicit matching `--external-provider-proof` and the bounded Factory store.
It interrupts the one actual foreground shell, confirms its process has ended,
and retains the original structured error and actual provider tool-call ID.
After a confirmed pause and worker replacement, it creates an independent
checkpoint/fork. Both descendants must process a different explicit new prompt
in the original session, quote the original error and finish without launching
another tool. The fork continues after its recovered source has been deleted.

The host checks the actual provider requests independently. Each continuation
must contain its saved error as the original tool result, rather than receiving
the error text in the new user prompt. The test retains the complete synthetic
requests and responses. A lost response, extra inference, repeated tool, missing
branch or mismatched call cannot qualify this case. Every optional gateway must
exit before a later capture. The deterministic cancellation path and all its
existing assertions remain separate.

An interrupted process does not establish whether an outside mutation happened.
Use the reconciliation procedure above before considering another mutation.
Cancellation evidence cannot substitute for the separate uncertain-effect test
or the final package's full acceptance.
