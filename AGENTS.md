# Working on Factorybox

- Factorybox is a standalone Linux system-container runtime based on pinned Sysbox and CRIU.
  IDEs, Dev Containers and agent harnesses are consumers, not lifecycle dependencies.
- Leave the Factory repositories and the historical runtime experiment unchanged.
- Keep one implementation writer. Do not use subagents for this programme.
- Use `mise exec --` and `apply_patch`. Keep generated evidence in `artifacts/`.
- Preserve `tests/reference` byte for byte against its import manifest. Extract behavior with its
  regression tests into the runtime; do not rename the proof harness into a production service.
- Preserve upstream test assertions. Failed or unrun required suites block production qualification.
  Cross-compilation is not native platform qualification.
- No KVM, custom kernel, new VM, host runtime installation, global sysctl changes or public
  publishing in this task.
- Native tests own unique containers and volumes. Check exact ownership before cleanup and leave
  unrelated resources alone. Keep the 3 CPU/3 GiB sandbox and 1 CPU/768 MiB separate Theia ceilings,
  420-second lifecycle deadline and maximum two live sandboxes.
- Keep only demonstrated optimizations on the selected path. Record preparation, first useful
  work, retained memory and failures. Do not infer p95 from a handful of observations.
- The initial trust boundary is separate workers per customer or trust domain. Same-worker
  recovery does not imply recovery after worker restart or cross-node migration.
