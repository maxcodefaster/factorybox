---
status: accepted
---

# Confirm storage release after the worker's namespace ends

An orderly PID 1 shutdown and completed storage release are separate outcomes.
On the admitted ARM64 container host, an outer container-manager mount copy can
retain the fixed filesystem after the worker's own unmount and descriptor close.
The exact original loop releases only when that outer namespace is withdrawn.
Forcing detach, extending the refusal interval or reporting release early would
hide the ownership problem.

An otherwise successful, accepted shutdown may record that exact pending
association outside the bounded filesystem before PID 1 exits. The component's
ordinary Close still refuses an unconfirmed release and retains its image lease.
Generic errors, changed identities and additional shutdown failures still fail.
An independent post-exit reconciliation must acquire the original leases, verify
the unchanged image and positively exclude every association to its inode before
recording completion. New admission performs the same reconciliation before any
mount. No phase formats, resizes, detaches or deletes backing.

Component assertions run in an isolated child mount namespace, without changing
their file, ENOSPC, remount, held-descriptor or refusal checks. Delivered-worker
qualification keeps its existing clean-exit assertion and additionally requires
post-exit kernel-release evidence. A pending receipt alone is not a pass. This
composition is verified by the exact delivered ARM64 worker, including a held
external descriptor that must refuse completion and unchanged data through three
epochs. This does not qualify another architecture or replace the full release
matrix.
