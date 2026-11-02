---
status: accepted
---

# Retire failed capture backing after confirmed native Delete

Ordinary Sandbox backing retirement also releases the private images of its last
failed, unpublished capture, but only with the original failed-capture Delete
admission and unchanged guard, writer and source-state evidence. Confirmed Delete
has withdrawn that generation's execution authority; retaining its potentially
multi-gigabyte images forever can exhaust the bounded store and prevent new work.

Keep the original requests, failures, guard evidence, deletion admission and exact
payload inventory. A separate durable journal beside the bounded image permits
recovery when the guest filesystem is full and lets the ordinary command complete
this stage for older overlay-only receipts without rewriting them. Live
references, foreign mounts, changed payloads and successors still refuse removal.
Unknown outcomes and published checkpoints retain their existing protections.
This trades direct inspection of the abandoned image bytes for bounded operation;
it does not authorize deleting a usable pause or discarding diagnostic metadata.

The same withdrawal applies when capture rollback returned the source to useful
work and moved the failed Pause into history before its later Delete. Ordinary
Sandbox retirement checks every such generation against its original source,
safe abort, process completion and retained failure. Separate per-generation
journals bind the exact partial RAM and filesystem inventory. A writer records
the original immutable-lower mountpoint before binding it, so cleanup can
uncover that alias with an idempotent, journaled operation. Older copies without
that receipt must already have no descendant mounts; missing evidence never
authorizes unmounting a foreign path.

For an older worker epoch, explicit retained cleanup requires its validated
same-store attachment, original owner/input, confirmed native Delete, absent
runtime/cgroup and completed original Sandbox-retirement receipt. Only the
capture stage completes. The original receipt and lifecycle record remain
unchanged, and no old runtime or CRIU recipe executes. Registered bases and
canonical Source are not payloads. This avoids either resetting retained data
or leaving multi-gigabyte partial copies permanently charged to the store.
The same retained command also completes an abandoned capture's already
confirmed failed-Delete admission; it does not require that failure to have
returned the source to useful work before Delete.
