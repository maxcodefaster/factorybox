# Factorybox

Factorybox is an experimental Linux system-container runtime built around pinned Sysbox and CRIU.
Its Go CLI starts workspaces, captures complete admitted process/filesystem state, creates
independent forks, pauses with snapshot RAM release, resumes and deletes owned runtimes.
It requires no KVM or custom kernel.

There is **no production-qualified release**. Sysbox containers share their worker's Linux kernel.
The initial trust boundary is a separate worker per customer or trust domain. Factorybox does not
provide VM isolation, a Kubernetes control plane or an installer for arbitrary cluster nodes.
IDEs, Dev Containers and agent harnesses are optional consumers.

Development and native tests currently use the existing Mac and OrbStack Linux ARM64 worker.
The runtime executes on Linux. Reference admission requires ARM64, 4096-byte pages and kernel
`7.0.14-orbstack-00380-ga7e0a2dc9535`. AMD64, other kernels and cloud workers remain unqualified.
Matching this tuple alone does not establish runtime installation, filesystem compatibility
or recovery.

## Current qualification

| Selected input | Exact local artifact |
| --- | --- |
| Sysbox | `factorybox-build-sysbox-a62d052944c84818b85a67c5f0a53e30` |
| CRIU | `factorybox-build-criu-182f84bb8fca4746b606fb83fc56f380` |
| Native fixture base | `factorybox-build-native-base-aade0777712c460cbd8bf22156888b6f` |
| Frozen developer rootfs | `factorybox-devx-inputs-03da7afcc972412591e68df3222c40c0` |
| Offline project images | `factorybox-devx-project-images-80f6bd67353a4641adb50b71530d2b1b` |

These names select ignored local evidence, not downloadable releases. A fresh checkout builds
and prepares its own inputs using the commands below. Runners verify supplied identities before use.

