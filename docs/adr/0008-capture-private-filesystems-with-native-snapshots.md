---
status: proposed
---

# Capture private filesystems with native snapshots

The measured ACME Pause spent 93 seconds rereading source and copied file data.
For fresh Sandboxes on the admitted schema-2 store, put the OverlayFS upper and
work directories in one private Btrfs subvolume. Keep Sysbox-managed private
volumes, including Docker data, as separately owned subvolumes at their admitted
manager paths. Capture the complete root set while the source is frozen. Keep
canonical Source outside it. Upper and work cannot be separate subvolumes:
the native copy-up test fails with `EXDEV`. Never snapshot the whole worker
store to obtain one Sandbox's state.

Use an atomic read-only snapshot and independent writable descendants. Bind the
saved state to its filesystem UUID, subvolume UUID, original lineage,
creation/change transactions and enforced read-only property. A descendant can
advance a read-only source's bookkeeping generation without changing its data;
the content-change transaction remains stable. Refuse nested subvolumes before
creation because a Btrfs snapshot would otherwise silently omit their data.
These are native storage proofs, not fabricated content SHA256 values.

This changes where integrity is established. Image import still verifies all
content. Captured private state relies on the admitted checksummed filesystem,
kernel snapshot semantics and a globally read-only saved root, rather than a
fresh full-file hash on every Pause. Worker root remains trusted as in the
existing immutable-base contract. Foreign mounts, changed snapshots, incomplete
creates, write admission and exact-owner retirement remain refusal boundaries.
Native tests independently compare complete source/snapshot bytes and metadata.

Retire an owned sealed snapshot by its verified kernel subvolume ID, never by
making it writable and recursively unlinking its files. Namespace removal and
extent reclamation are separate outcomes. Observe the exact root item's removal
with bounded metadata queries and the caller's context; the kernel's blocking
subvolume-sync ioctl does not provide a cancellable Go wait. Confirm capacity
from filesystem counters rather than treating disappearance as freed bytes.

The snapshot primitive is not yet a production Pause path. Layout, capture,
Resume, Fork, retirement and retained-worker recovery must agree on the new
manifest before selection. Keep the existing qualified copier selected until
the exact candidate image passes those gates and the actual ACME lifecycle
measurement. Existing directories, stores, snapshots and runtime identities are
not converted or rewritten in place.
