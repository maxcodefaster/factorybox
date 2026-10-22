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