| Gate | Evidence and limits |
| --- | --- |
| Portable Go packages and Python tools | Mac Go checks pass; 99 Python tool tests pass |
| Reference integrity | 232 sealed reference files verified; imported overlays remain unchanged |
| Current Sysbox clean rebuild | `factorybox-build-sysbox-526aba43b72d44bc8f0a1769e6a1fe50`: fresh caches, identical runc/fs/mgr binaries and package manifest |
| Current runc module | `factorybox-upstream-e3c049a6660a47a5ad44b00f51236a0d`: 279 main tests, 227 subtests, 14 original upstream skips |
| Current runc integration | `factorybox-upstream-9e3324f1d97e4130a6d6ea6040d62b5a`: 47 passes, twelve original upstream skips |
| Unchanged fs/mgr/IPC/library modules | Retain the combined pass in `factorybox-upstream-eafc65ac3e6f46fcb89b97480cf224d8`; the manager's four shiftfs cases skip on this worker |
| Current CRIU core | `factorybox-criu-core-ea025fce7b7f4939b70aeec382d5bdfe`: 18 unchanged C tests, two dump/restore cycles each, zero skips |
| Prepared-base pause/resume | `factorybox-runtime-cb225f6502304e15ac47e7b8c1410aa7`: nine tests, 46 subtests, zero skips, unchanged frozen inputs/source/components and owned cleanup. Current descriptor admission and managed-volume settings retain the selected CRIU's full pause/resume/RAM-release and resumed-source fork assertions |
| Capture guard failure/disarm | `factorybox-runtime-d1ff147321434c1391dbbc3893b6e90d`: all twelve original cases after retrying interrupted pidfd polls, owned cleanup |
| Worker loss after completed CRIU | `factorybox-runtime-09a48beb73f94b9cb8e0749a548d648e`: nine tests, 43 subtests, zero skips; checkpoint/pause source recovery, copy-child death, live/forged receipt refusal and lost failed-pause publication recovery |
| Filesystem and descriptor admission | `factorybox-runtime-bd020a38178f40a88695427f11d3ab1b`: ten tests/43 subtests, zero skips, selected CRIU. Original hook assertions remain; held root/child mount contexts reject before CRIU with original PID/RAM/useful work and history intact. A transient context settles before a complete checkpoint and independent fork. Changed descriptor observation refused on replay; 128 real contexts produce less than 4 KiB of evidence |
| Detached-file admission regression | The minimal same-inode visible/detached-file differential fails with both selected CRIU and the nested-overlay candidate. `factorybox-file-admission-8b6270e71471451eab9de7a96fb94816` proves that the actual guard scanner recognizes the unsupported detached file while admitting visible files and real memfd objects. Complete selected-component admission suite `factorybox-runtime-1b609b83bced4a129dcb0cde9e5190b7` passes twelve tests/48 subtests, zero skips, unchanged frozen sources/components and owned cleanup, including original RAM/replay/independent-fork assertions. This is safe admission, not persistent detached-file restore support |
| Detached proc directory admission | Baseline `factorybox-runtime-ffe678d6c203485296eeaf68609cae43` enters CRIU with an unsupported foreign proc FD directory and loses its running-source outcome. `factorybox-runtime-3d8a35f3e5f149c3b3a17ff1c7433bfb` passes all eleven tests/45 subtests with selected CRIU, zero skips and owned cleanup. Held unsupported directories reject before CRIU with original identity/RAM and immutable replay; transient ones settle before a complete independent fork |
| Supported detached proc regression | `factorybox-runtime-7c048c13dff14e70bea1c105da877778`: nine tests/42 subtests, zero skips, selected CRIU and owned cleanup. Default/restricted detached proc roots, readable own FD directory and descendant restore remain supported. The test pins its original mount outside the sandbox so a recycled numeric mount ID cannot produce a false separation failure; every original assertion remains |
| Image ownership and durability | `factorybox-runtime-bd020a38178f40a88695427f11d3ab1b`: original eight adapter tests and 40 adapter subtests unchanged, including one complete-directory barrier, failed-barrier refusal and symlink/hardlink/foreign-mount rejection |
| Earlier separate-Theia baseline | `factorybox-devx-lifecycle-e3c84fef88f548a68ed7ecbd49da5a3b`: eight app phases and nine IDE checks, unchanged inputs/global sentinels, full cleanup |
| Last completed developer lifecycle with Theia and OpenCode | `factorybox-devx-lifecycle-33d1823f30604cf0924f6378cc404355`: eight app, nine IDE and eight authenticated OpenCode API checks; 363.96 seconds Go, 394.26 seconds outer; unchanged inputs/global sentinels and full cleanup. Includes worker-loss recovery and copy-stage measurements; precedes the separately native-tested alias timing correction |
| Process-tree candidate developer lifecycle | **Failing** in `factorybox-devx-lifecycle-9e5ae782721743058bd9a18d0b507894`: six app, six IDE and six OpenCode phases completed; pause encountered an open `anon_inode:[fscontext]` mount-transaction descriptor that CRIU cannot dump. Pause remained unresolved. 200.07 seconds Go, 246.86 seconds outer execution, verified failure archive and owned cleanup. This is not a successful performance observation |
| Developer lifecycle after descriptor admission | **Failing** in `factorybox-devx-lifecycle-d7ed29713e7f4feaacfa7e352c909e52`: all eight app/eight OpenCode phases and nine IDE checks completed, including RAM-releasing pause/resume, fork of resumed state and independent IDE edits. Go reached its original 390-second limit during deletion of `after-resume`; the outer 420-second deadline also expired. The separate IDE peer passed and cleaned up; the worker failure archive was verified and owned resources cleaned. This does not qualify the complete lifecycle |
| Candidate developer lifecycle with managed-volume backing | `factorybox-devx-lifecycle-ab5fe3cd0ebf41fe9d262e75e5bb9050`: all eight app/eight OpenCode phases and nine IDE checks, unchanged inputs/global sentinels and owned cleanup passed. Same frozen Go/components/consumer inputs as the preceding timeout; only the worker's existing Sysbox `--disable-inner-image-preload` flag changed. Go took 270.43 seconds and outer execution 291.37 seconds. CRIU remains the unselected process-tree candidate |
| Initial Docker-root fault integration | **Failing** as a complete gate in `factorybox-devx-lifecycle-66939bbac7764ec49f3fde9241d3337a`: root tracer killed after 69,632 actual page-image bytes; authorized cure retained 15 processes/110 threads, original stable process births/app state/running containers and a new successful Docker exec. Seven ordinary app phases then completed. Go reached 390 seconds during the resumed-state fork; outer execution reached 420 seconds. Full failure archive verified and owned cleanup completed. The dedicated recovery regression additionally requires a fresh checkpoint/fork of the cured original; this partial run does not qualify it |
| Docker recapture after cure | **Failing** in `factorybox-devx-lifecycle-1580441aaa3849c884b7054d10142189`: authorized cure of 15 processes/115 threads, unchanged containers/app state, new Docker exec and original-source mutation passed. The fresh checkpoint failed because CRIU could not enumerate a Docker Unix socket. Inputs/global sentinels unchanged; verified failure archive and owned cleanup |
| Docker recovery with corrected client namespace | **Failing** in `factorybox-devx-lifecycle-c26bd35b1a214f3dac2ff1ad591fa548`: the original process-tree candidate refused a changed original thread identity before granting cure. No complete cure result was published. Inputs/global sentinels unchanged; verified failure archive and owned cleanup. Earlier isolated/two-process passes do not qualify this real workload |
| Early-hold Docker attempt before proc admission | **Failing** in `factorybox-devx-lifecycle-30459414a2d941b793e2f7cc7912006f`: the first ordinary checkpoint failed on a directory handle from an unenumerable proc mount, before fault injection. This does not test early-hold recovery. Full failure archive verified and owned cleanup completed; the small proc admission regression reproduces the failure without Docker |
| Docker cure and fresh recapture with proc admission | **Failing** as a complete gate in `factorybox-devx-lifecycle-1c5479188db2407492e30ad2ab6adfed`: authorized cure retained 13 processes/103 threads; new Docker exec, original-source mutation, fresh checkpoint, independently useful current-state fork and divergent branch/source mutation passed. Outer execution timed out at 420 seconds during the final prior-checkpoint fork. The separate 600-second archive attempt also timed out and retained its exact volume; offline reconciliation is recorded separately. No complete gate or latency distribution is qualified |
| Overlay2 consumer diagnostic | **Failing**, unselected, in `factorybox-devx-lifecycle-977034ecb79f4547bf463ce678509fd5`: the actual private Docker daemon used Overlay2; first useful application and checkpoint passed, but the first fork failed while CRIU restored a nested overlay mount with `ENOENT`. Complete 4,363,124,736-byte failure archive verified and owned cleanup passed. The frozen wrapper changes only the consumer's storage-driver flag. This is not a performance improvement or developer qualification |
| Nested-overlay shared-root candidate | `factorybox-runtime-71387dc3a4c544478999c8b735285313`: nine tests/44 subtests, zero skips, unchanged frozen inputs/components and owned cleanup. Plain/direct/relative-layer-link cases preserve independent RAM/files through descendant fork after source deletion, pause/resume and resumed-source fork; an escaping link refuses restore while its source remains useful. CRIU `factorybox-build-criu-retained-ef33bdea879e417f93ba5fb378f603a8` remains unselected. This small fixture does not qualify Docker or select Overlay2 |
| Nested-overlay candidate CRIU core | `factorybox-criu-core-d95764528974401693b8a911fd4288b0`: all 18 unchanged C tests passed two dump/restore cycles each; 1,030 upstream test files remain unchanged; exact cleanup passed. This does not replace the full developer or interrupted-Docker gates |
| Shared-root candidate developer lifecycle | **Failing** in `factorybox-devx-lifecycle-2353068904b44e84a5f526cffa2b02f6`: all eight app, eight OpenCode and nine Theia phases completed, but the unchanged 390-second Go deadline fired while waiting for the final `delete after-resume` process. Outer execution took 418.04 seconds. Frozen inputs/global sysctls and the separate IDE peer passed; all 7,601,127,424 failure-archive bytes verified and owned cleanup passed. This is not a complete developer qualification |
| Delete observer diagnostic | **Failing**, diagnostic only, in `factorybox-devx-lifecycle-63d5dfe38d514510b5e64543547e41f1`: an archived CLI-only stack observer left every original assertion and deadline unchanged. The outer 420-second deadline expired before the descendant fork became useful; no slow-delete stack was collected. Preparation consumed 206.07 seconds. All 5,722,079,232 worker-archive bytes verified. Initial worker cleanup passed, but IDE removal timed out; separate reconciliation now confirms the IDE cleanup. Terminal input/global checks were not completed |
| Overlay2 Docker-abort attempt | **Failing before Docker** in `factorybox-devx-lifecycle-833336aec74042219ccedee937053ab4`: the selected Sysbox-fs again missed the unchanged 60-second readiness deadline while its manager was ready. No application or injected Docker-abort assertion ran. Execution took 89.83 seconds; inputs/global sysctls stayed unchanged, all 1,585,446,912 failure-archive bytes verified and owned cleanup passed. Startup remains unresolved |
| Unix client namespace diagnostic | `factorybox-runtime-3c5218a84cdf47269bf70ba96848c418` reproduced an accepted server socket with the external worker's namespace cookie, absent from the source Unix table, without Docker or CRIU. `factorybox-runtime-15cb7afe89f842e4adfe816e2b2286c6` verified that the corrected test client creates a visible socket with the source listener's cookie. Original actor RAM/useful work and owned cleanup passed. This is a fixture mechanism, not recovery qualification |
| Earlier developer timeout | `factorybox-devx-lifecycle-b444028260b14d709e536b5d519016e4`: seven app/IDE/OpenCode phases; reached the 390-second Go/420-second outer limits. Remains a failed observation. Logs/counters retained; its verified payload was retired after the complete replacement passed |
| Interrupted CRIU process recovery | **Failing** in `factorybox-runtime-3fab270ea5f641369da8fa3ff8a60e0d`: the source remains held after the verified CRIU child is killed; no process-completion or safe-abort receipt is issued. The original pinned cgroup's freezer reads `0` at the actual kill and `1` after the failed worker returns |
| Retained Compel context mechanism | `factorybox-compel-recovery-65c298fc4c094c368c60e626f757a0d4`: unretained negative control and 25 recovery cases, including eight freezer, eight three-thread and eight outstanding-RPC repeats; original RAM/FP marker/descriptors, useful work and owned cleanup. Not integrated into the runtime guard or qualified for real page dumping |
| Real page-dump retained-context diagnostic | `factorybox-runtime-retained-4b8ae9c08b4042afadd36d69d9d6c433`: an unselected CRIU candidate cured the actual killed tracer's source, removed its injected mapping, matched saved leader registers/signal mask and retained all 32 MiB of private RAM; TCP rollback and the previous committed fork still worked. Source stayed frozen, so the required lifecycle assertion remains failed |
| Earlier single-process checkpoint recovery candidate | `factorybox-runtime-retained-bec13cf5f97c4ff3ae08289a8454d48e`: nine tests, 40 subtests, zero skips; actual tracer death, original running PID and complete RAM, TCP and previous fork, immutable failure replay, five changed-proof refusals and twelve physical grant controls |
| Earlier single-process pause recovery candidate | `factorybox-runtime-retained-e6a2a2badf1240cb88ccb35811b49a6a`: nine tests, 40 subtests, zero skips, twelve physical grant controls, retained failed-pause history, failed identity reuse refused, original RAM/TCP/useful work and owned cleanup |
| Two-process checkpoint recovery, parent dump | `factorybox-runtime-retained-aecb5633952e4a949c6143f2e9a24b39`: nine tests, 40 subtests, zero skips; exact parent and child preserved, 32 MiB private RAM each, TCP, earlier independent fork, twelve grant controls and immutable failure replay. Original and cloned children can each work and be reaped by their own parent |
| Two-process checkpoint recovery, child dump | `factorybox-runtime-retained-2dd90dc06c21406e9bdf68712aa42180`: nine tests, 40 subtests, zero skips; same two-process assertions, with CRIU killed at the selected syscall stop after at least 8 MiB of the child's own page image |
| Two-process failed-pause recovery | `factorybox-runtime-retained-5f8309b38f2540e496b1a91b40f8327d` and `factorybox-runtime-retained-5d5f0dfdd6d54d36b934534c9f6313fb`: parent and child dump interruption respectively, nine tests/40 subtests each, complete 64 MiB private RAM, TCP, retained failed history, failed identity reuse refused and twelve grant controls |
| Current candidate single-process regression | `factorybox-runtime-retained-46081a69e2964424a335bc0e1257dc76`: nine tests/40 subtests, original 32-MiB actor, twelve grant controls, immutable failure replay and owned cleanup |
| Original process-tree candidate CRIU core | `factorybox-criu-core-ce41c2e4d82b4da5914a7f08a8c85847`: 18 unchanged C tests, two dump/restore cycles each, zero skips, owned cleanup; process-tree candidate `factorybox-build-criu-retained-e61b26949f3a4fa99171812f8241d0c4` |
| Early-hold candidate CRIU core | `factorybox-criu-core-5c73df9036394dc29da56de54271a992`: 18 unchanged C tests, two dump/restore cycles each, zero skips and owned cleanup for `factorybox-build-criu-retained-866198cfe78e40d3882aeaec0c94235c`. Adds an ordered unselected patch to hold the pinned source group immediately on tracer death before waiting for worker rollback/exit. Thread identities, cure authorization and source-release checks remain strict; failure recovery is not yet qualified |
| Thread-churn recovery regression | Original candidate fails in `factorybox-runtime-retained-dacf7d03a7ae414498aac0a425904437`; early-hold candidate passes in `factorybox-runtime-retained-07d5cf8b05ae4bb6b39f3343319efb92`. Exact same frozen Go context/Sysbox components. All nine tests/40 subtests, complete 32-MiB private RAM, original TCP properties/round trip, previous independent fork, triggered nonleader exit and owned cleanup pass with the early hold. `artifacts/docker-interruption-work/thread-churn-comparison.json` binds the results. This does not qualify the Docker case or all interruption timings |
| Current candidate lifecycle regressions | `factorybox-runtime-b2de68ed2c034956af84484d4b013ec2` and `factorybox-runtime-5096b5a264c042f5953d9fead26ce6da`: prepared-base pause/resume with nine tests/46 subtests and worker-loss recovery with nine tests/43 subtests, zero skips, owned cleanup |
| Daemon startup | `factorybox-devx-lifecycle-41c1d086fa494fd3810d14261ef26499` hit the unchanged 60-second Sysbox-fs readiness deadline before capture; a subsequent minimal worker with the same binaries/capability bounds started successfully in `factorybox-startup-probe-3c2ff51d8adb42e18b70a1efaa9e614c`. The later manager/FS reproducer locates the SHM helper deadlock; its separately tested fix remains unselected |
| Isolated daemon startup repeats | `factorybox-startup-repeat-688b6049d2424070bccb7ab81685140f` and `factorybox-startup-repeat-7cae3a33d0134e2b93337e51b5f04f33`: 32 real Sysbox-fs starts each, normal and `GOGC=1`, original 60-second deadline, owned cleanup. Neither reproduced the earlier startup hang; no manager or active workload was present |
| Manager/filesystem startup reproducer | **Failing**, diagnostic only, in `factorybox-startup-manager-da3ab5045441469f9bfee89ed54b59e0`: seven starts reached readiness in 0.215 to 1.594 seconds; the eighth missed the original 60-second limit. The captured Go stack waits in `linuxUtils.ShmSysctlUserNamespaced()` on its helper, whose sole thread is blocked on a futex inside libc's `setxid_mark_thread`. Same selected daemon bytes, writable worker, actual `--log` flags and capability/resource limits; no application or rootfs unpacking. Inputs unchanged and owned cleanup passed. The helper failure is reproduced; no runtime fix is selected |
| Direct SHM startup regression | Baseline `factorybox-shm-probe-1ff01412903740e6986105ffa124e86d` reports sixteen successful probes, then inconsistently reports missing kernel support. Candidate `factorybox-shm-probe-8d177aa7887445a9baa0c08f9e1825ab` passes all 32 with support enabled, the unchanged five-case upstream distro test and refusal of unmapped/shared-IPC/shared-mount helper invocations. Original global sentinels and parent IPC SHM values remain unchanged; exact cleanup passes |
| Re-executed SHM helper startup candidate | `factorybox-startup-manager-9257f527a89e4b84a88a20c85736a3d7`: all sixteen real manager/FS starts pass within 0.053 to 0.459 seconds using the original 60-second bound. Sysbox-fs `factorybox-build-shm-56a79120673e44e0b6bc9ec2e627c51e` is rebuilt; manager bytes are unchanged. Frozen inputs and owned cleanup pass. This is startup evidence for an unselected candidate, not a fork/resume latency distribution |
| SHM fixture consolidation and rebuild | The repository build command produces byte-identical FS/test executables in `factorybox-build-shm-80622be049604875b974e100dd8ffc0f`; original upstream test files remain unchanged. Repository runners pass another 32 supported probes in `factorybox-shm-probe-8aab46e9889d44e998643d840f2da805` and sixteen actual daemon starts in `factorybox-startup-manager-d6262342364144f4afbbaf8770fc6650`, within 0.073 to 1.156 seconds. Inputs and owned cleanup pass. These repeated fixture checks do not qualify the complete developer/failure lifecycle |
| SHM-candidate Docker recovery | **Failing** as a complete gate in `factorybox-devx-lifecycle-759c03a993434903849e731bf4789352`: real startup, first useful app, prior checkpoint and root-tracer recovery of 13 processes/106 threads pass, including original containers/app state, new Docker exec and source mutation. The subsequent fresh checkpoint fails with `Can't lookup mount=83 for fd=18 path=/runc`; current-state and prior-state forks are not reached. Go takes 227.48 seconds and outer execution 269.12. Inputs/global sentinels remain unchanged; all 3,807,379,456 failure-archive bytes verify and owned cleanup passes. The unselected composition changes only FS; runc/manager bytes remain pinned |
| Docker fault attempt with file admission | **Failing**, unqualified, in `factorybox-devx-lifecycle-c1f6664e9c3947c28f1607e38e50ca58`: initial rootfs/base preparation consumes 191.16 seconds, and first useful app takes another individually observed 75.645 seconds. The actual tracer never reaches the required root page-image boundary within the unchanged 30-second tracing limit; no complete injected-fault/recovery proof is published. Go takes 247.32 seconds and outer execution 321.35. Inputs/global sentinels remain unchanged, all 3,605,004,288 failure-archive bytes verify and owned cleanup passes. This missed injection does not qualify the Docker `/runc` recapture fix |
| Native fault-tracer wait | Polling baseline `factorybox-fault-wait-2839c920006f45228a8ae080aca272ce` takes 1.808 seconds for 2,716 observations and fails its separate one-second diagnostic budget. SIGCHLD-based waiting passes in `factorybox-fault-wait-11820cc9c52f4039ad4f5375c2877e4c` at 0.205 seconds. Repository runner `factorybox-fault-wait-2016c1fde8fe48c48263af50803821d2` passes both the counted producer at 0.640 seconds and an absolute deadline with a stale notification at 20.140 ms. Inputs and owned cleanup pass. The Docker injection deadline and page-image assertion remain unchanged; these observations measure test instrumentation, not fork/resume performance |
| Complete candidate Docker-root recovery | `factorybox-devx-lifecycle-8dcd74a0ca0b4019ba45556ac1e46092` passes the complete dedicated gate with the SHM FS composition and shared-root CRIU candidate, using Docker Overlay2. Tracer death after 69,632 actual root page-image bytes is cured with 13 original processes/103 threads. New Docker exec, original-state mutation, fresh checkpoint, independent useful current-state fork, divergent branch/source mutation and a useful prior-checkpoint fork all pass. Go takes 346.66 seconds and outer execution 402.81, within the unchanged 390/420-second bounds. Inputs/global sentinels and exact cleanup pass. Current/prior forks take 22.162/38.834 seconds through useful work; preparation takes 179.608 seconds and cold first use 90.303. Components and storage driver remain unselected pending ordinary developer and remaining failure gates; no latency distribution or production release is qualified |
| Combined candidate developer attempt | **Failing** in `factorybox-devx-lifecycle-b11fca474290494b97cb5a01a14c6ffa`: the same components with Overlay2, separate Theia and OpenCode reach only the first useful app/IDE/API phase before the 420-second outer deadline. Preparation takes 178.435 seconds, cold first use 122.498, first checkpoint 65.207 and the fork CLI 60.355. The fork completes after the outer deadline; no useful-fork, descendant or pause/resume verdict is qualified. All 4,381,999,104 failure-archive bytes verify. Worker cleanup passes; the original result correctly retains failed IDE cleanup. Separate IDE reconciliation `0674564a6fce471c8ea0f858126737b3` verifies its 1-MiB workspace and removes it. Offline verification confirms frozen inputs/components/source/example and absence of all owned resources. Terminal global-sentinel checks did not run |
| Stop failed worker before peer cleanup | Native baseline `factorybox-cleanup-order-0b9189eab46144658983e0aa6c09fb73` observes the real worker still running when peer cleanup begins. `factorybox-cleanup-order-e2726e0bc4d045059034c8f26a530977` passes after moving the ownership-checked stop ahead of peer cleanup. Both tiny failure archives verify and exact cleanup passes. Unit regressions preserve stop/peer/archive order and refuse changed ownership before or after stopping. This is cleanup evidence, not a replacement for the combined developer gate |
| Upstream Systemd `/proc/kcore` assertion | **Failing on the reference worker**; the original assertion is preserved |

