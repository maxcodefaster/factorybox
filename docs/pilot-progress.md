# Investigation notes and unresolved limits

The local pilot is complete. The [acceptance report](pilot-validation.md)
and [selected profile](../upstream/amd64-pilot-profile.json) define its current
status. This page retains useful investigation evidence and rejected approaches,
without treating intermediate passes as additional qualification.

## Selected optimizations

The frozen v0.1 comparison source is
`factorybox-pilot-baseline-8829afb65616415585502684623e6e5c`, with 508 files and
per-file hashes. The matched comparison uses its actual packaged CLI rather
than recompiling the baseline from current production sources.

Materialization removes one duplicate source payload read. Mount/inode admission
still precedes copying; complete target content, metadata and hardlink checks
must match the saved digest. Initial capture, replay, saved-generation checks
and live pause-backing verification retain full scans.

- `factorybox-filesystem-probe-8337f5fee2d44fefa6bddd6c07c5dcb5` reproduces the
  old two-open behavior; `7cb4e00910314d19875d07b49b845854` passes the new
  one-open regression and original integrity assertions.
- `factorybox-filesystem-probe-d73a217d61a04dbaa444f70713e740b9` intercepts the
  actual copy helper with fanotify. Same-size/same-mtime mutation is rejected
  by full target verification. Its overlapping diagnostic timings are excluded.
- `factorybox-filesystem-probe-1edfe0b278b94e74ac2ff07dd4d875a6` retains all
  five interleaved 64-MiB/4,121-entry observations. Medians are 3.211 s before
  and 3.112 s after. This isolates about 3%, rather than the whole lifecycle gain.

Private Docker Overlay2 needed three ordered CRIU fixes. The original-component
control `factorybox-runtime-6074c8099fc94a9b9498c2aac47f1322` reproduces missing
nested-layer mappings. The patched native overlay suite
`factorybox-runtime-96aeda28ec3048899a3889f965faa65f` passes all four overlay
cases, including refusal of an escaping layer link, without skips. The final
profile adds complete interruption, upstream, reproduction and matched gates.
The original VFS consumer input remains unchanged; copied daemon options select
Overlay2 with both input hashes retained.

Existing immutable-base reuse is already demonstrated for independent fresh
starts. It avoids repeating initial preparation on the same worker. It does
not copy inherited agent RAM into a supposedly fresh sandbox or establish a
shared image store across workers.

## Failures that explain the boundaries

The original filesystem-service startup timeout remains unexplained.
`factorybox-devx-lifecycle-f7c02247159d4e13bf768da0ad1d39c5` waits 60 seconds
for its socket and fails before a sandbox starts. A subsequent standalone probe
with the same filesystem executable passes in
`factorybox-worker-startup-619627561c3548b9bcf4a19e5df1d2e0`.
The complete failed volume/logs remain losslessly recoverable through receipt
`factorybox-storage-repack-db510823028d4a8ba8db62bdeb017b88`.
The selected Go PID-1 worker has bounded startup and fails closed. Its later
successes do not explain that earlier failure's cause.

An early client also created an exec task before PID 1 delegated the private
cgroup root. The worker correctly refused it. The supplied client now reads
the readiness hint through Docker's archive API before executing live
`worker-status`. This fixes that specific race, not the older service timeout.

Real OpenCode's first active-tool checkpoint failed on an overlay-backed
inotify handle in `factorybox-devx-lifecycle-3ed1013b7ccf4e61a999ca5adaa2e554`.
Selected dump/state diagnostics are in
`factorybox-opencode-fsnotify-diagnostic-2a8f569febb84cba94e9890599f7574f`.
The next attempt `factorybox-devx-lifecycle-1bdec0dc333c4c44b0848a70d9216f7f`
searched configured roots but missed the consumer's additional config watch.
The runtime now admits bounded namespace-local inotify fallback roots through
an OCI annotation; the consumer declares all its actual watched paths.
Watchers remain enabled. Actual notifications and agent tool results pass in
every final restored/forked branch. Unlisted watch topologies remain unqualified.

The attached-exec reproducer has an unsupported CRIU session-ID topology.
Its complete failure is retained in
`factorybox-consumer-worker-loss-7922e2c48436428092f33074be0873ce` through
`factorybox-storage-repack-0cc274facfaf4f8aa3cf822276bee30f`.
The first policy rejected
every non-init foreign-parent root, but also rejected the legitimate Factory
service case in
`factorybox-consumer-worker-loss-9ecf06da5baa457caa05d07afc8e9e61`.
That broad policy is rejected. The selected admission allows at most one such
root per PID namespace and refuses multiple roots as `nested-exec-session`
before CRIU. This does not qualify arbitrary interactive sessions.

The final integration batch found a test-only `poll` interrupted by a signal
in `factorybox-runtime-retained-2a76e3266f104a81ae03c64af93801a2`.
Its source test remains unchanged. An ordered fixture patch retries EINTR in
a new copied test against the original absolute 100-ms deadline. Every original
assertion and exact before/after hash is preserved. The acceptance report lists
the passing replacement and other failed preparation/audit invocations.

## Excluded experiments and retention

SHM namespace/startup changes, lazy pages, delta snapshots and read hints are
excluded from the selected runtime. Isolated startup or microbenchmark passes
do not qualify complete consumer lifecycles or performance distributions.
Historical ARM64 runs do not qualify the current AMD64 worker or another host.
The historical experiment stays cold.

Closed failure dumps use verified lossless encodings. Superseded payloads were
retired only after replacement proof and with receipts retaining source,
results, logs and member hashes. Frozen inputs and unresolved reproductions
remain. Completed immutable inputs may share identical files; archived inputs
must never be rewritten in place. Storage guards still require 8 GiB of artifact
headroom, a 32-GiB artifact budget and 12 GiB of host free space.

The full pre-cleanup README, progress diary, obsolete recovery plan and v0.1
goal are preserved with original hashes in
`factorybox-review-4a4eecc0993542998708ec042c6853cf/before/`.
Their dated intermediate statuses are historical. Current deployment and
recovery requirements live in [worker-contract.md](worker-contract.md).
