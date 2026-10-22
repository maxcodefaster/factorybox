# Factorybox pilot acceptance

Status: the accepted local Factory pilot goal is complete on 2026-10-22.
The [current pilot reference](../upstream/amd64-pilot-profile.json) passes all
44 selected evidence gates, including functional cases, matched performance,
native/upstream checks and independent reproduction. Production qualification
remains false.

Final independent evidence verification is
`factorybox-pilot-qualification-273f02beb1984e8c8299bdefe6ee76cc`.
It binds profile SHA-256
`5bcb332ae0981e2c32bd4a0a57e1902f6f9c5221c965e3c5380b1f0413fb47ff`.
Run `tools/verify_pilot_profile.py` with the retained local inputs to repeat
this verification; it creates a new evidence receipt and changes no archived
result. The Factory repositories and historical experiment remain unchanged.

The accepted scope is [GOAL.md](../GOAL.md). The immutable local v0.1 reference
and its original results remain in [amd64-validation.md](amd64-validation.md)
and `upstream/amd64-profile.json`. [pilot-progress.md](pilot-progress.md)
retains investigation history, rejected approaches and failures.

## Delivered pilot worker

The standalone Go PID-1 worker uses pinned Sysbox and CRIU, with no IDE,
Dev Container, agent harness or proof-test package in its runtime dependencies.
Its local administrative CLI and JSON record/checkpoint schemas are version 1.
The [worker contract](worker-contract.md) describes deployment, readiness,
ownership, retries, persistent storage and same-host recovery. The runnable
[Compose integration example](../examples/worker/README.md) uses those commands.

| Input | Exact local artifact or SHA-256 |
| --- | --- |
| Worker | `factorybox-build-worker-c9a0e13ce158453db409f5076edcea7b` |
| Independent offline rebuild | `factorybox-build-worker-f822f34670104e6c8c7af3f518db4627` |
| Image config ID, identical in both builds | `sha256:e292c1e66d350c6f0a768b59512bac012ecf380b585f0717ecaf0f112f3c335d` |
| Complete runtime/worker package, identical in both builds | `08979176fe269250ef423a18b86cfaa3d9237c24e3164361f31e4ead24f8dce3` |
| Delivered Go CLI/supervisor | `479a692b06c3ff4c2ed867e60b3ada78d5261c2c488401c5d8428dd1f269d6d1` |
| Sysbox | `factorybox-build-sysbox-b86e971efe9b46989e6809996b358f0a` |
| CRIU | `factorybox-build-criu-retained-67076a2ded4549ab9114b28a0f975325` |
| Independent clean CRIU rebuild, identical binary/source/patch identities | `factorybox-build-criu-retained-44e22c219f774997ad876356457ba09f` |
| CRIU binary | `cccbbab851e500e6fc3e5ac63d6be28f376c10630080ba9904b2fdc81bd1f7ee` |
| Independent offline Sysbox rebuild, all three binaries identical | `factorybox-build-sysbox-repro-eb4e293349ee4d7f98306544d99e5b25` |
| Native base | `factorybox-build-native-base-b8dc7327368e4b9bab47bca533070f4a` |

Build results retain every frozen source/toolchain/component identity, ordered
patch, license notice and exact build command. The independently reproduced
USTAR contains the standalone CLI, components, build/component manifests and
license notices; the image uses that same bundle. Frozen build results still
say unselected: qualification is a separate evidence decision, not an edit to
their archived inputs. Optional generated compiler caches were retired with
receipts after validation; rebuilding always uses a fresh cache.
These are offline rebuilds from verified existing toolchains, dependency
images and pinned source inputs. They do not repeat network/APT acquisition
or guarantee future availability of external package repositories.

## Measured lifecycle improvement

