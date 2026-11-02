---
status: accepted
---

# Observe the original kernel attachment before admission

After abrupt worker loss, the original autoclear loop can outlive the worker's
namespace and keep the image lease held. Unlike orderly exit, this leaves no
accepted namespace-exit receipt. Treating every busy image as retryable would
hide unknown ownership; forcing detach or unlocking it would violate the store
boundary.

While holding the unchanged outer worker lease, admission may remain unready
only for one positively identified original association. The saved image
identity, loop number, offset, size, direct-I/O/autoclear flags and Factorybox
owner label must match. Multiple associations, unknown locks, changed inputs or
additional cleanup failures refuse admission immediately. No mount, new epoch,
adoption, release receipt or readiness is published while observing. Cancellation
ends that observation. Both image ownership and associations must actually
release before normal attachment can proceed.

Keep the existing component Close interval and orderly-exit receipts unchanged.
Native abrupt-loss qualification must hold the original block descriptor
independently, prove the successor stays unready without a mount, release only
that owned descriptor, then complete every original recovery and descendant
assertion on the exact delivered image.

The native ARM64 proof establishes this boundary on its admitted profile.
Other architectures and hosts still require their own exact-image qualification.
