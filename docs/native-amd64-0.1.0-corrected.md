# Corrected 0.1.0 AMD64 verification

Updated October 28, 2026. The corrected ordinary image passes every original
native release gate, the complete namespace regression and all 17 current MAIN
qualification groups. It is independently reproduced. Overall production and
public-distribution qualification remain false;
no stable tag, release, image publication or installation activation happened.
All five current-image operating cases also pass with an independent closure.

The original requested commit `955446f1aa9630a9692de86dc8568292bc401647`
and its completed verification remain in the [earlier report](native-amd64-0.1.0.md).
The new source is its descendant, without resetting local work or changing the
reviewed Factory consumer pin.

## Exact inputs and delivered bytes

| Identity | Value |
| --- | --- |
| Factorybox source | `1284d7357d070a4fc46ba33cfe14937ae8ae480a` |
| Reviewed Factory consumer | `a5b9a931fa2a61e328d122f6ce1250664b52459e` |
| Recorded version | `0.1.0` |
| Ordinary worker | `factorybox-build-worker-7c0a09e5d9b746bdbfb595c868cdaadf` |
| Independent worker | `factorybox-build-worker-9b3a2787df75475292c5997ca08c2efe` |
| Local Docker image ID | `sha256:aa6d803a56c8ea346823681e6b90e3f4cca55699ac7b8960e5bb288bc5271121` |
| Deterministic worker package SHA-256 | `f1e4c85d8ae184aed3899dadeb3b8b39079410686036932dc79deb43235ac22b` |
| Retained compressed package SHA-256 | `30ec2e10aaca339136c206d6886386011edd4658c361a057785066bc63262a52` |
| Factorybox executable SHA-256 | `d7734394fb92d333c3e08a11dbc8e24da2bc3e6b50ac7b940fe69a5ee34e8247` |
| CRIU executable SHA-256 | `93e9659473056aa0c53c7828bc338b6c41cbef4893ca4164624f2b6e2937636c` |
| Sysbox-runc executable SHA-256 | `3c66c0239a512ebd51e4a0aab8737e557d726f5989314bc64af172273aed47e3` |
| Sysbox-fs executable SHA-256 | `f1a23c667862785719849fbb1b28e1e541604a0a5309777e69683b16a20173fb` |
| Sysbox-mgr executable SHA-256 | `9e61acac59a3143b6cbd50907274e8873b01901dc34f36e2fab5ea8228c28d07` |

Both worker builds match in package, image, every layer, executable bundle,
source context, source lock and normalized image-context transport. Every CRIU
phase was also independently compiled: retained base, recovery corrections,
shared descriptions and namespace access. Their final CRIU builds are
`2d9c401a393f4a5b80b112124e3bd56c` and `4d2d91959cb54bae854bdf16b13b45e6`.
The unchanged three Sysbox executables remain bound to their independent
compilation proof. These are local image identities, not published GHCR digests.

## What changed and what passed

The ordinary pipeline had omitted five recovery corrections already present in
the earlier programme composition. The unchanged process-tree regression
reproduced three false-success paths. The base source lock now selects the
original process-tree failure-propagation patch; production tooling adds and
requires the other four exact recovery patches before shared-description and
namespace-access assembly. Direct ordinary worker builds enforce the same set.
Protected build fixtures, imported overlays and reference assertions remain intact.

The four additional CRIU defensive backports cover thread-array reuse,
controller-string bounds and all six callers, mount/lock scan widths and exact
loginuid write length. All 1,030 original CRIU test files remain unchanged.
The seven portable gates pass with 415 Python tests and 232 immutable reference
files. Both CI architectures pass the extracted-function and original seven-case
process-tree checks; this does not qualify an ARM64 worker image.

The complete non-publishing runner passes all 23 original gates:

- CRIU core: all 18 selected original cases, with two cycles each.
- Complete pinned Sysbox unit and libcontainer integration packages.
- Bats lifecycle and Systemd suites, retaining their original upstream skips.
- All fourteen required worker lifecycle, timer, capacity and retirement suites.
- Planned and abrupt worker replacement.
- Both Factory source and reserve/pool lanes on the reviewed consumer pin.

