---
status: proposed
---

# Keep copy-on-write inside the fixed store boundary

Prepared environments need independent writable filesystem clones without
allocating another complete copy of their tools and dependencies. A plain ext4
store makes the existing reflink-capable copier fall back to full copies.
The candidate format therefore uses Btrfs inside the same physically fixed-size,
privately leased image. Guest-created subvolumes cannot enlarge that backing
file. Directory qgroups are not the aggregate capacity boundary.

Image schema 2 identifies the new format, owner UUID and exact single-device
geometry. Schema 1 continues to mean exactly the existing ext4 format. An
existing image is never reformatted, resized, translated or selected by guessing
its content. Each live CoW mount must additionally prove that its filesystem
UUID and sole backing device belong to the exact owned loop association.
Its virtual filesystem device identity is distinct from the physical loop
device number; saved ownership and recovery must preserve that distinction.

Component checks must retain the original ext4 assertions and independently
prove real kernel reflinks, private writes, actual ENOSPC, rollback after
releasing an owned reserve, remount persistence and exact loop release.
Selecting the format for new workers also requires the normal native lifecycle,
retirement, recovery and full-consumer gates. The component candidate alone does
not qualify a worker or change a retained installation.