The fixed comparison
`factorybox-lifecycle-comparison-b73cf2a287f242c99ee0f8ee6348ff83`
passes all twenty complete native runs: five per configuration and example,
interleaved on this host with identical consumer inputs and resource ceilings.
Its [full local report](../artifacts/factorybox-lifecycle-comparison-b73cf2a287f242c99ee0f8ee6348ff83/benchmark-report.md)
contains all observations, medians, ranges, preparation, first useful work,
IDE timing, retained memory, storage and the documented between-run maintenance
gap. No observations were discarded or reordered.

| Factory phase, median seconds | v0.1 VFS | Candidate Overlay2 |
| --- | ---: | ---: |
| Source checkpoint | 24.158 | 6.875 |
| Checkpoint of a fork | 24.550 | 7.328 |
| Checkpoint after resume | 21.969 | 6.958 |
| Useful fork from ready checkpoint | 18.044 | 5.995 |
| Useful fork of a fork | 19.300 | 6.287 |
| Useful fork after resume | 20.799 | 6.525 |
| Pause | 24.165 | 7.593 |
| Useful resume | 8.613 | 2.834 |
| Prepared fresh start | 33.683 | 33.099 |
| Cold worker + local preparation + useful app | 58.152 | 55.581 |

Every declared Factory checkpoint/pause/fork halving target and every
start/resume/minimal regression limit passes. The minimal example remains
about 2.009 seconds to a useful fork, 1.870 seconds for pause and 1.045 seconds
to useful resume. Its resumed-source checkpoint regresses from 1.783 to 2.100
seconds (+17.7%), within the declared 20% allowance. These are five-observation
medians on one host, not p95 estimates or a stable latency service level.

The selected mechanisms and their measured contributions are distinct:

- A removes one full source payload read in a protected materialization while
  retaining full mount/inode admission, complete copy verification and target
  independence. Actual copy-helper interception rejects changed bytes despite
  unchanged size/mtime. Its isolated microbenchmark is about 3% faster; the
  combined lifecycle comparison does not isolate A's latency contribution.
- B uses private Docker Overlay2 plus the ordered nested-overlay CRIU patches.
  Each descendant retains independent complete mutable data. The measured
  Docker representation is about 335 MB/10,102 entries instead of
  1,362 MB/31,475 entries. This does not establish a global shared-image store,
  lazy pages, delta snapshots or cross-worker deduplication.
- C composes the existing immutable-base API for independent fresh starts.
  One-time preparation is about 21–22 seconds in the functional reuse proofs;
  additional bundle preparation is about 64–67 ms. App initialization remains
  separately charged, about 32 seconds for Factory. No inherited agent/RAM
  state is presented as a fresh start.

Pause proves zero anonymous memory in the stopped source cgroup and zero
resident snapshot-RAM file pages. Whole-worker services, filesystem cache and
metadata still retain memory; the full comparison records that separately.

## Recovery and integration proofs already complete

| Delivered-image case | Local evidence |
| --- | --- |
| Complete ordinary Factory/Theia lifecycle with real running agent/tool | `factorybox-consumer-worker-loss-00169ac3133a4b7fbac7c379b6befa9c` |
| Minimal planned whole-worker replacement | `factorybox-consumer-worker-loss-489e8a575ad846dea3a670d2189dd56d` |
| Factory, separate Theia, real agent/tool, planned whole-worker replacement | `factorybox-consumer-worker-loss-80a765bb06af4beb88b909811a5a78ca` |
| Same, abrupt whole-worker loss after committed pause | `factorybox-consumer-worker-loss-97b8ba1399b84279bdc05f042ad40023` |
| Minimal, loss during an unconfirmed pause | `factorybox-consumer-worker-loss-5250a96123544bce884b7424b201b4f9` |
| Factory/Theia/real tool, loss during an unconfirmed pause | `factorybox-consumer-worker-loss-bd3793325d49471db9b2ec25500a5041` |
| Public recovery and exact replay; corrupt, foreign, stale, partial and duplicate-store rejection; planned and abrupt loss | `factorybox-worker-restart-probe-cbe150aa0a294304a5a77a87291e4146` |
| Unsupported attached exec topology rejected before CRIU with original process/RAM intact | `factorybox-consumer-worker-loss-0caf42c882724c9bafe0bf3b09999ea7` |
| Independent fresh-start base reuse, minimal | `factorybox-consumer-worker-loss-3cd3e1440a174f829d856bba6a857f01` |
| Same, Factory/Theia on independently reproduced image | `factorybox-consumer-worker-loss-51e94fe86ab44ec09cb25291085be860` |
| Supplied Compose file and exact workflow client, worker removal/recreation, recovery, recapture/fork/delete | `factorybox-worker-example-ef2308c7d09b430595d5581a214e9c06` |

