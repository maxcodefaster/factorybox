---
status: proposed
---

# Retire saved state through explicit selections and durable ownership

Checkpoint and epoch retirement will be explicit operations with exact saved
identities and durable completion records. Recovery selections must survive
worker replacement and block retirement until explicitly released. This costs
more metadata and coordination than deleting paths by age, but an expired name
cannot establish that a live process, independent descendant or selected pause
has stopped needing its backing.

A committed unresumed pause remains protected. Unfinished captures, imports,
restores and unrecognized entries must block deletion. A completed descendant
can outlive retired ancestor backing only after its private prepared inputs are
verified. Immutable base aliases remain real dependencies even when RAM images
have independent copies. Retirement must retain request identities and original
provenance so retries cannot recreate a retired generation or adopt a successor
that appeared at the same path.

The actual guest pressure proof shows a separate idmapped overlay retaining an
unlinked inode and all its blocks. Retirement will report observed capacity and
remaining references; removing names alone cannot certify reclaimed storage.
An interrupted deletion needs an inventory of exact owned objects and a bounded
retry that refuses changed identities and foreign mounts. This proposal still
requires implementation, native interruption/ownership tests and full consumer
evidence before selection for production.

For an old unresumed pause, a running descendant alone cannot establish a
durable recovery handover. Epoch retirement must name and verify an independent
committed successor pause, including complete private RAM/filesystem backing and
its recovery provenance. A descendant may have performed useful independent
work since recovery. Releasing an old selection is a separate operation and
does not authorize withdrawal of that committed pause.

The accepted retirement operation must keep its successor inputs protected
until the old payload deletion and durable completion are confirmed, including
after interruption. That protection cannot depend only on an in-process lock.
A retry must not interpret vanished mounts as missing data or use the old mount
IDs as current authority. Retained immutable base metadata and full content
need a read-only check against the exact current store attachment before an
owned deletion plan can be prepared. These are retirement admission rules;
successful inspection alone grants no deletion authority.

For the first manual path, retirement maintenance will block lifecycle work
throughout the store until its exact durable completion. This reduces
availability during maintenance, but avoids separate per-checkpoint leases and
their release/recovery ordering. The fence must also apply to explicit
controller CLI options and to a fresh worker using the same store. An old
source tombstone remains after the active store fence is removed.

The internal fence implementation binds an immutable preparation to the actual
private fence inode, full bytes and store image. Replacing an attachment permits
only a separately authorized device/mount-ID translation, preserving original
inode, ownership, modes and hashes. A completion file alone cannot clear the
fence while any inventoried payload path remains. This coordination mechanism
does not itself authorize a complete epoch inventory, resolve dependencies or
prove a recovery handover. The public retirement path must supply those checks
before acceptance; it remains unimplemented and this decision stays proposed.