Every child reports successful cleanup and unchanged checked inputs. No assertion,
deadline or native child was removed, skipped or interrupted to obtain a pass.
The separate ordinary namespace suite passes all 15 main tests and 62 subtests,
with zero skips, including the original status-47 and added status-201 complete
nested-zombie lifecycles. The original C/kernel parent-child regression also
saves/restores all three processes and preserves the child's original status 47.

The Docker host matches Linux AMD64, kernel `7.2.8-9-omarchy`, 4096-byte pages,
cgroup v2 and the admitted local storage path. The original resource ceilings,
maximum two live sandboxes and 420-second lifecycle deadline remain enforced.
No kernel, VM, host runtime or global sysctl change was made.

## Evidence and retained sources

The controller directory under `artifacts/` is
`factorybox-upstream-applicability-19fb25b7b9ed4487bc4e404a16f291d6`.

| Receipt | SHA-256 |
| --- | --- |
| `corrected-amd64-closure.json` | `39d280aa3c850bf341490d417a739ddc640fcb9423a937dc885c93455b34950e` |
| Full matrix `factorybox-release-native-f1ece1d25f1a407c8bf83c60888173d9/result.json` | `470417341e3b83bb4a2ac3d2f4a18e2169bd27df7893048b467048f39898a167` |
| Namespace `factorybox-runtime-5051159e67d948e2aeb273427268a8e1/result.json` | `19aed40fa2b935b9c9441bcf620c19288af940a9c9d93e8dceb89d13042b0f36` |
| `current-distribution-verification.json` | `ecbefb2c1cd1c0ee2658b5cb5633fefb94e19bd6c36259c8039604c86c4c5951` |

Matching sources are in `factorybox-release-sources-1b6a37d54f1540e094054ec6bdb4141c`.
The archive `factorybox-0.1.0-linux-amd64-sources.tar.zst` is 101,450,254 bytes,
SHA-256 `2170dc09b56433a38639f132a921e6504120f3bd917643fceca93da2f43cd0a8`.
All 4,673 members and complete decoded bytes were independently checked.
The unchanged CLI, Sysbox Go and observed native-library companions have new
explicit bindings to this image; their original archives/results were preserved.
The [distribution report](distribution-0.1.0.md) describes their limited coverage.

The never-started image inspection is
`factorybox-distribution-audit-f651e6bf4f0c496da872b7a86ff5db52`.
All 514 retained metadata files verify; 258 installed package tuples match the
exact native base. Inventorying 141 source-package versions and inherited
executables does not establish complete distribution clearance.

## Cleanup and limits

### Current MAIN programme follow-up

The same delivered bytes also pass the broader 17-group MAIN qualification on
source `eec596cec30a1cd93f9d208902e9e7560ae5911e`, which adds only documentation
since runtime source `1284d7357d070a4fc46ba33cfe14937ae8ae480a`.
Fifteen complete lifecycle suites use ordinary native builds, the exact
delivered CLI and components, with zero skips; two storage groups exercise the
exact worker image. The current namespace result is reused by its verified
hash. The remaining 16 groups were executed afresh with their original checks.

| Group | Evidence under `artifacts/` |
| --- | --- |
| Lifecycle | `factorybox-runtime-1aa4f7234f044040ad7bf6994496611d` |
| Guard | `factorybox-runtime-33e1640de82c4f5699a00d425a0f44d9` |
| Capture | `factorybox-runtime-f7692184bc4942718d7251b0e2099d66` |
| Pause | `factorybox-runtime-6546cb11388b47a083b5ebb4b260da4f` |
| Base | `factorybox-runtime-81757d36d4674865a3ea129314ea791a` |
| Base pause | `factorybox-runtime-99c3475ed4794ff194fcac8cfd341e61` |
| Delegation | `factorybox-runtime-2e3b07a8a80a45fb8c9d5281c1fa44d7` |
| Auxiliary controllers | `factorybox-runtime-e236a56498974dec9dc4f6a401695ea7` |
| Namespaces | `factorybox-runtime-5051159e67d948e2aeb273427268a8e1` |
| Time namespaces | `factorybox-runtime-b7616f7735ba4feab756ab300e7f3756` |
| Detached proc descriptor | `factorybox-runtime-3197bc54b58744288e3b8a0af36fbfd1` |
| Overlay | `factorybox-runtime-a6c7fa19a743493d865a06d0b854c897` |
| CRIU interruption | `factorybox-runtime-63fa4eed27b547dfa2eb8513b8e37e7b` |
| Capture admission | `factorybox-runtime-6af2e39c3df14dc7a67bd23e935c8c89` |
| Capture-worker loss | `factorybox-runtime-558dbee2f17a47dfafeaff21e344200a` |
| Storage admission | `factorybox-storage-admission-10bfd4c5056848d0ab623a79f9564ee4` |
| Storage controller loss | `factorybox-storage-admission-b1322e9de0f54c208539387c503b1032` |

