---
status: proposed
---

# Bound persistent storage with a fixed-size filesystem

The production candidate will use a private, fixed-size filesystem image for its
persistent store. The isolated kernel and actual Sysbox-guest observations prove
that a plain Btrfs leaf qgroup does not bound additional guest-created subvolumes
on directly supplied Btrfs directories. A physically bounded filesystem provides
an aggregate capacity without depending on each guest's subvolume membership.
This proposal is not a selected or qualified runtime path.

The current prototype uses existing ext4 and loop support in the pinned worker
image and host kernel. Its 96-MiB native proof reaches actual ENOSPC, refuses a
second 4096-byte write at zero available bytes, writes a rollback receipt after
releasing an owned reserve, retains old data across clean remount and verifies
release of its exact autoclear loop association. The first failed test and its
ordered correction remain recorded in `docs/production-progress.md`.

The integration must keep store capacity separate from the 12-GiB host free-space
floor. An existing preflight check against the containing filesystem is not a
hard quota. The bounded path needs its own admission floor and recovery reserve,
and new backing allocation must also preserve the host floor. A small bounded
filesystem must not inherit an impossible 12-GiB internal free-space requirement.

The store image, immutable identity and exclusive lease must remain outside the
mounted data filesystem, protected from guests. Startup must verify the exact
backing inode, fixed size, owner, filesystem identity and mount. Unknown or
interrupted initialization must refuse adoption or formatting. A changed mount,
image or lease must refuse operation. Cleanup may release only the exact owned
association; it must never detach an unrelated or newly reused loop device.

All writable paths that are claimed to share the store capacity must reside
inside that filesystem, including managed private Docker data and admitted
writable sandbox backing. External writable administrative inputs are not
silently included in the claim. Full lifecycle, committed-pause recovery after
planned and abrupt worker replacement, actual guest write pressure and negative
identity tests are required before selecting the backend. The image's journal
and allocation overhead, preparation cost, retained memory and failures must be
reported. No existing store is migrated or reinterpreted automatically.

The opt-in worker integration now has a small native Sysbox actor proof of
checkpoint/fork, measured pause/resume and actual guest ENOSPC. Direct loop I/O
is required and its actual kernel flag is rechecked. The host and internal
capacity observations are separate. A preserved failure shows that unlinking
through one overlay can leave a guest alias retaining the unlinked inode and
its blocks; reclamation must be observed rather than inferred from names.
These observations and every correction are pinned in the production progress
record. This decision remains proposed pending worker replacement, full consumer
and required failure/qualification evidence on the integrated profile.