Skipped cases are not executed passes. The CRIU selection is not the full ZDTM matrix.
Required failing or unrun gates block a production release.
An owned diagnostic, `factorybox-proc-kcore-ac45c0f06a5e46f5ac4bcc02140c6756`, reproduced the
missing entry in a freshly mounted private Proc filesystem without Sysbox. `/proc/kmsg` and
`/proc/kallsyms` existed. The original Bats case expects `Permission denied` for `cat /proc/kcore`;
this worker returns `No such file or directory`. No dummy Proc entry or assertion replacement
is used to hide that platform difference.

## Runtime and consumer boundaries

| Path | Responsibility |
| --- | --- |
| `cmd/factorybox` | Local administrative CLI and platform diagnostics |
| `internal/lifecycle` | Persisted lifecycle intent, immutable identities, original kernel handles and reconciliation |
| `internal/sysboxcapture` | Pinned capture/restore adapter, complete filesystem verification and private Sysbox state |
| `internal/pageview`, `internal/snapshot` | Extracted logical memory readers and snapshot validation |
| `upstream/lock.json` | Exact source commits, archives, hashes and ordered patches |
| `upstream/ports.json` | Provenance of extracted runtime behavior |
| `upstream/licenses` | Retained component license notices |
| `tests/reference` | Sealed implementation, fixture and assertion reference |
| `examples/minimal/.devcontainer` | Ordinary digest-pinned workspace without IDE or agent requirements |
| `examples/factory` | Compose Dev Container, existing Factory Connector assignment and optional OpenCode launcher |
| `tools` | Bounded build, native qualification and evidence-retention commands |
| `artifacts`, `dist`, `.cache` | Ignored evidence, binaries and prepared source inputs |

The chosen path uses an ordinary complete retained-source CRIU writer and complete writable
filesystem backing. An explicitly imported immutable lower may be shared read-only. Each fork
gets independent writable layers, private manager volumes and runtime identity.

The reference worker starts its pinned manager with `--disable-inner-image-preload`.
This existing Sysbox setting skips stop-time managed-volume sync-out into the outer
system-container rootfs; startup still imports preloaded volume contents. Factorybox
checkpoints independently copy and verify those complete private volumes. Export through
an outer system-container Docker commit/build would omit this sync-out and is not the
Factorybox persistence contract. Ordinary upstream suites retain their original settings.

Factorybox freezes the admitted source while capture runs. It does not need task-completion hooks
from an agent. Capture consistency with arbitrary external writers is unqualified. Dev Containers
describe applications; they do not define the snapshot engine. Factory remains the product
control plane in the composition example.

## Develop on the Mac

```sh
mise install
mise exec -- go test ./cmd/... ./internal/... ./tests/...
mise exec -- python3 -m unittest discover -s tools -p 'test_*.py'
mise exec -- python3 tools/reference.py verify
mise exec -- go build -trimpath -buildvcs=false -o dist/factorybox ./cmd/factorybox
./dist/factorybox version
./dist/factorybox doctor
```

`doctor` returns nonzero on macOS. Host checks do not execute Linux-only fixtures.
Cross-compilation does not qualify another architecture. Native runners currently target the
existing `orbstack` Docker context and own separate containers and volumes. They do not install
a node-wide runtime or change global kernel settings.

## Prepare and build pinned components

```sh
mise exec -- python3 tools/sources.py prepare
mise exec -- python3 tools/sources.py verify --offline
mise exec -- python3 tools/build_native_base.py
mise exec -- python3 tools/build_criu.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
mise exec -- python3 tools/build_sysbox.py
```

Preparation checks source archive hashes, local overlays and zero-fuzz ordered patches. Declared
test-fixture compatibility edits carry exact before/after hashes; original assertions are
preserved. A recipe or cache mismatch fails instead of silently reusing changed sources.

To replace one prepared source after a deliberate recipe change:

```sh
mise exec -- python3 tools/sources.py prepare --component sysbox-runc --refresh
```

The preceding cache is retained under `.cache/retired`. Do not edit prepared or archived sources
in place. Change maintained patches and lock metadata, then prepare a new source tree.

The native builder uses an exact public Envbox input and dated, signature-checked Ubuntu APT
inputs. CRIU has a pinned source epoch and runs its owned-scope C regressions before compilation.
Artifacts record source contexts, toolchains, package manifests and executable hashes.

```sh
mise exec -- python3 tools/build_sysbox.py \
  --clean --compare factorybox-build-sysbox-EXACT_RUN_ID
mise exec -- python3 tools/build_criu.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --compare factorybox-build-criu-EXACT_RUN_ID
```

The current Sysbox comparison passed from empty Go caches. The current CRIU binary also matched
comparison build `factorybox-build-criu-3781b0dbb40f4928a0b52b046ec6f4bd`. These observations cover
the selected native builder, not another platform. The current Factorybox CLI also matches
between the native image/admission gate and the passing frozen developer context.
Factorybox's original-code license must be
chosen before public distribution. No public release or Factory installation is part of the
current qualification.

## Local lifecycle API

The CLI accepts a trusted, administrator-prepared OCI bundle. It runs as worker root with
matching Sysbox manager/filesystem services, a private root-owned state directory and a finite
private cgroup2 limit. It is not an untrusted OCI-spec service.

Inside an appropriately prepared worker:

```sh
fb() {
  factorybox --root /var/lib/factorybox \
    --runtime /opt/factorybox/bin/sysbox-runc --runtime-sha256 EXACT_SYSBOX_SHA256 \
    --criu /opt/factorybox/bin/criu --criu-sha256 EXACT_CRIU_SHA256 \
    --manager-root /var/lib/sysbox/manager "$@"
}

fb start --bundle /work/bundle source
fb state source
fb checkpoint --snapshot-id aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa source
fb fork --source source --snapshot-id aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa branch
fb pause --snapshot-id bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb branch
fb resume --snapshot-id bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb branch
fb delete branch
```

Snapshot IDs are caller-chosen immutable 32-character hexadecimal identities. Forking a fork or
a resumed sandbox uses the same commands. Forks do not depend on a live source registration
after complete backing has been captured.

The state root must be mode 0700 and root-owned. Operations persist intent before external
creation, retain exclusive locks and bind requests to original executable, PID, cgroup, mount and
directory identities. Successful retries reconcile the original operation. An uncertain create,
capture or restore is not silently repeated. Deleted IDs cannot be reused.

