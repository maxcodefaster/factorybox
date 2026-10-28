# Corrected 0.1.0 AMD64 verification

Updated October 28, 2026. The corrected ordinary image passes every original
native release gate and the complete namespace regression. It is independently
reproduced. Overall production and public-distribution qualification remain false;
no stable tag, release, image publication or installation activation happened.

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