The real pinned OpenCode process executes an actual held foreground shell
tool. Each branch independently preserves and changes private RAM/files,
receives real filesystem watch notifications, completes with exit 37 and
feeds its actual result back into the agent. Only the model response is a
deterministic local test peer. Real external inference/provider reconnection
and external side effects remain additional pilot tests.

An incomplete-pause proof holds the actual copy helper through fanotify,
verifies persisted `pausing` intent and absent completed checkpoint, then kills
the complete worker. A fresh worker refuses that incomplete state twice while
still recovering the earlier committed pause, recapturing and independently
forking it. No old process, helper or descriptor survives the replacement.

## Final regression gates

All fourteen runtime native suites pass against the delivered worker's exact
non-test core and pinned components: lifecycle, guard, capture, pause,
immutable base, base pause, delegation, auxiliary controllers, namespaces,
time namespaces, mount descriptors, overlay, admission and worker loss.
The first seven results are in
`factorybox-pilot-native-gates-826f6cb2326645b88e9bd58329367f99`;
that parent then stops at the capacity guard before the next test starts.
The remaining seven pass in
`factorybox-pilot-native-gates-91eb7ce76c7640309936b9c39bb1ce31`.
These are explicitly tagged native test builds; delivered CLI/image proofs
above and in the comparison are separate.

The latter batch also passes CRIU core
`factorybox-criu-core-0769dd1a021d491d8f4c77aa363d48a5`: all eighteen selected
unchanged upstream C cases, two ordinary checkpoint/restore cycles each.
All 1,030 upstream test files retain their original bytes.

The ordinary agent lifecycle and minimal replacement pass in
`factorybox-pilot-integration-gates-5a340ceb260c43409b934807363f7d76`.
That driver then incorrectly expects a positive native result from the
deliberately red raw-RAM control; its original failure stays retained.
The corrected explicit negative-oracle check passes in
`factorybox-pilot-integration-gates-27a21ae46818493490a99210c895902e`.
Its subsequent child-interruption test encounters signal-interrupted `poll`
and reports helper death. The original full failure remains preserved.

The ordered `tests/compatibility/retained-pidfd-eintr.patch` retries EINTR only
in a fresh test copy against its original absolute 100-ms deadline, preserving
all helper, frozen-RAM, mapping and no-release assertions. The source-tree
test and entire reproduced Go build source remain unchanged. Exact patch and
before/after hashes are declared in `tests/pilot-fixture-extensions.json`.
Child, controller, accepted-grant guard and thread-churn interruption cases
then pass in
`factorybox-pilot-integration-gates-753a1a9eff934a9fb3e48ea1252c0549`.
All six ordinary-build Docker fault cases also pass in that batch using the
exact delivered CLI bytes: root and daemon page dumps, failed pause, helper
loss, controller loss and guard loss after its accepted cure grant. They
preserve original state and a useful independent fork from the earlier
checkpoint. Earlier loss windows remain outside this evidence.

The first upstream preparation,
`factorybox-upstream-20fb491ab00e42758ecb612f5ee6ce06`, fails before test start
because the host Docker default bridge is absent. That failed result remains
retained. Repeating the original successful builder's explicit host-network
configuration passes both fresh suites in
`factorybox-pilot-integration-gates-50d113d2dbea48beae5ebc3391e31edf`.
The test containers themselves have no network. No host network configuration
is changed.