The caller owns the original bundle/rootfs. Factorybox does not remove it on deletion, and
rejects its concurrent use by another sandbox. Generation cleanup verifies ownership and
retains source data, history and ambiguous outcomes.

### Immutable prepared base

```sh
fb prepare-base --source /work/base-input developer-base
mount --bind /var/lib/factorybox/bases/developer-base/rootfs /work/bundle/lower
mount -o remount,bind,ro /work/bundle/lower
```

The OCI bundle uses that empty `lower` mountpoint and annotation
`"io.factorybox.base": "developer-base"`. Import completely compares source and copy, records
intent, and protects the retained base with a read-only self-bind. Checkpoints and forks use
checked read-only aliases. Writable trees retain complete content/metadata digests and
independent inodes.

The adapter admits the saved ID-mapped, single-lower Sysbox overlay and recognized private
manager volumes. It verifies contents, UID/GID, mode, mtime, hardlinks, symlinks, device identities
and xattrs, and rejects unowned binds or foreign mounts. Inode identities and ctime need not
remain equal after copying. Executable hooks and arbitrary mount layouts are not admitted.
Base garbage collection and reconstruction after losing the worker mount namespace are not
implemented.
Filesystem configuration admission runs while the original source is safely frozen, before the
guard enters CRIU capture. The worker repeats admission. Known preflight rejection therefore
thaws the original process and retains a failed immutable operation rather than leaving it held.
Namespace and filesystem rejection records remain distinct; replay refuses a changed kind.
This does not authorize thaw after an uncertain CRIU failure.
Descriptor admission also checks readable proc FD-table directories whose mount is absent
from every captured mount namespace. The pinned CRIU's supported canonical own-process
directory remains admitted. Unsupported foreign process/thread references get the same
bounded settling/rejection path as mount transactions. Diagnostics keep at most sixteen
samples; mount inventory is bounded to 8 MiB per namespace and 65,536 rows overall.

### Pause, recovery and failure containment

Pause publishes complete backing before stopping the original source. It records original-cgroup
anonymous memory, syncs file-backed payloads and verifies zero snapshot-payload residency.
RAM-backed pause storage is rejected. Resume retains the original runtime identity and checks
unchanged exclusive working backing before restoring.

Recovery requires the same live worker and retained mount namespace. Worker-restart RAM recovery
and cross-node migration are unsupported. Partial preparation, restore and deletion have
explicit ambiguity checks; complete recovery across every failure boundary remains required
for release.

After CRIU has returned successfully, the worker verifies the frozen, untraced original source,
restores the checked private network state and writes a process-completion receipt before
copying filesystem backing. The receipt binds the source PID/start, worker PID/start, capture
identity, pinned CRIU and original Sysbox state digest. It does not publish a checkpoint.
If the worker then dies or backing fails, both controller and guard require the original worker
to have exited before acknowledging an abort and releasing the source. Failed pause history
is retained; replay can reconcile a lost final status publication without repeating capture
or writing the freezer. Changed receipts or source-state bytes are refused.
GNU cp runs on a retained OS thread with a parent-death signal, so killing the copy worker also
terminates its exact copy child. The native fault gate stops that child before killing its worker
and checks its exit through a pidfd.

The interruption gate kills the verified CRIU child through its pidfd after at least 8 MiB of
image data while it traces the source. No complete checkpoint is published. The preceding
committed checkpoint remains independently usable. Verified network rollback restores only
the admitted private firewall/socket delta, including original TCP cookies, marks and repair modes.

With the selected CRIU this does **not** prove complete process rollback. The source remains held. A read-only native
diagnostic found a residual 5.023-MiB executable `CRIUMFD` mapping. It did not prove every register
or memory mutation safe, and its instrumentation has been removed. There is no manual thaw or
weakened assertion in the required gate. Automatic recovery with the selected CRIU is still
failing and blocks production qualification. The explicitly unselected candidate below adds
a bounded guard-authorized recovery path.

The separate `tests/compel-recovery` mechanism fixture retains the live Compel control block
and transport socket in a C child before killing the original tracer. The child waits for
the exact original tracer's pidfd to report exit and resets its inherited SIGCHLD handler
before taking ownership of new ptrace stops. It reuses Compel's existing cure routine, holds
other threads stopped while curing the parasite leader, and re-freezes before detaching.
The coordinator permits useful work only after that complete cure. This is a test mechanism,
not an authority used by the runtime's guard.

An unretained control leaves an injected mapping while its original C actor remains usable.
The retained-context fixture separately checks the original PID, all 32 MiB of actor RAM,
an ARM64 FP marker, descriptor targets, default unblocked SIGPIPE and removal of the injected
mapping. It covers an ordinary recovery and eight repeats each with a frozen cgroup, three
threads, and a three-thread outstanding RPC. The RPC payload is a dedicated sleep command;
this does not qualify real page dumping, TCP rollback, a multi-process Sysbox workload or
handoff between the capture worker and the independent guard. The required interruption
gate above remains failing.

The unselected `upstream/patches/criu-retained-context-candidate.patch` applies this
mechanism inside the actual CRIU page dump. Version three records the complete living source
tree, with an implementation bound of 512 threads, from the original cgroup mount, including
Sysbox's `init.scope`. Every TID/TGID must match CRIU's captured tree. The record binds each
thread's birth and identifies the process currently being dumped. Native recovery qualification
currently covers one or two multithreaded Go processes; the bound is not a 512-thread qualification.
CRIU creates one retained helper per active process. It replaces the current arm atomically
while its writer is alive and uses a separate log for each process. The final arm remains
immutable after writer death.

The retained child closes inherited RPC descriptors and waits for the exact original tracer
and worker to exit. The original independent guard can then authorize cure
using the capture identity, original controller/guard identities and a random 256-bit nonce.
The C helper accepts one complete canonical grant from a private root-owned file and always
leaves the source frozen after cure. It reattaches every original thread, keeps all other
processes stopped while executing the active process's existing Compel cure, verifies the
unchanged complete tree and rejects any remaining injected mapping in any process.
If both controllers have exited, its older diagnostic
fallback can cure while held, but records `guard_authorized: false`; that result cannot
authorize release. This patch is deliberately absent from `upstream/lock.json`.

The Go guard first verifies the held, untraced original source, dead writers, original live
controller, checked network rollback, unchanged raw Sysbox state and the helper's pinned CRIU
executable, birth and pidfd. It independently verifies the complete thread set, TGIDs and births
before authorizing cure, then verifies them again and checks all process mappings after cure.
After the C cure, it rechecks the original source and publishes a
distinct `capture-process-restored.json`, binding the arm, request, result and network hashes.
It does not publish successful capture completion or a checkpoint. Normal failed-capture
acknowledgement then permits the controller to release the original source. Failed pause
history remains immutable. Replay checks the same evidence and current raw Sysbox state
without running CRIU or writing the freezer.

The real diagnostic passed in `factorybox-runtime-retained-916cb3bea5284fa1b5fde391cb4e1711`
and `factorybox-runtime-retained-d52fc7fa30f64d7099f0647f0c24b958`, then with the tracked
experiment commands in `factorybox-runtime-retained-4b8ae9c08b4042afadd36d69d9d6c433`.
Its independent observer
reads and hashes the actual actor mmap through the original process while it remains held.
The negative control `factorybox-runtime-retained-ccc8bf720abf4312a1b321c6c277b29b` deliberately
changes one byte without refreshing the actor's cached hash; the read-only diagnostic rejects
that stale claim after reading all 32 MiB. Both controls preserve the existing running-source
assertion.

Guard-authorized checkpoint recovery subsequently passed in
`factorybox-runtime-retained-cf454789666b49bbad5509a8a7ea107c`, including changed arm,
request, result, restoration and network proof refusals. Actual CRIU-loss pause recovery passed
in `factorybox-runtime-retained-6284dc056fb64de3a452456aa29fe8b2`, including retained failure
history and refusal to reuse the failed identity. Those runs used the preceding candidate.

The physical grant control `factorybox-runtime-retained-6b21c389a499485480b40290bdea4111`
then found that substring checks accepted a wrong capture token while the original Go guard
was stopped. The source was not released, but the C helper cured under an invalid grant.
The corrected candidate compares one complete canonical record. In
`factorybox-runtime-retained-2b34c0c6880d44be8b3e89ba7fc81df5`, absent and eleven invalid
grants left the exact live helper idle and the source held; a valid original-guard grant
subsequently recovered that same source. The controls cover nonce, controller, guard,
version, token, trailing data, duplicate keys, a partial record, permissions, hardlinks
and symlinks. All 32 MiB of original RAM, original TCP state, useful work and the preceding
independent fork passed.

The first two-process probe, `factorybox-runtime-retained-4f162bc7d61445c5a595dcdd665ba585`,
failed because the preceding candidate intentionally admitted only one process. The initial
tree extension passed with a sleep child in
`factorybox-runtime-retained-a516b5e44b234460b2559ceb4310d236`. A stronger probe then found a
per-process helper log collision in `factorybox-runtime-retained-bfb22a2faf764cc0a393175a18b37151`;
the child helper never armed. Per-process logs fixed that failure.
The two-Go-process proofs above read and hash each original process's actual 32 MiB through
`/proc/PID/mem`, check original PID/birth retention and TCP, and use the previous committed
fork to prove independent child memory and parent-child signal/reap behavior.

The missed parent fault in `factorybox-runtime-retained-108385e5281849aca4fa2109c230e540`
is retained as a failed test observation. The test now traces only its exact owned CRIU,
stops it at a syscall boundary after at least 8 MiB of the selected process's own page image,
then kills and reaps that exact tracer. It does not add a production pause hook or relax the
original page-data/running-source assertions. Real Docker/Dev Container interruption recovery,
other capture windows and worker/guard loss during the cure remain unqualified.

The subsequent full developer run found an additional ordinary failure boundary: a live
mount-transaction `fscontext` descriptor rejected during pause. The operation remained
unresolved after CRIU's normal failure. Its complete failure archive is retained in
`factorybox-devx-lifecycle-9e5ae782721743058bd9a18d0b507894`. Handling or rejecting that
kernel state before capture, with a proved original-source recovery outcome, remains required.

The minimal original failure is retained in
`factorybox-runtime-eec02c0d2df94530ad7377b1d2664987`: an actual open guest `fscontext`
also leaves the source held with the selected CRIU. Descriptor admission now runs before
capture-enter. It inspects frozen source thread file tables, deduplicating only after a
successful kernel `KCMP_FILES` comparison. Unshared tables and comparison failures receive
their own scan. Bounds are 8,192 descriptors per inspected table and 65,536 across a scan.
Observed unsupported descriptor classes receive the bounded settle interval.

