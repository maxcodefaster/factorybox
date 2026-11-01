---
status: accepted
---

# Retire failed capture images after their confirmed native Delete

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
