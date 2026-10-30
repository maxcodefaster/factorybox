---
status: accepted
---

# Separate aggregate worker budgets from native fixture ceilings

A Worker contains the administrative services and all Sandboxes in one trust domain.
Its finite CPU and memory ceilings are selected by its consumer before admission.
They must not inherit the 3 CPU / 3 GiB ceiling used to qualify the native fixtures:
that made a shared production worker reject larger budgets and exposed independent
Sandboxes to aggregate memory exhaustion.

The worker still refuses unlimited, zero or malformed cgroup limits. This does not
resize an existing worker, choose consumer scheduling policy or qualify a larger
deployment. Existing native fixtures retain their 3 CPU / 3 GiB limits, separate
1 CPU / 768 MiB Theia, two-live-Sandbox ceiling and lifecycle deadlines. Each newly
selected deployment profile still needs native evidence.