When a mount context is present, the controller briefly releases and re-freezes the original
source for at most two seconds of admission time. Namespace and filesystem checks then run
at the final frozen point. A stable workload has no intentional settle delay or extra
descriptor receipt. A context that stays open receives a durable pre-capture rejection;
the independent guard restores the original running source, and a failed pause retains its
history. Replay verifies the original descriptor observation and never runs capture again.
Diagnostic samples are capped at sixteen contexts. This uses no application or agent hook.

The same admission also recognizes regular-file `O_PATH` handles whose original mount ID
is absent from every captured mount namespace. A minimal visible/detached file-clone
differential reproduces the real Docker `/runc` dump error with both selected and candidate
CRIU, without Sysbox or application services. The guard pins each relevant original handle
without reading its data or changing its offset. Anonymous memfd/shmem objects are identified
by their actual kernel device and remain on CRIU's dedicated path. This gives a transient
runtime mount transaction a chance to finish before infection. A persistent detached-file
mount remains unsupported; recognizing it is not a new CRIU restore implementation or a
complete Docker recovery qualification.

The proc-directory transient fixture now queues a close signal only after observing its actual
source cgroup frozen. Its former wall-clock release could finish before admission began,
failing the original assertion that a real blocker was observed. The original assertions and
deadlines remain. `artifacts/runc-mount-work/transient-fixture.json` records exact before/after
test-source hashes. The signal introduces no new captured RPC connection; this is a fixture
synchronization change, not a production quiescence hook.

The first settle probe in `factorybox-runtime-634335460c1d42ebbe38a1bce2a3b76a` found that
an interrupted zero-time pidfd poll was treated as a dead or invalid source. Retrying
`EINTR` fixed it. The complete selected-CRIU admission gate passed in
`factorybox-runtime-bd020a38178f40a88695427f11d3ab1b`. The subsequent full developer
reproduction completed all app, IDE and OpenCode assertions, including the previously failing
pause, but timed out during final deletion. That separate deadline failure remains a release gate.

## Execute native gates

Commands take exact successful local input artifact names. The reference remains immutable:

```sh
mise exec -- python3 tools/reference.py check
mise exec -- python3 tools/reference.py build
mise exec -- python3 tools/reference.py native
mise exec -- python3 tools/native_core.py
mise exec -- python3 tools/native_failure.py

mise exec -- python3 tools/lifecycle_native.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --suite base-pause
```

Lifecycle profiles include `lifecycle`, `guard`, `capture`, `pause`, `base`, `base-pause`,
`delegation`, `namespaces`, `time`, `mountfd`, `overlay`, `admission`, `worker-loss` and the required failing `interruption` gate.
Each runner checks declared assertions, inputs, process outcome and exact owned cleanup.
Small native fixtures do not replace full developer-workload qualification.
New native runs build from a complete frozen Go source context in their own artifact and
check it again afterward. The failed pre-fix proc run additionally retains an exact
reconstruction of all 52 originally recorded source hashes under `source-reconstruction`;
its original binaries/results remain unchanged.
Native test binaries use `-trimpath -buildvcs=false`, like the CLI, so identical frozen
contexts can share executable bytes without embedding each run's temporary source path.

```sh
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite all
mise exec -- python3 tools/upstream_unit.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite runc-integration
mise exec -- python3 tools/upstream_bats.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite lifecycle
mise exec -- python3 tools/criu_core.py \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID

mise exec -- python3 tools/compel_recovery.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

The actual retained-context candidate has separate, explicitly unselected experiment commands:

```sh
mise exec -- python3 tests/criu-retained-candidate/build.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
mise exec -- python3 tests/criu-retained-candidate/native.py \
  --candidate factorybox-build-criu-retained-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --grant-control
mise exec -- python3 tools/criu_core.py \
  --retained-candidate factorybox-build-criu-retained-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
mise exec -- python3 tools/lifecycle_native.py \
  --retained-candidate factorybox-build-criu-retained-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite base-pause
```

The native candidate experiment requires both the original running-source assertions and
its direct RAM/TCP diagnostic. It retains `passed: false` and exits unsuccessfully if either
fails, even when `mechanism_passed: true`. `--pause` exercises failed-pause recovery;
`--grant-control` checks the live helper's grant refusals before valid recovery.
`--multiprocess` adds a second multithreaded Go actor with another 32 MiB of private RAM;
`--interrupt-child` selects the child's own dump rather than the parent's.
`--corruption-control` additionally requires its raw-RAM oracle to go red.
Neither the candidate runners nor their verdicts promote it into the selected lock.
The separate nested-overlay experiment also has reproducible, explicitly unselected inputs:

```sh
mise exec -- python3 tests/criu-retained-candidate/build.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --nested-overlay --nested-overlay-stable-paths --nested-overlay-shared-root
mise exec -- python3 tools/lifecycle_native.py \
  --retained-candidate factorybox-build-criu-retained-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID --suite overlay
```

The ordered patches preserve the earlier rejected candidates. The `overlay` suite requires
plain/direct/relative-link source lifecycles, independent layer and RAM mutation, a descendant
without the original source, pause/resume with snapshot RAM release, and a fork after resume.
An escaping layer link must refuse restore while its original source remains useful; unresolved
creation evidence must remain retained for the outer fixture's exact cleanup. Namespace-root
layer backing and overlapping covering-mount aliases remain unsupported by this candidate.
These diagnostics do not select Docker's storage driver or qualify the complete consumer.

The developer consumer runner also accepts `--retained-candidate` instead of `--criu-build`
to qualify the same explicit candidate with the ordinary Docker/IDE/OpenCode workload.
Use `tests/docker-overlay-candidate/native.py` in place of `tools/devx_lifecycle.py` for the
explicitly unselected Docker Overlay2 consumer. It accepts the same arguments, replaces the
one original `--storage-driver=vfs` setting in the frozen fixture and records the changed
driver in its result. The complete candidate Docker-root proof uses this wrapper.
Add `--suite docker-interruption` for a separate recovery regression against the real
Docker root process's page-dump tracer loss. This suite requires the explicit candidate
and omits optional IDE/harness consumers. It checks authorized cure, original stable process
identities, unchanged containers/app state, a new Docker exec, mutation and a fresh
checkpoint/fork of the cured source, independent branch mutation and a usable prior
checkpoint. The ordinary lifecycle suite retains every pause/resume/descendant assertion.
The root has
a small address space; injection requires its own page image to contain at least one
4096-byte page. This complements the existing 8-MiB actor probes and does not qualify
Docker-child dump interruption or loss of the recovery helper/guard.
The tracer waits for SIGCHLD and reaps only the exact owned PID, with an absolute deadline.
Its small native regression counts actual producer stops and checks an absent event with a
stale notification. It can run without preparing a developer workload:

```sh
mise exec -- python3 tools/fault_trace.py \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

The failed dedicated gate exposed a test client outside the captured network namespace.
The corrected client creates its socket inside that namespace before connecting. A separate
native diagnostic verifies both invisible external-namespace and visible source-namespace
accepted FDs without Docker or CRIU. No socket is skipped or marked external to pass capture.
The next full attempt encountered a changed-thread refusal, so recapture remains unqualified.

New candidate builds apply `criu-retained-early-hold-candidate.patch` after the preserved
original process-tree patch. Frozen older candidates retain their exact patch inputs and
remain loadable. Add `--thread-churn` to the candidate native runner for an owned nonleader
thread exit triggered at the actual tracer stop; every original RAM/TCP/source/fork assertion
remains. This is a timing regression probe, not permission to ignore changed identities.

The outer Systemd Bats suite has separate pinned image acquisition through `tools/bats_images.py`
and options `--suite systemd --images factorybox-bats-images-EXACT_RUN_ID`. It retains the failing
`/proc/kcore` assertion on this worker. Skips and failures are never converted into executed passes.

CRIU core selection lives in `tests/criu-core-cases.json`. It preserves C assertions and
`-Werror`, verifies credentials and original process retirement, and checks every restored test's
own `PASS`. It does not invoke global hugepage configuration.

## Developer composition

`examples/factory` uses ordinary Compose/Dev Container files and the existing Factory Connector
assignment format. Its app has a RAM-only instance nonce/counter, workspace marker, post-create
hook and Compose dependency. There is no Factorybox-specific template format. Installation
authority URLs, identities and credentials require explicit wiring.
The native proof uses a closed test authority inside the captured network. It does not qualify
reconnecting forked Connectors to an external Factory authority or assigning fresh production
credentials to cloned applications.

```sh
mise exec -- python3 tools/devx_inputs.py \
  --factory-artifacts /absolute/path/to/runtime-artifacts.json --pull-missing
mise exec -- python3 tools/devx_project_images.py
mise exec -- python3 tools/devx_compose.py \
  --consumer-inputs factorybox-devx-inputs-EXACT_RUN_ID \
  --project-images factorybox-devx-project-images-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
```

Input JSON supplies exact `build-runtime-host` and `developer-ide` repository digests.
Preparation verifies their platforms/identities and exports a never-started runtime. The full
lifecycle verifies that frozen rootfs archive by size and hash, allowing use after the unused
original image is evicted. Consumers executing images still require their exact cached identity.

The current frozen consumer contains Docker 29.9.0, Compose 5.6.0, Dev Containers CLI 0.89.0,
Node 24.21.0, the real Factory Runtime Connector, OpenCode server 2.0.27 and Bun 1.4.3.
The offline registry serves checked original manifest/blob bytes, preserving digest verification
inside the private Docker daemon.

```sh
mise exec -- python3 tools/devx_lifecycle.py \
  --consumer-inputs factorybox-devx-inputs-EXACT_RUN_ID \
  --project-images factorybox-devx-project-images-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --criu-build factorybox-build-criu-EXACT_RUN_ID \
  --theia-image EXACT_CACHED_IDE_REPOSITORY_DIGEST
```

Omit `--theia-image` for a lifecycle without an IDE. The fixture retains its 3-CPU/3-GiB worker,
separate 1-CPU/768-MiB Theia, maximum two live Sysbox sandboxes, 390-second Go test and 420-second
outer execution deadline. It has no external network or published service ports.

Theia starts once live checkout ownership is prepared, alongside private Docker initialization.
Short-lived helpers clone actual live project mounts in their original namespaces, transfer
detached mount descriptors over a private socket, and bind them into the IDE's pinned mount/root
namespace. They do not edit raw overlay upper directories. The IDE uses the mapped project UID,
a read-only root and no capabilities. Views/handles are released and the IDE is stopped before
final sandbox teardown.

The passing full proof retained one native Theia connection through checkpoint, forks, a fork of
a fork, pause, resume and a fork of the resumed source. Independent final IDE edits were read
back through both real Connectors and Dev Containers. This is a test composition, not a public
workspace projection API or browser-editing qualification.

