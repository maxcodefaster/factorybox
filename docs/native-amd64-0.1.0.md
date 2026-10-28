# Native AMD64 verification of 0.1.0

The latest verified image is the October 28
[CRIU exit-status follow-up](#criu-zombie-exit-status-follow-up).

Initial verification: October 27, 2026. All 23 required native functional gates passed on the
ordinary `0.1.0` worker. This verifies the exact AMD64 image below. It does not
qualify ARM64, arbitrary Kubernetes nodes or the overall production release.
The [current release checklist](first-release-status.md) remains authoritative.

## Exact inputs

| Input | Identity |
| --- | --- |
| Factorybox source | `9a17ba720d3b03f0c2fddc785ae2190542fb3807` |
| Original requested source | `955446f1aa9630a9692de86dc8568292bc401647`, retained and separately verified before these fixes |
| Reviewed Factory consumer | `a5b9a931fa2a61e328d122f6ce1250664b52459e` |
| Worker build | `factorybox-build-worker-7e0a025ca2454077a91404e2ca64da53` |
| Worker image ID | `sha256:b70c858c6c8a918f1fca3259cd469c9f53a85d1a4e5be12723dd6e762a689ebf` |
| Factorybox executable SHA-256 | `d7734394fb92d333c3e08a11dbc8e24da2bc3e6b50ac7b940fe69a5ee34e8247` |
| Reconstructable package SHA-256 | `da9b6b511e5a077bfa7a80586b3202ebbe89b267809790ec8eb30e6593a2d80e` |
| CRIU executable SHA-256 | `bde983c1af31bc2f5a4d4cc155db2ad9d3f1834bea5661e613ffc8351c1d3008` |
| Sysbox runc executable SHA-256 | `3c66c0239a512ebd51e4a0aab8737e557d726f5989314bc64af172273aed47e3` |
| Sysbox fs executable SHA-256 | `456cdefe722d1970191824d870e9f85283b34c1cb67d7e82649a9ce5964051be` |
| Sysbox mgr executable SHA-256 | `9e61acac59a3143b6cbd50907274e8873b01901dc34f36e2fab5ea8228c28d07` |
| Factory consumer image ID | `sha256:fcd1f6c493e02c51e979463ec3ffef51d08572f9ece3866904c1000534520aab` |
| Factory connector SHA-256 | `8231bbf05e6ab63c4f13d9e98cabc38b8a76d8cfe71cc6b365ff949b3bdfc7f9` |

The source commit contains the capacity-fixture correction and the image parent
timestamp fix. The former omits unused guest DWARF without changing executable
code, assertions or store reserves. The latter supplies deterministic `/opt`
directory metadata in the build context. It preserves the packaged file bytes
and permissions. Both fixes have regression tests.

The admitted host has kernel `7.2.8-9-omarchy`, 4096-byte pages and cgroup v2 with
the systemd driver. Docker uses its existing local overlayfs/containerd path on
Btrfs. Test stores use bounded ext4 loop images with the existing direct-I/O
requirements. The delivered CLI's doctor matches the admitted tuple. No kernel,
VM, host runtime or global sysctl was installed or changed.

## Results and independent builds

The native receipt is
`artifacts/factorybox-release-native-dcb1e1c8db0a4f4ebf8f05065848bbc6/result.json`,
SHA-256 `1e804b500d42f8095ed7443d4427c041a22e0e6c4752460ca79ddf0187dfdfe9`.
It binds all 23 original child receipts and their cleanup results:

- CRIU core, Sysbox unit and integration gates.
- All seven lifecycle Bats cases. Systemd executes four passes and retains its
  two original upstream skips. No new skip or assertion removal was introduced.
- All fourteen required runtime suites, including capacity, retirement,
  restore holds, stopped memory and timer protection.
- Planned and abrupt worker replacement.
- Both Factory consumer lanes, including the prepared standby pool.

All seven portable gates passed with 409 Python tests, Go checks, vet and
immutable reference verification in
`factorybox-checks-951a0250905a402180136f8cdc17378a`.
[Both portable CI architectures passed on the source commit](https://github.com/maxcodefaster/factorybox/actions/runs/40586576130).
That CI result is separate from native runtime qualification.

Independent no-cache worker build
`factorybox-build-worker-90f6cc09046a4ff2a153c2322fb27276` reproduced the complete
image, all layers, package and executable identities. A cold bounded upstream
build in `factorybox-upstream-cabbc3c927c14bdb91994fc755b2037b` independently
reproduced all three Sysbox executables and the exact package inventory.
CRIU build `factorybox-build-criu-retained-319936da90494b508db61a8a42e0d257`
independently reproduced the selected CRIU executable, prepared source and
ordered patches. These use verified pinned toolchain and dependency inputs.

## Failures and resource handling

The original independent worker build
`factorybox-build-worker-acc4fdca454e480f9f048904de9bc50a` remains failed.
Streamed layer inventories isolated its difference to `/opt` timestamps. The
fixed builds above pass the original byte comparison.

The first `0.1.0` native attempt,
`factorybox-release-native-5eec8633485d4957b47042db23c4dc75`, remains failed.
After four passing gates, the unchanged artifact-headroom guard refused to
start Systemd. It was not a Systemd assertion failure. After verified storage
maintenance, the complete runner executed every gate again.

The successful run uses the original runner and native children without changed
assertions or deadlines. A recorded supervisor stops only the waiting release
launcher after a native child starts. The child runs uninterrupted with its
original timeout and cleanup. After it exits, the supervisor shares only
byte-identical closed inputs and resumes the launcher before the next artifact
admission. Every sharing operation records hashes and file identities. Original
result files remain unchanged.

Only this run's owned bounded builder and state volume were removed. No
containers remain. All nine pre-existing volume identities are preserved, as
are selected images, frozen inputs and failure evidence. Closed diagnostic
archives and the failed build's compiler cache were compacted only after full
stream verification, with reconstruction receipts. Superseded package
transports retain verified reconstruction from their immutable bundles.
No global prune or retained installation cleanup was performed.

## Observations and remaining release work

These are individual acceptance observations, not a latency benchmark. The
source lane took 45.04 seconds to prepare and 15.19 seconds from its Start
request to its first useful response. Its whole worker cgroup held 2,551,717,888
bytes while the assigned sandbox was paused. The reserve lane took 42.03
seconds to prepare; its 45.38-second Start-to-response interval includes the
explicit unused-standby Pause/Resume and new-container checks. Its paused
whole-worker measurement was 2,887,368,704 bytes. Those whole-worker counters
include services, other fixture work and filesystem cache. They are not the
paused sandbox's private RAM. No p95 or speedup claim follows from these runs.

The complete machine-readable closure, source inventory, reproducibility
comparisons, cleanup and maintenance receipts are in
`artifacts/factorybox-stable-release-preparation-860f11f0da1c46deb738873477799273/`.
Its `verification-result.json` binds all child hashes and exact inputs. The
matching-source inventory covers 4,651 entries; no source archive or image was
published.

The current ARM64 matrix still needs a matching existing host. The real-provider
continuation cases remain unrun on this image after a quota refusal. The
historical EBUSY holder remains unidentified. Final profile selection, upstream
review and the exact base-image distribution obligations must be closed before
publication. The 24-hour soak and 100-run series remain deferred. Production
qualification is false; no release was published or installation activated.

## Sysbox-fs poll retry follow-up

The subsequent pre-release upstream review found a listener-lifetime issue.
The selected handler closed its seccomp session on every non-POLLIN event.
Upstream `1ac3fd588495674eb8604f3c79b1573348eb3487` retries a lone transient
POLLERR and preserves terminal handling for hangup and invalid descriptors.
Factorybox backports that change and all six upstream regression cases as an
ordered patch. Exact original/prepared test hashes are declared in the lock;
existing assertions, immutable overlays and vet remain intact.

The controlled old-decision test fails specifically on the transient-event
case. The backport passes all six cases. This is a unit-level decision proof,
not a claim to have reproduced a spontaneous kernel event. The complete native
unit suite subsequently runs these tests too and passes.

| Updated input | Identity |
| --- | --- |
| Factorybox source | `1023be56dd70ed2115b1125c122a4f056b7824f8` |
| Worker build | `factorybox-build-worker-7f9b25ef0b0044aa8b3888e25756aa71` |
| Independent worker build | `factorybox-build-worker-bff3eef9b7ef4aa9a128c52f06f45e10` |
| Image ID | `sha256:b325efac164561af8abd74932b2d0e44633fed613d44c7dba6b574235db34be8` |
| Package SHA-256 | `5c162592cb6c58cb716294c00dd97371c1c90ae201a027af7d2680da3e694c0f` |
| Sysbox-fs SHA-256 | `f1a23c667862785719849fbb1b28e1e541604a0a5309777e69683b16a20173fb` |
| Native matrix | `factorybox-release-native-b6664d353f744f1dbbf6a87049115afa` |
| Native receipt SHA-256 | `1167019e9903dd203b6a9cc3f8f80041dd8c53e03eb9d51c9e94aa75b0dd174e` |

All 23 required native gates pass again on this exact ordinary image, including
both replacement modes and both Factory lanes. Systemd executes four passes
with the same two upstream skips; lifecycle executes seven passes. The CLI,
Sysbox runc/mgr, CRIU, reviewed Factory consumer and project images retain the
identities above. The unchanged CRIU recipe passes the explicit frozen-base
equality check before reuse, and CRIU core is rerun in this matrix.

The clean component build is `factorybox-build-sysbox-320f9daca5994052bdaefaaa5c1a78e3`.
Independent compilation in the complete upstream unit build
`factorybox-upstream-82b3bb6c0c4e47359a9aa20ce6829489` reproduces all three Sysbox
executables and the exact package inventory. Separate no-cache worker builds
reproduce the full image and package. Portable result
`factorybox-checks-e9fa84b1449e4138ba9ccf37bf8efa03` passes all seven gates with
409 Python tests.

Evidence is in
`artifacts/factorybox-seccomp-poll-6e5c1be21f2f49a681b3fb3285fdef67/`.
`verification-result.json` binds every child receipt, build identity and cleanup.
`matching-source-inventory.json` verifies 4,654 preferred-source entries without
publishing an archive. The initial `noexec` fixture attempt remains failed in
`factorybox-seccomp-poll-a198c864cab6459db2a67207877273e3`. The first sharing
attempt refused before replacing any target because its stat check included
read-updated access time; its original script/result are retained under
`failed-sharing-atime/`. The corrected helper preserves every content and
ownership identity check. The initial worker assembly's missing-base-lock
refusal is also retained separately from its successful explicit-lock retry.

Only the owned bounded builder and state volume were removed. All nine retained
volume identities remain unchanged and no containers remain. Storage maintenance
fully verifies every decoded diagnostic archive before replacing its raw copy.
Redundant legacy transports remain byte-for-byte reconstructable from frozen
bundles. Identical completed inputs share files with hashes and identity
receipts; active selected worker inputs are untouched. Original results remain
unchanged.

The source-lane observation is 38.66 seconds of preparation and 13.97 seconds
from Start to its first useful response. Its whole-worker paused measurement is
2,540,175,360 bytes. The reserve lane records 33.94 seconds of preparation and
46.09 seconds to its first useful response, including the deliberate standby
Pause/Resume and new-container checks. Its paused whole-worker counter is
2,957,549,568 bytes. These include services, fixture work and filesystem cache.
They are individual acceptance observations, not private paused RAM, p95 or a
demonstrated speedup.

ARM64, the real-provider cases, historical EBUSY attribution, final profile and
distribution checks remain open in the current checklist. No stable tag,
release, image publication, installation activation or deferred soak was started.

## CRIU zombie exit-status follow-up

Verified October 28, 2026. All 23 required native gates pass again on the
ordinary image below, including planned/abrupt worker replacement and both
Factory consumer lanes. This supersedes neither the historical receipts nor
their retained failures. Overall production and ARM64 qualification remain false.

| Input | Exact identity |
| --- | --- |
| Tested Factorybox source | `0b73944d6a34439d97984dcd8da8a08cbc264ae4` |
| Reviewed Factory consumer | `a5b9a931fa2a61e328d122f6ce1250664b52459e` |
| Ordinary worker | `factorybox-build-worker-d98fbdfb094b4c38b37b7c0dcc2c36d3` |
| Image ID | `sha256:1b8ac1fc16d8467ffd0f80bedd4567e9510453bc0682c5439bced7ff3b878c6b` |
| Package SHA-256 | `88d27ebdbe916ee19c0addc156c8613f3ce774e55d8faefea9aa569234b357c6` |
| Factorybox executable | `d7734394fb92d333c3e08a11dbc8e24da2bc3e6b50ac7b940fe69a5ee34e8247` |
| CRIU executable | `fd8055bf432bf651cdfe00844c9fce65b1ded896e1ac48c118eb9b26ae675818` |
| Sysbox runc executable | `3c66c0239a512ebd51e4a0aab8737e557d726f5989314bc64af172273aed47e3` |
| Sysbox fs executable | `f1a23c667862785719849fbb1b28e1e541604a0a5309777e69683b16a20173fb` |
| Sysbox mgr executable | `9e61acac59a3143b6cbd50907274e8873b01901dc34f36e2fab5ea8228c28d07` |

The ordered CRIU patch backports upstream `e956db75f7a60c883daf9440a3ffed909515d2f1`.
It preserves the high exit-status bit of a restored zombie: status 201 had been
restored as 73. A native regression extracts the actual old/new restore function;
the old function fails 128 of 256 normal statuses and the corrected function
preserves all 256. Its red and green results remain in
`factorybox-criu-zombie-exit-regression-37983868c62b43c5b457b0438c8f0b0e`.

The separate real namespace suite,
`factorybox-runtime-a5d844f468c544418aada7847aa58302`, passes 15 tests and 62
subtests with zero skips, including the original status-47 lifecycle and a new
status-201 case. The latter preserves parent/PID/wait status, RAM, independent
fork/recapture, source deletion and descendant assertions. Original reference
fixtures remain unchanged; only fresh native copies receive the declared actor
patch and recorded suite-selection extension. All 1,030 upstream CRIU test
files remain unchanged; that is a file-integrity count, not 1,030 executed tests.

The full matrix is
`factorybox-release-native-21323bab8196426e82394fbc8498e35f`, result SHA-256
`60da1aef3d782e9d59f90fc6c4373d23338a1825c5ded0bdb2231132c56a8abf`.
Every required child passed with cleanup and unchanged-input checks. Existing
upstream skip sets are retained, including Systemd's four executed passes and
two original skips. All seven portable gates pass with 411 Python tests in
`factorybox-checks-1c17c49abc7049b5abe3780e1b40d723`; all 232 reference files and
vet remain intact.

Independent worker `factorybox-build-worker-825279bfefb54318852d88c029d963f5`
reproduces the CLI, every bundle member, package and complete image. Independent
CRIU compilation `factorybox-build-criu-retained-5f5c9bae6e5c49acad640f39098068f1`
matches the selected `factorybox-build-criu-retained-ec3fa02d5dbb4c3ca826438e10c36f6d`.
The unchanged Sysbox components retain their independent native compilation proof
from `factorybox-upstream-82b3bb6c0c4e47359a9aa20ce6829489`. The new matrix also
reruns the complete Sysbox unit and integration suites.

The failed comparison build `factorybox-build-worker-796990d77d2b4dab9872974875b2f7b5`
remains failed. Its CRIU executable was identical, but a different build-receipt
name changed `build.json` and therefore package/image hashes. Repeating assembly
with the same exact receipt produced the matching pair; no provenance field or
comparison assertion was removed. Its optional compiler cache is losslessly
retained with reconstruction receipts. The first namespace admission refusal
and portable fixture-guard failure also remain explicit.

Evidence closure is
`factorybox-upstream-review-4dd04b8717004785908a2ffdc5a543db/verification-result.json`,
SHA-256 `204bd2ad1e46fb8d6defb0b835ee180be2cef80944cc2cdc43eb0d1262650637`.
It binds the native matrix, separate namespace suite, independent builds,
portable gates, matching source archive and exact cleanup. The same admitted
kernel/page/cgroup/storage profile and all resource/deadline limits above were
used. The owned builder was removed; zero containers and all nine retained
volume identities were confirmed. No host configuration or installation changed.

Storage maintenance kept the 32-GiB cap and 8-GiB start headroom. Lossless dump
compaction verifies the complete decoded bytes. One older, superseded local
OpenCode prepare-phase failure's bulk snapshot was deliberately retired after
an exact-consumer-lane passing replacement, with full member hashes, selected
diagnostics, source, logs and the original failed result preserved in
`factorybox-storage-retirement-13a7532290b54a44a2fd0347accbb134`. That retired bulk
payload is no longer reconstructable. Current unresolved EBUSY and provider
failure evidence remain preserved. A canceled optional repack removed only its
incomplete encoder output and retained its complete original archive.

These are individual acceptance observations: source-lane preparation 42.50 s,
Start to first useful work 14.83 s; reserve-lane preparation 37.53 s and first
useful work 35.34 s, including deliberate standby Pause/Resume and new-container
checks. The standby Resume command took 3.57 s. Paused whole-worker counters
were 2,402,082,816 and 2,964,664,320 bytes, including file cache and test services;
their anonymous counters were 29,196,288 and 26,537,984 bytes. The paused workload
cgroups had zero anonymous RAM and zero resident snapshot bytes. These samples
establish no percentile, speedup or end-user latency guarantee.

The current matching sources and image inventory are in the
[distribution report](distribution-0.1.0.md). ARM64, real-provider acceptance,
historical EBUSY attribution, upstream review and the final distribution/profile
gate remain open. No stable release or image has been published, no retained
installation activated, and neither deferred endurance series started.