The controller is
`factorybox-release-profile-qualification-4e632f9fc8c34032baa267f3e6706387`.
Its independently verified `current-main-closure.json` has SHA-256
`c9cac202f9ce92342ccdce3b5256af9d88ac45e8fcf82b2f71a4561d0412fb26`.
It checks complete current archive decompression, frozen source manifests,
production-core and executable bindings and exact owned-resource cleanup.
`cleanup.json`, SHA-256
`52fbe970cae5d66eb57f3eab5e4f225ff253bc11181fde90112570d266244c6a`,
confirms zero containers, all nine pre-existing volume identities and all
232 immutable reference files. Artifact allocation was 25,701,203,968 bytes
after cleanup, within the unchanged 32-GiB limit with the 8-GiB start reserve.

Five sequence controllers stopped between successful children at that reserve;
their original refusal logs remain. Closed current dumps were losslessly
re-encoded and fully verified before their prior encodings were removed.
Twelve older successful MAIN output bulks were then retired after complete
decompression/member verification and replacement proof. Every original case
passed on the current build. Native assertion files remain byte-identical except
the declared additive namespace regression: its original status-47 whole file
was mechanically reconstructed byte for byte before retirement. Their
source, results, logs, complete member headers/hashes, selected diagnostics and
retirement receipts remain; those twelve old whole captures are deliberately
no longer reconstructable. The controller's three
`superseded-main-retention*.stdout` logs identify each receipt. All current full
captures, frozen inputs and unresolved failures remain preserved.

This closes the 17 MAIN groups. Full consumer/pressure/drain cases,
additional storage/integration coverage, the regular series and
final profile still need current-image reconciliation. The 23 release-image
gates above remain separately complete. Neither set closes ARM64, actual-provider,
historical EBUSY or distribution obligations.

### Current operating cases

All five original operating cases now pass on the same corrected image and
source `9ea72cce234e5da7baf3c007ba176d882e79e40c` (documentation-only changes
after the MAIN qualification above):

| Case | Evidence under `artifacts/` |
| --- | --- |
| Startup refusals | `factorybox-worker-startup-failure-318720b54885405994189e8ac45d704c` |
| Bounded-store identity | `factorybox-bounded-identity-8027448dcd8f481db7c731ed9b068482` |
| All stop guards | `factorybox-bounded-base-stop-e2d6f9e42bbb4e63bc19069fba9f4f29` |
| Supplied Compose workflow | `factorybox-worker-example-651b75dcf98243208fd359ea1a5b2161` |
| Actual recipe transition and rollback | `factorybox-operations-transition-a0bec394aa4548ab83f20330a2a4e3cd` |

The controller is
`factorybox-release-operations-current-84cd579aa0274ce8a6202e4149c3796f`.
Its independent `closure.json` has SHA-256
`7a5737f3bd177d9eae48bfd0556fdc1d8932ff439af6490ca25fc9e2a1f6f4bf`.
It binds exact image/CLI inputs, all original checks, unchanged observed global
sysctls, zero containers and all nine retained volume identities.

The transition performs useful work with the earlier recipe, explicitly deletes
and drains it, seals its complete store, and starts the corrected image on a
separate fresh store. A confirmed current pause must survive refusal of an
incompatible image and rollback to the exact corrected image. Complete RAM/files,
useful continuation and an independent descendant after source deletion pass;
the sealed earlier store's complete contents and recorded metadata remain equal.
This verifies the documented procedure for these recipes and component identities.