`examples/factory/opencode.sh` optionally launches the supplied OpenCode 2 bundle. It accepts one
workspace, requires `OPENCODE_PASSWORD`, binds to loopback and keeps its database in that
workspace's `.git` directory unless `OPENCODE_DB` is supplied. `FACTORY_OPENCODE_CONFIG` remains
optional. Model fetching is disabled. The `--opencode` fixture option checks the supplied server's
authenticated native API across lifecycle phases. Add it to the same command to include the
optional harness in the Sysbox workload. The last completed selected-CRIU combined gate passed
in 394.26 seconds; the process-tree candidate with managed-volume sync-out disabled passed in
291.37 seconds. Both include full cleanup. Every phase verified the supplied OpenCode 2.0.27 process's namespace PID/version through
`/api/info`, authenticated session reads and rejection of unauthenticated reads. Probes execute
inside the sandbox's user, PID, mount and network namespaces. Their own HTTP connections consume
complete responses and confirm socket closure before the next checkpoint.

This checks the restored server's liveness, identity and authentication. It does not qualify model
inference, resumed tool execution or agent-turn semantics. External network peers are not forked.
In-flight TCP connections can cause CRIU to reject capture; the fixture does not enable
`--skip-in-flight` or discard connection state to pass.

Additional IDE, desktop or harness applications belong in consumer images and ordinary app
configuration. Their checkpoint compatibility requires native proof. No application is required
by the core lifecycle.

## Performance observations

| Phase | Earlier separate Theia | Earlier Theia and OpenCode | Latest selected Theia and OpenCode |
| --- | ---: | ---: | ---: |
| Frozen rootfs, immutable base and OCI preparation | 42.707 s | 31.885 s | 25.082 s |
| Prepared start through first useful Connector response | 43.300 s | 57.278 s | 48.387 s |
| Source checkpoint preceding the first fork | 25.948 s | 17.374 s | 19.435 s |
| Fork of that checkpoint through first useful app response | 13.439 s | 29.156 s | 23.371 s |
| Branch checkpoint preceding the descendant fork | 10.735 s | 25.995 s | 34.533 s |
| Fork of fork through first useful app response | 14.380 s | 24.037 s | 37.682 s |
| Pause with stopped source and snapshot RAM release | 14.980 s | 15.666 s | 32.464 s |
| Resume through first useful app response | 4.122 s | 8.966 s | 16.739 s |
| Resumed-source checkpoint preceding its fork | 34.678 s | 31.192 s | 21.852 s |
| Fork of resumed source through first useful app response | 28.514 s | 14.070 s | 18.959 s |

All columns are individual shared-worker observations with the selected components. Evidence:
`factorybox-devx-lifecycle-e3c84fef88f548a68ed7ecbd49da5a3b` and
`factorybox-devx-lifecycle-147a342d25c245909ffc9e308334fcbf`, then
`factorybox-devx-lifecycle-33d1823f30604cf0924f6378cc404355`. Outer execution took 318.03,
352.08 and 394.26 seconds; Go lifecycle including teardown took 291.53, 337.22 and 363.96 seconds.
The first column precedes the filesystem-admission and image-durability changes; it is a
historical baseline, not a controlled before/after performance comparison.
The first two precede the worker-loss recovery changes. The latest includes those changes
and stage measurements, and precedes clearing inherited immutable-alias timing fields.
That timing-only correction passed the native base/pause suite separately.

The unselected process-tree candidate with descriptor admission completed every functional
phase in `factorybox-devx-lifecycle-d7ed29713e7f4feaacfa7e352c909e52`, but the complete gate
timed out during final deletion. Its individual observations were checkpoint 32.079 seconds,
first fork through useful work 29.685 seconds, pause 22.730 seconds and resume through useful
work 20.211 seconds. Immutable-base import took 41.746 seconds versus 9.698 in the last complete
selected-component pass. Earlier deletes in the same failed run took 29.922 and 14.741 seconds;
final deletion started with less than sixteen seconds left on the Go deadline. These observations
do not yet distinguish slow deletion from insufficient remaining execution time. No performance
optimization or deadline increase has been selected from this failed run.

The same frozen Go/component/consumer inputs subsequently passed with Sysbox's existing
`--disable-inner-image-preload` setting in
`factorybox-devx-lifecycle-ab5fe3cd0ebf41fe9d262e75e5bb9050`. First deletions of source,
descendant, resumed fork and branch took 0.831, 0.715, 0.694 and 0.549 seconds, respectively,
instead of copying private manager volumes back into the rootfs. Full Go lifecycle took
270.43 seconds, including all assertions and teardown; outer execution took 291.37 seconds.
First useful fork took 22.499 seconds, descendant fork 18.892, pause 28.327, useful resume
8.656 and useful fork of resumed state 31.065. The first checkpoint took 36.504 seconds.
`artifacts/manager-sync-work/comparison.json` binds both results, matching input hashes and
all CLI timings. Background worker load remains uncontrolled, so these are individual
observations; only the removed sync-out work is selected, not a uniform latency improvement.
The CRIU candidate remains unselected.

The separate unselected Overlay2 consumer attempt
`factorybox-devx-lifecycle-977034ecb79f4547bf463ce678509fd5` reached first useful work and
checkpoint, then failed the first fork. Its retained CRIU restore log reports a missing
path while mounting a nested Docker overlay. The CLI's listener-handoff error is the
subsequent symptom. The full failure archive and exact driver-only wrapper are retained;
VFS remains the reference. No Overlay2 speed or footprint claim is selected from this failure.
Its small native reproducer binds exact source/component hashes in
`artifacts/overlay2-work/minimal-comparison.json`. The first mapping candidate restores one
nested overlay, but the extended regression catches unusable `/proc/self/fd/N` option strings
when forking that fork. The first stable-name alias candidate then fails with `EINVAL`.
Kernel-only differential `factorybox-overlay-alias-f3fc70b1856d48a2bd36236803776fd1` reproduces
that failure with separate upper/work bind mounts and passes when both use a shared backing
root. Original layer contents, failed native logs and complete archives remain.
The ordered shared-root candidate subsequently passed the complete small overlay suite in
29.02 seconds of Go execution, including all four cases. This is suite duration, not an
individual fork latency or developer-workload performance claim. The selected CRIU and
VFS reference remain unchanged.
The complete Docker/Theia/OpenCode attempt then reached every functional phase, including
independent IDE edits, but timed out during final deletion. Its first useful fork took
36.378 seconds, descendant fork 20.021, pause 22.317, useful resume 20.700 and resumed-source
fork 19.794. Frozen-rootfs preparation took 25.039 seconds and immutable-base/OCI preparation
73.166 seconds. The original deadlines remain unchanged; the failed gate is retained.

The first checkpoint's private Docker backing had 3,759 entries and 174,119,022 file bytes,
versus 25,100 entries and 1,174,254,496 bytes in the earlier passing VFS observation.
Complete Docker-tree capture/copy/verification took 4.831 seconds versus 21.489.
`artifacts/overlay2-work/consumer-filesystem-comparison.json` binds those outputs.
These runs use different unselected CRIU candidates and uncontrolled shared-worker load.
The 85.17% file-byte reduction is observed; an overall latency improvement is not established.
The next diagnostic did not reproduce the last deletion within its allotted time. Frozen-rootfs
unpacking rose to 95.500 seconds and immutable-base/OCI preparation to 110.569 seconds.
Base source reading/verification took 52.418 seconds, copying 5.795 and target
reading/verification 49.547; those same stages took 4.178, 1.571 and 4.732 seconds in the
earlier complete VFS observation. The source still has 41,386 entries and about 1.54 GB.
This exposes preparation and read/verification variance in addition to the late-deletion
timeout; it does not establish a delete-specific implementation defect or justify weaker
integrity checks. No maintained CLI instrumentation or new runtime selection was introduced.

Three isolated preparation diagnostics use the same frozen CLI and rootfs archive without
Docker, Sysbox daemons, IDE or harness processes. Controls imported the complete base in
163.263 and 77.402 seconds. Discarding only the input tar's clean cache pages after extraction
took 84.954 seconds; that hint is not promoted. Each import preserved all 41,368 entries and
1,457,081,288 file bytes, passed its source/copy/target integrity mechanism and cleaned its
owned resources, but failed the separate 30-second diagnostic budget.
`artifacts/base-prepare-work/comparison.json` binds the inputs and observations. Tree digests
differ: the tar has no root-directory header, so each new extraction has its own root mtime,
which the digest includes. No identical whole-tree hash or application lifecycle pass is claimed.

A frozen diagnostic CLI adds only CPU profiling around the unchanged preparation command.
Run `factorybox-base-prepare-9088035c2c99450ea31e8e0b3c223eb3` imported in 122.076 seconds,
including 47.706 seconds source verification, 15.942 copying and 57.329 target verification.
Its 36.56 sampled CPU seconds include 25.16 in file reads, 3.34 in SHA-256 and less than one
in JSON encoding. File-cache refaults/reclaim increased despite no cgroup memory-limit or OOM
events and a peak below 1 GiB. This identifies file reading as the main sampled CPU cost; it
does not establish why shared-worker preparation varies or justify removing integrity checks.
Earlier larger-buffer and file-cache-hint diagnostics also failed to establish a repeatable gain.
The production scanner, component selection and complete developer deadlines remain unchanged.

The earlier regression `factorybox-devx-lifecycle-b444028260b14d709e536b5d519016e4`
exceeded the unchanged 390-second Go/420-second outer limits. Seven app, IDE and OpenCode phases
completed; the final resumed-source checkpoint was still in progress when the Go deadline fired.
Its prepared start took 77.363 seconds, prepared fork 46.838 seconds, pause 42.688 seconds and
resume 13.544 seconds. These partial observations remain failed despite the later complete pass.
The first checkpoint took 44.567 seconds. Its Docker backing still contained 25,100 entries and
about 1.17 GB, but copying/verifying that tree took 22.071 seconds versus 8.778 in the prior pass.
Worker counters recorded 42,666 swap-in pages during pause versus 447 previously, with no
additional CPU-quota throttling during either measured pause. `io-comparison.json` in the failed
run retains exact timing/counter input hashes. This identifies filesystem work and reclamation
for further measurement; it does not isolate a causal code regression or justify a larger deadline.

