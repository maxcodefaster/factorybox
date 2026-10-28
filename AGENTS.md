# Working on Factorybox

- Factorybox is a standalone Linux system-container runtime based on pinned Sysbox and CRIU.
  IDEs, Dev Containers and agent harnesses are consumers, not lifecycle dependencies.
- Modify Open Software Factory only for the authorized Factorybox consumer integration.
  Leave other Factory repositories and the historical runtime experiment unchanged.
- Keep one implementation writer. Do not use subagents for this programme.
- Use `mise exec --` and `apply_patch`. Keep generated evidence in `artifacts/`.
- Preserve `tests/reference` byte for byte against its import manifest. Extract behavior with its
  regression tests into the runtime; do not rename the proof harness into a production service.
- Imported overlays named by the reference manifest are also immutable. Apply later fixes as ordered
  patches. Declare any necessary test-fixture compatibility change with exact before/after hashes; preserve
  every assertion and keep vet enabled.
- Preserve upstream test assertions. Failed or unrun required suites block production qualification.
  Cross-compilation is not native platform qualification.
- No KVM, custom kernel, new VM, host runtime installation, global sysctl changes or public
  publishing in this task.
- Native tests own unique containers and volumes. Check exact ownership before cleanup and leave
  unrelated resources alone. Keep the 3 CPU/3 GiB sandbox and 1 CPU/768 MiB separate Theia ceilings,
  420-second lifecycle deadline and maximum two live sandboxes.
- Use `reference.new_artifact` before generating a new build or native run. Generated artifacts
  have a 64-GiB budget, with 8 GiB of headroom required before starting and 12 GiB of host free
  space reserved. Keep frozen inputs and active runs. Compact closed-run dumps only after complete
  decompression verification; preserve source, results, logs and reconstruction receipts.
  Completed artifact inputs may share immutable files. Never rewrite archived inputs in place.
- Keep the historical experiment cold. Its archived generated executables are recoverable from
  the verified cleanup archive; do not recreate them or expand diagnostic archives without a
  concrete test need. Retire superseded large payloads with receipts when their replacement
  proof passes, preserving the current unresolved failure and selected inputs.
- Keep only demonstrated optimizations on the selected path. Record preparation, first useful
  work, retained memory and failures. Do not infer p95 from a handful of observations.
- The initial trust boundary is separate workers per customer or trust domain. Same-worker
  recovery does not imply recovery after worker restart or cross-node migration.