The first attempt,
`factorybox-operations-transition-b810c96bc29f42af91e6a020ad7b8e50`, remains failed
(result SHA-256 `786492c5900e701bef5ec55014dd2f158e5061f41267d486e50d766c1db0f668`).
Its old comparison image also changed Sysbox-runc, so the valid rejection named
that component before reaching the fixture's specific CRIU oracle. Both complete
failed store archives are retained and independently decoded/verified.

The corrected fixture selects previously verified ordinary worker
`factorybox-build-worker-d98fbdfb094b4c38b37b7c0dcc2c36d3`, image
`sha256:1b8ac1fc16d8467ffd0f80bedd4567e9510453bc0682c5439bced7ff3b878c6b`.
Its CLI and all three Sysbox executables match the current build; CRIU differs.
Every original assertion, vet/build step and 420-second native deadline remains
unchanged. Only three exact worker/package input identities change from the
original recipe; the before/after hashes and unchanged support files are declared
in `actual-transition-plan-v2.json`. The earlier image serves only this controlled
comparison. It is not selected as a production fallback.

### Current retained-context integrations and reviewed IDE build

All five original retained-context cases are independently verified on source
`3e9f70d4f47bd336da292bc9d93d812081a5cac1`, whose runtime core remains identical
to the corrected build above. The protected native fixture compiles an explicitly
kernel-pinned AMD64 candidate CLI, SHA-256
`9ca3a0e435eba95f878e36af46855e1b6be9fc0d18eb74f7cb594da170e0c305`.
It uses the exact current worker's four component executables. This is separate
from the delivered ordinary CLI/image acceptance above.

| Case | Native evidence under `artifacts/` | Outcome |
| --- | --- | --- |
| Interrupted child | `factorybox-runtime-retained-1dbc73a003fa43e9887fc29d7219aa4a` | 12 main / 61 subtests pass |
| Controller loss | `factorybox-runtime-retained-e101162680a345bcae63c0e3def13e77` | 12 / 61 pass |
| Guard loss | `factorybox-runtime-retained-4915d2ce20eb42649a972d3db76d5015` | 12 / 61 pass |
| Thread churn | `factorybox-runtime-retained-831160f0329f42f5a309cedbe0416354` | 12 / 61 pass |
| Deliberate RAM corruption | `factorybox-runtime-retained-6189b348d724495bb97dab8aa72e391a` | Exact original complete-RAM assertion fails; rejection control accepted |

The controller is
`factorybox-current-integration-83d9cdd7ba84476c80b3760f35e3a099`.
Its independent `retained-closure.json`, SHA-256
`d6083dfce96d76ae4e0c2202dd1d00985fffcd601df105326d929b934930812c`,
verifies all five source contexts, current production core, executable inputs,
complete capture decompression, unchanged original assertions, zero containers
and all nine retained volume identities. There are no added skips. The corruption
child remains failed and retains its full evidence; its parent accepts only the
original single RAM-integrity failure with successful adapter checks and cleanup.

The first extra controller incorrectly required the aggregate to have a cleanup
field, although cleanup belongs to its actual native child. Its rejected result,
`retained-progress.json` SHA-256
`50f8207eab2b24f36d1ab360b405c390d0bc2e106ad223de1829e644202004fe`,
is preserved. The corrected reader checks the actual child and rejects six bad
cleanup/outcome/hash/OOM controls. It reuses the already successful native case
without rerunning or relabelling that rejected controller.

Eight older successful retained-context output bulks were retired only after
complete original decompression, member hashes, selected diagnostics and exact
original assertion/flag checks against their current replacement. Sources,
results, logs, frozen inputs and receipts remain. These eight superseded whole
captures are deliberately no longer reconstructable; every current full capture
and unresolved failure remains. Byte-identical closed executable inputs share
immutable files, including the explicitly hash-bound accepted negative control.