Six isolated production-copy diagnostics retain their exact inputs, CPU profiles and result hashes
in `artifacts/filesystem-compare-work/comparison.json`. They reuse the same frozen 25,100-entry
Docker tree under 3 CPU/3 GiB. The previous copy implementation took 27.928 and 11.685 seconds;
the current implementation took 11.994 and 13.245 seconds. A 256-KiB read-buffer candidate took
11.702 seconds and was not promoted. Retaining an additional 384 MiB of anonymous RAM took
9.809 seconds and did not reproduce the live workload's slowdown. These observations do not
establish a causal code regression, a buffer improvement or a full-workload speedup.
Four subsequent interleaved diagnostics in `artifacts/hashparallel-work/comparison.json`
compared the original serial fingerprint with three bounded file-reading/hash workers.
Serial complete copies took 22.500 and 10.019 seconds; parallel copies took 11.749 and
11.382 seconds. All four retained the exact full tree hash and counters and cleaned up.
The first serial run failed the unchanged 20-second diagnostic budget; the other three
passed all 50 checks with zero skips. The second serial control was faster than either
parallel run, so the parallel candidate was not promoted.
Writable-tree capture records separate `source_read_us`, `copy_us` and `target_read_us`
observations. The latest full run's first Docker tree took 4.164 seconds to read/verify the
source, 0.886 seconds to copy and 2.854 seconds to verify the independent target.
Reused verified source digests record zero source-read time for the new copy; immutable aliases
clear their import's historical copy observations. A native test reproduced the stale-alias
measurement before correction in `factorybox-clone-compare-9fbf7a9b01104e4e818187cc167958e4`.
The corrected complete adapter and base/pause suite passed in
`factorybox-runtime-d3be875bda4f47f59b97b678ff9b1b48`. Every integrity check remains.

Preparation precedes prepared start. Fork timings use an already published checkpoint. A fresh
current-state fork must also pay its preceding checkpoint: the latest combined observation's
19.435-second checkpoint plus 23.371-second fork totals about 42.81 seconds. These are component
sums, not a separately instrumented aggregate request. App-response timings precede the
subsequent IDE/OpenCode assertions; those checks have their own observations. Artifacts retain
CPU, I/O and memory samples. External IDE writes are serialized outside capture.

These observations do not establish p95 or isolate improvements from background load.
The sub-three-second p95 target remains unachieved. Selected optimizations are the verified
immutable lower, complete target verification with fresh source-digest reuse, one durability
barrier per filesystem/image generation and a reused digest buffer. Rejected lazy/delta/read-hint experiments
remain historical evidence, not runtime defaults.

The preceding full regression, `factorybox-devx-lifecycle-16442a4d4a5c46c9895d407b6a04487a`, hit
the unchanged 60-second capture deadline after its resumed-source dump. Its retained metadata
shows CRIU finished in 1.06 seconds, filesystem copying/verification took 41.99 seconds and
filesystem durability took 16.53 seconds. Only six of 268 images had their ownership adjusted
before timeout. Image finalization previously forced a separate file barrier for each image.
The current path verifies independent regular files through the pinned directory, rejects
foreign mounts, and uses one complete filesystem barrier plus directory sync. The failed
run remains failed; a later complete pass does not establish a latency distribution.

## Storage and exact cleanup

At admission, new builds/runs require 8 GiB of headroom inside a 32-GiB artifact budget and
12 GiB of additional host free space. Developer runs refuse to start while a preceding volume is retained.
Fixtures share verified immutable component outputs through hardlinks and mount them read-only.
Developer results distinguish `worker_cleanup` from complete `cleanup`; the latter also requires
the optional IDE's cleanup. A retained `-ide-work` volume blocks the next developer run.
On failure, the runner stops and verifies the exact owned worker before closing the IDE peer;
ending the Docker client alone does not stop its container. Archival follows peer cleanup so
external workspace writers are also quiescent.
`reconcile_devx.py --ide` checks the exact derived IDE names and owner before archiving/removing
that workspace. It leaves original failed results unchanged and records its own observation.
For the older delete-observer failure, `theia/reconciliations/5d5e8a5ebbf44164a3ba520a32eb4bdd`
verified all 1,048,576 IDE bytes before removal. Repeat observation
`ba77ba80aff84d5cb8546365b84340c5` verified the archive again without recreating a volume.
The original parent `cleanup: true` described its worker only, while its nested IDE result
correctly retained `cleanup: false`; the parent reporting and admission regressions are fixed.

```sh
mise exec -- python3 tools/artifact_storage.py status
mise exec -- python3 tools/reconcile_devx.py factorybox-devx-lifecycle-EXACT_RUN_ID
mise exec -- python3 tools/reconcile_devx.py factorybox-devx-lifecycle-EXACT_RUN_ID --ide
mise exec -- python3 tools/artifact_storage.py compact \
  /absolute/path/to/artifacts/closed-run/output/capture-source.tar
```

Failed developer volumes stream directly into bounded single-thread Zstandard archives.
Their read-only tar helper has one CPU, 1 GiB RAM and no additional swap. This is offline
failure retention after the native workload stops; it does not raise native resource limits.
Stored bytes are decoded and hashed during capture; the final encoded file must match the
verified stream. Source removal requires complete verification and exact ownership checks.
Timeouts, corruption and capacity failures retain the source and remove incomplete copies.
Other closed-run dumps and unique lifecycle test executables are automatically compacted
after successful cleanup. Shared executable inputs retain their existing hardlinks. Frozen
inputs and active runs remain directly usable.

After the complete Docker-root replacement passed, retirements
`factorybox-storage-retirement-9f9ff32096154300bb82c5286c553a5a` and
`factorybox-storage-retirement-b572b6b16d9b43f986a85fb9213f8c5b` retired the superseded `/runc`
and missed-injection payloads, reclaiming 969,805,733 file bytes. Their source/results/logs,
all member metadata, small contents and large-file hashes remain. The minimal detached-file
reproducers and current unresolved developer archive remain complete. Lossless compaction
of cleaned test executables reclaimed another 619,675,648 allocated bytes, with standard
decoding receipts. The new developer failure occupies 1,440,734,965 encoded bytes.

Reconciliation `b6c1452e4c3f46c498a1b3a8365276f1` verified 21,307,064,320 original bytes against a
7,145,530,085-byte archive in 441 seconds, then removed the original owned developer volume.
Encoding receipts retain hashes and reconstruction commands.

At the user's cleanup request, 18 superseded Factorybox failed-state payloads were retired after
a later successful lifecycle. Source contexts, results, logs and checksum receipts remain.
The initial journal is in `factorybox-storage-cleanup-d18a0d90d81041618cafa478cd84abec/summary.json`.
The later passing Theia lifecycle superseded three additional payloads, recorded in
`factorybox-storage-retirement-e36113b34bf140589aff22417eb44e44`. Not every older encoding receipt
still has a payload. The historical experiment's source and evidence contents remain unchanged.
There is no global Docker pruning or deletion of unrelated installation data.
The obsolete OpenCode API-route probe payload was separately retired after the combined passing
gate, in `factorybox-storage-retirement-495dcaaaad464d51b5a04053858ff3a6`. Its logs remain. The
in-flight TCP capture failure is retained for failure-recovery hardening.

Cleanup `factorybox-storage-cleanup-adeab56542034a96b1e92817d27e98c0` retired three superseded or
incomplete developer archives and moved 1,242 historical generated executables into one
lossless archive. Every archived file was decoded and checked against its original hash and
mode before removal. The manifest retains paths and hardlinks for reconstruction; imported
reference builds, current component inputs and the unresolved capture-failure dump remain usable.
The cleanup reclaimed 7.72 GiB across both directories, including the retained archive.
Retirement `factorybox-storage-retirement-a4f6b95e289b493f8baede3a31497018` later reclaimed
another 5.65 GiB from the pre-receipt worker-loss payload and duplicate startup rootfs archive.
Its successor native fault proof passed. The newer complete developer-failure archive in
`factorybox-devx-lifecycle-b444028260b14d709e536b5d519016e4` was retained until the complete
`factorybox-devx-lifecycle-33d1823f30604cf0924f6378cc404355` replacement passed. Retirement
`factorybox-storage-retirement-d50c4d0ae0d945f2b9c26e0b57dd2b68` then rechecked its encoded
SHA-256 and retired only that superseded 5.72-GiB payload. Startup logs, the unresolved actual-CRIU
interruption evidence, source/results, IO comparisons and encoding/retirement receipts remain.

Retirements `factorybox-storage-retirement-9d9239931312448b9482d7d3eaced250` and
`factorybox-storage-retirement-60f3a729caec445c853d4588b0116bd5` reclaimed another
1,055,227,904 allocated bytes from 21 superseded candidate payloads after the current
one/two-process and lifecycle replacements passed. Each original archive was completely
decoded and verified again. Separate verified compressed indexes retain small-file contents,
all member metadata and hashes of omitted large members. Sources, binaries, results, logs,
original encoding receipts, selected inputs, current proofs and unresolved failures remain.
The new developer `fscontext` failure archive consumes part of the fixed 32-GiB budget;
the 8-GiB next-run reserve is enforced unchanged.

Lossless repacks retain the exact decoded archive bytes while using a larger Zstandard
window. The historical executable archive now lives in
`factorybox-storage-repack-9360baa2972347eaa7ac0f5afcb60b13/historical-executables.tar.zst`;
all 1,242 original file hashes/modes and the complete 6,754,856,960-byte tar stream were
verified before retiring the old encoding. The complete original mount-transaction failure
archive now lives in
`factorybox-storage-repack-25ba22d59d294e59babf32a76d18cd49/failed-volume-0.tar.zst`;
all 13,358,858,240 decoded bytes match its original encoding receipt.
The latest developer-timeout archive now lives in
`factorybox-storage-repack-062dd845c947434d999b9f3ae37fc62f/failed-volume-0.tar.zst`;
all 22,943,891,456 decoded bytes match its original encoding receipt. These three repacks
reclaimed 10,705,268,770 bytes without removing decoded failure or executable data.
The original manifests/results/encoding receipts remain unchanged. Each former archive path
has a `.repacked.json` recovery pointer with the replacement SHA-256 and verified receipt.
Decode these larger-window archives with `zstd -d --long=31`, as recorded in their receipts.