| Sysbox gate | Evidence | Coverage and original skips |
| --- | --- | --- |
| Complete unchanged B86 modules, carried forward | `factorybox-upstream-716f9a8228c541ec85bc217921669a6f` | 399 main / 323 subtests; 18 original skips |
| Fresh B86 libcontainer core | `factorybox-upstream-0727661ef6d64862bcb7241aa3e174b0` | 28 main / 30 subtests; no skips |
| Fresh B86 runc integration | `factorybox-upstream-6d037ceae11a417899ec519a1edacd8c` | 47 main; same 12 original skips |
| Unchanged B86 lifecycle Bats, carried forward | `factorybox-bats-43e40ea96667466d9a64e936b0cfd065` | 7 passes; no skips |
| Unchanged B86 systemd Bats, carried forward | `factorybox-bats-61c38bd02f914609b5c31e40c7b750fe` | 4 passes; 2 original skips |

The original skip names/reasons remain in [the v0.1 report](amd64-validation.md)
and each result. They are not waivers or newly executed tests. Fresh core and
integration selection requires the original exact test counts/skip set.
Vet remains enabled. Final portable Go/Python, tagged-probe compilation/vet,
232-file reference, fixture declaration and diff checks pass in
`factorybox-checks-4e30afdbb335400a8fd49392b7f0b90a`.

The final audit reruns all pinned upstream source checks offline: all 1,030
CRIU tests and every declared Sysbox test input match their manifests; existing
fixture compatibility changes remain explicitly declared with their hashes.
The 18 original fixture declarations, five ordered extensions and copied-test
EINTR patch verify against the full frozen v0.1 source manifest. The entire
repository Go context still matches the independently reproduced C9 build.

Two initial audit-tool invocations remain failed in
`factorybox-pilot-qualification-16660415d16143a2a2ea6a1d74bdb7bb` and
`factorybox-pilot-qualification-f43710c6a46449f2b635de53b011097d`.
They respectively resolved benchmark artifact basenames from the wrong
directory and included unrelated consumer Go fixtures in the runtime-source
comparison. The corrected verifier checks the complete runtime source closure,
rejects unexpected runtime files, hashes the actual executed CLI, re-extracts
all twenty complete observations and passes every selected gate in the final
receipt above. No native result, assertion or observation is altered.

Final inspection finds no run-labeled containers or volumes left behind.
Generated evidence occupies 20,950,413,312 allocated bytes (about 19.51 GiB),
within the 32-GiB budget and with the required 8-GiB new-run headroom intact.
Closed-run failure dumps and all documented retention/sharing receipts remain.

## Platform and limits

Only the evidenced Linux AMD64 host tuple is admitted: kernel
`7.2.8-9-omarchy`, 4096-byte pages, cgroup v2 and the tested local Docker/volume
storage path. Worker ceilings are 3 CPU/3 GiB with at most two live sandboxes;
separate Theia has 1 CPU/768 MiB. Each lifecycle has its 420-second deadline.
No KVM, custom kernel, host runtime installation or global sysctl changes are
required or performed.

Use separate workers per customer or trust domain. This is host-kernel
system-container isolation. It supplies its own lifecycle interface; Sysbox
CLI/API compatibility, VM isolation, arbitrary Kubernetes support, cross-node
migration, host-reboot recovery, lost-storage recovery, unsaved-running-RAM
recovery and tail-latency guarantees are not established.

The evidenced service/reconnect path admits at most one foreign-parent non-init
exec root per PID namespace. The known attached topology with multiple such
roots is refused as `nested-exec-session`. Arbitrary interactive TTY/exec
sessions remain unqualified. Earlier controller-loss windows and guard loss
before cure authorization remain outside the retained-context fault proofs.

The historical FS startup timeout remains unresolved and preserved in full.
The selected PID-1 worker bounds startup, refuses failure, and passes the fresh
image runs above; those successes do not explain the old timeout. Original
upstream skips/unrun required assertions, production image reduction, public
distribution/license review and broader deployment tests continue to block a
production-qualified release.