The earlier developer-IDE image uses Factory commit `be8e3ba`; it cannot represent
the reviewed `a5b9a931fa2a61e328d122f6ce1250664b52459e` pin because 15 Theia source
paths changed. New local build
`factorybox-reviewed-ide-build-a688bb288087413b95c4c37b1586301d` uses that exact
reviewed source and its unchanged Dockerfile, with a 3-CPU/3-GiB builder and
900-second build limit. Its image is
`factorybox-reviewed-ide-a688bb288087413b95c4c37b1586301d@sha256:6600a59afa1c2ee011478da4dab592bb543ba4a8874d3af6b33cade0641ae756`.
Native ELF/backend inspection and the original IDE setup admission pass in
`factorybox-theia-admission-2bdfbe38fb3b48ca9c872b918e23e76c`.
The owned builder/state are removed and retained volumes are unchanged.
Build/setup alone do not qualify the full IDE lifecycle. The reviewed runtime/hub
build `edb59de28a7d44d48fa4cfe79426f297` is a distinct image role; the older frozen
developer rootfs remains a distinct historical input.

The reviewed raw replacement is independently verified in
`factorybox-devx-inputs-73caf48e38814e32bee3afca47371757`, result SHA-256
`2430f07fb96e571244de5abc8bf690064eeb92a8ba4b866e79f7a700afb038f1`.
It binds the reviewed runtime and IDE images above and the complete 1,823,902,720-byte
Docker export, SHA-256
`d9f436161a580316cbf9445d62a0c60145525f81dd5071c7a5431fe20d64acd4`.
Its source image/version inspection and export completed successfully. A subsequent
XZ-encoding attempt exceeded its fixed 300-second deadline in
`factorybox-devx-inputs-c7847c6606fc4eed8fdcd8e1d416352d`; that original result
remains failed, SHA-256
`cb00f460f572cd429fb702fe7d50f3e80f07dad58fac67e5849333d599fb3c6f`.
Its complete raw export, partial encoding, source and logs remain intact.

The new raw input reuses only the completed export/inspection stages offline.
It shares the exact immutable file and passes the unchanged developer-input
loader. The independent `raw-export-reuse-verification.json` has SHA-256
`0dc80b029f77b045fdf407c09f46e36e8ed62179a62aad9b127db28e0ee56ccb`.
It verifies every raw byte and selected image, preserves the extra controller's
tuple/JSON readback rejection, confirms zero containers and all nine retained
volume identities, and does not reclassify the failed XZ attempt.

The next eight original integration commands are prepared but unrun. Native
admission currently refuses their start: about 25.67 GiB of artifacts plus the
required 8-GiB reserve exceeds the unchanged 32-GiB budget. A request to increase
that budget to 64 GiB is pending; no admission check is bypassed. All original
native limits and the 420-second lifecycle deadline remain unchanged.

### Earlier release-matrix cleanup

Only the owned bounded builder and state volume were removed. Zero containers
remain; all nine retained volume identities are unchanged. Verified identical
closed inputs share immutable files. The superseded earlier successful namespace
bulk was retired under `factorybox-storage-retirement-8690dd456bda4e6c8b1379500ca5b5cf`
after complete decompression and file-hash verification. Its source, results,
logs, complete member index and selected diagnostics remain; that older whole
capture is deliberately no longer reconstructable. The current full capture,
frozen inputs and unresolved failures remain intact.

The original process-tree failures, caught fixture-edit/header mistakes,
admission refusals and interrupted intermediate matrix retain their original
outcomes. That intermediate matrix completed only two gates and left 21 unrun;
the corrected matrix executed all 23 afresh.

ARM64 image qualification remains missing. The eight real-provider continuation
cases remain at zero passes after HTTP 429 before tool start; the saved next
lower bound is `2026-10-29T00:00:00.460472Z`. The historical EBUSY holder remains
unattributed. Remaining upstream/process/LSM decisions, complete native/inherited/OS
source and notice coverage, the inherited notice conflict and the final exact
programme profile remain open. Older changed-component programme proofs cannot
be silently inherited. See the [current checklist](first-release-status.md).

The 24-hour soak and 100-run series remain deferred. These functional results
make no long-term stability, general Kubernetes, arbitrary-host or p95 claim.