Subsequent failed developer runs retain their complete decoded archives in
`factorybox-storage-repack-00a3855811ff4df4b497d5bb339f38b9`,
`factorybox-storage-repack-551039e5c8ef402a9d0e6b255c3437f6` and
`factorybox-storage-repack-23028118d2c845c390716edf3e18f04a`.
The exact verified raw lengths are 12,149,850,112, 4,763,680,768 and 4,644,143,104 bytes,
respectively. Original receipt paths have recovery pointers; no decoded failure content
was removed. These repacks reclaimed 4,969,001,141 bytes within the same artifact budget.
The pre-admission Docker failure retains all 3,396,337,664 original decoded bytes in
`factorybox-storage-repack-8391fe2c8b544f7d82236d7316194e4b`; its former archive path
has the verified recovery pointer. Identical closed-run CLI binaries also share immutable
files, with full hash/closure checks recorded in
`factorybox-storage-share-16651028400346e5afd2a6c315352269` and the subsequent share receipt.
No executable bytes or failure data were discarded by these shares.
Further identical closed CLI shares are recorded in
`factorybox-storage-share-3a2ed6854dfd44f692864f2a533a36fd` and
`factorybox-storage-share-a181d759322147098597b95305277c57`.
The subsequent `factorybox-storage-share-b2b32ba94006420da9f63fad3f332745` shares 200
identical closed-run executables and reclaims 2,460,667,904 allocated bytes after checking
their declared hashes, modes, ownership and closed-run status.
`factorybox-storage-share-00e6a7f53a7a43efa2d7804d4c3fcff5` shares ten further identical
closed-run executables, reclaiming 62,029,824 allocated bytes.
`factorybox-storage-share-01ed7e28db3e4384a0671063ece70384` subsequently shares twelve
closed executables and reclaims 166,985,728 allocated bytes.
`factorybox-storage-share-8f60e82956d4465ba38bdef2b426cfe1` shares three more identical
closed inputs and reclaims 42,893,312 allocated bytes.

Reconciliation `074130fbd8ee4171bf5210a680c71f7f` completed the retained Docker-cure
failure's cleanup after verifying all 11,050,942,464 raw bytes. Its lossless replacement
in `factorybox-storage-repack-6f1f2ea51d6a4079a338be25ad8cd9ec` occupies 845,743,491 bytes
and reclaimed 2,888,524,443 bytes from the prior encoding. The original receipts remain.
Cleanup verification now resolves one replacement through its original pointer and pinned
repack result, rejects foreign/symlink paths and changed receipts, and checks both the encoded
hash and complete bounded decoded stream. It does not execute commands from receipts or
recreate raw copies. `artifacts/storage-replacement-work/verification.json` records a full
verification of that real replacement; three regressions cover discovery, tampering and a
replacement whose valid encoded hash conceals different decoded bytes.
The complete Overlay2 consumer failure was also losslessly repacked in
`factorybox-storage-repack-b74705a5cbdd41e787d3dc0ed8d0c581`: all 4,363,124,736 decoded bytes
verified, 767,185,252 encoded bytes retained and 667,262,720 bytes reclaimed. Its original path
has a pinned recovery pointer. Neither archive payload was retired.
The later shared-root deadline failure retains all 7,601,127,424 original bytes in
`factorybox-storage-repack-308b63ec36d84b8d9f3a25d575fa9503`, occupying 799,020,194 bytes
after complete verification. This reclaimed 1,534,978,181 bytes from its prior encoding.
The original failed result and encoding receipt remain unchanged.

Source sharing `factorybox-storage-share-65400a46197c43ebbdda3b8decc93f0f` verifies all
fifteen closed candidate source-tree hashes. Its `relative-identity-result.json` additionally
checks that different paths within one context retain distinct inodes; identical files share
only across contexts at the same relative path. Frozen file modes, ownership, mtimes and contents
remain. Further closed executable sharing is recorded in
`factorybox-storage-share-b357d1d83dd6498ea7ff6ef921f2efad`.

Archive `factorybox-storage-executables-44d7b5774c024456bbf4644c4ed32d2a` losslessly retains
74 exclusive executable inode groups from older successful runs absent from this README's
named proofs. The complete decoded contents, hashes, modes, owners, hardlinks and nanosecond
mtimes were verified before replacing those old executable paths with recovery receipts.
The 374,763,056-byte archive reclaimed 633,786,368 allocated bytes. Selected component builds,
named proofs, all failed runs and source/results/logs were not changed. Its `result.json`
contains the exact extraction command; restore only the needed member for a concrete replay.
`restore-verification.json` records an actual single-member extraction on this Mac with matching
contents, ownership, mode and nanosecond mtime. The temporary restored copy was removed.

Retirement `factorybox-storage-retirement-d00fb19e41a04902a08f283db7a5fcd2` removes three
superseded successful payloads, reclaiming about 223 MB after preserving complete member
metadata, small-file contents and large-member hashes. The replacement base/pause proof has
identical test/component bytes and 9/46 checks. The selected worker-loss proof retains all
earlier scenario names, adds forged-completion refusal, and also supersedes an unused older
candidate's identical 9/43 test. Existing source records, executables, results, logs, original encoding receipts,
current selected proofs and every current failure remain.
Retirement `factorybox-storage-retirement-586931eeb9f04b2d8aa7fec6179e0638` reclaims about
913 MB from five resolved component-bootstrap/project-image failures and two resolved
post-CRIU copy-worker-loss failures. Their complete successful successors are respectively
`factorybox-devx-compose-dd27b3f3f6ab461a8e4ec22b3c9c3f23` with the current consumer inputs
and project images, and the selected 9/43 worker-loss proof. Complete member metadata,
small-file contents and large-file hashes remain; current developer/tracer failures and
selected inputs are untouched.

The delete-observer worker archive retains all 5,722,079,232 original bytes in
`factorybox-storage-repack-04f9874f32b0444cad877ab90405c5c9`; its former encoding has a
pinned recovery pointer. This lossless repack reclaimed 1,042,927,425 bytes.

The newer unresolved `/runc` checkpoint failure retains all 3,807,379,456 decoded bytes in
`factorybox-storage-repack-7736fedc94144bceb101431ce4bc81b4`, with a pinned recovery pointer
and complete decompression verification. Its 617,407,152-byte replacement saves 694,729,155 bytes.
Retirement `factorybox-storage-retirement-b49fb02d9d37454f9f8fdf78a918a304` removes only the
superseded pre-application startup payload from `factorybox-devx-lifecycle-833336aec74042219ccedee937053ab4`.
The exact replacement passes 32 SHM probes, sixteen daemon starts and real startup/useful work
in the later Docker attempt. Complete member metadata, small-file contents, large-file hashes,
original sources/binaries/logs and the failing startup/thread observations remain. The current
`/runc` failure is preserved and the complete developer recovery gate is still failed.
This retirement saves 544,839,889 file bytes. Sharing twelve further identical closed-run
executables in `factorybox-storage-share-bff2369ce1064b7ebb6d3b2bf3cfab59` reclaims another
139,640,832 allocated bytes without changing their contents.
The missed-injection attempt retains all 3,605,004,288 raw bytes in
`factorybox-storage-repack-11250c7452be44479c1d68f9c7e0a341`; its fully verified
401,954,986-byte replacement saves 844,136,407 bytes and has the original pinned recovery pointer.
Source sharing `factorybox-storage-share-265bbd5f48f84ddf94c320e6e7528410` verifies all
23 completed Sysbox source manifests before and after sharing 14,206 identical files only
at the same relative paths, preserving modes/owners/mtimes. It reclaims 112,594,944 allocated
bytes without changing within-context source identities between different paths. Closed
diagnostic executable compaction retains exact decoded bytes with standard encoding receipts;
no current failure, frozen component or selected input is discarded.

Retirements `factorybox-storage-retirement-7b454edbd53b4de1ad522314e6f564a5` and
`factorybox-storage-retirement-4f2e21e42c024886a8071d922b2cf527` reclaimed another
367,439,872 allocated bytes from four older successful lifecycle payloads, using later
complete same-component native proofs. Verified small-file contents/member indexes, source,
results, logs and original encoding receipts remain. The latest complete developer timeout
archive remains retained; new runs still require the original 8-GiB reserve.

Retirement `factorybox-storage-retirement-23611e9a3d6d4af49f6ead7cb0f6f54f` reclaimed another
359,542,784 allocated bytes from twelve older successful payloads after checking later passes
with matching components, native test hashes and test counts. Compressed member indexes,
small-file contents, large-member hashes, source, binaries, results and original receipts remain.

## Remaining release requirements

Production qualification requires complete bounded failure recovery beyond the candidate's
one/two-process page-dump proofs, including real Docker/Dev Container trees, recovery on the
selected path, a complete developer pass within the unchanged deadlines after descriptor admission,
and resolution of the observed
daemon-startup hang; resolution of required upstream failures/skips on supported
workers; continued full developer qualification within the existing deadlines after behavioral
changes; and native qualification of every claimed deployment target. The latest complete pass
does not establish a latency distribution. Browser behavior,
concurrent external writers and further workload compatibility need their own evidence.
A deployment/installation contract, worker storage quotas and license review are required
before distribution. The optional OpenCode API composition has native proof; model inference,
agent-turn semantics and resumed tool execution remain outside that proof.

Kubernetes scheduling and worker lifecycle management remain separate integration work.
Factory production integration and public publishing have not been performed.
## Sysbox SHM startup candidate

The selected Sysbox-fs can hang in an empirical SHM namespace check. Its raw-clone child
executes Go and libc setxid code with inherited thread state. The separate
`upstream/patches/sysbox-libs-shm-probe-candidate.patch` replaces only this check with a
three-second self-executed helper. Standard process creation sets the same UID/GID mappings
and private user/IPC/mount namespaces before the new executable starts. Inherited namespace
handles let the helper reject shared namespaces before opening the original three sysctl
files. It never writes their values. Unexpected helper failures remain errors.

The patch adds regressions and leaves every existing upstream assertion unchanged.
`tests/sysbox-shm-candidate` freezes a patch application against a supplied component build,
compiles on that build's retained native builder, runs the complete affected `linuxUtils`
test package including 32 real probes and checks sixteen actual daemon starts. Native builds
and workers retain the 3-CPU/3-GiB ceilings; the startup deadline remains sixty seconds.
The reference source lock and selected binaries do not include this patch.

```sh
mise exec -- python3 tests/sysbox-shm-candidate/build.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --builder-run factorybox-upstream-EXACT_RUN_ID
# Add --baseline to compile the common regression against the unchanged function.
mise exec -- python3 tests/sysbox-shm-candidate/native.py \
  --build factorybox-build-shm-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
mise exec -- python3 tests/sysbox-shm-candidate/startup.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --shm-build factorybox-build-shm-EXACT_RUN_ID \
  --native-base-build factorybox-build-native-base-EXACT_RUN_ID
mise exec -- python3 tests/sysbox-shm-candidate/compose.py \
  --sysbox-build factorybox-build-sysbox-EXACT_RUN_ID \
  --shm-build factorybox-build-shm-EXACT_RUN_ID \
  --regression-run factorybox-shm-probe-EXACT_RUN_ID \
  --startup-run factorybox-startup-manager-EXACT_RUN_ID
```

The optional composition reuses the supplied runc/manager bytes and substitutes only this
FS executable. Its base lock, supplemental patch, per-component inputs and native proof hashes
are recorded separately. This explicitly unselected composition is not a homogeneous full
Sysbox rebuild. Full developer/failure gates and the required upstream suites remain necessary
before choosing new reference components.
