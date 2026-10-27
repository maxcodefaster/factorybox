# Native AMD64 verification of 0.1.0

Verified October 27, 2026. All 23 required native functional gates passed on the
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
