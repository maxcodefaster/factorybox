# Distribution preparation for 0.1.0

Updated October 28, 2026. The AMD64 native tests pass; public distribution
qualification remains false. These receipts prepare release material without
changing the tested image or publishing an asset.

## Exact image and prepared sources

The audit inspects ordinary worker
`factorybox-build-worker-d98fbdfb094b4c38b37b7c0dcc2c36d3`, image
`sha256:1b8ac1fc16d8467ffd0f80bedd4567e9510453bc0682c5439bced7ff3b878c6b`.
Its complete native result is recorded in [the AMD64 report](native-amd64-0.1.0.md).
Both the source archive and tested runtime bind commit
`0b73944d6a34439d97984dcd8da8a08cbc264ae4`, including the CRIU zombie exit-status
correction. The preceding image's source archive remains a historical receipt.

| Prepared archive | SHA-256 | Coverage |
| --- | --- | --- |
| `factorybox-0.1.0-linux-amd64-sources.tar.zst` | `a9876f1bd3556580d9e0c0f0882eec0eb04629df2887edcded0a15bc6aaa0d71` | Frozen Factorybox sources, exact prepared Sysbox/CRIU sources, original component archives, patches, recipes and build receipts |
| `factorybox-0.1.0-linux-amd64-cli-dependencies.tar.zst` | `36a199458f864f3ab9fd3a1ab9384d919e99826eaece90eea5128490dd1d221f` | The CLI's 20 exact external module ZIPs, 27 module notice files and 39 notice files from its Go toolchain |
| `factorybox-0.1.0-linux-amd64-sysbox-go-dependencies.tar.zst` | `ccb267c14ef43c24a1c3f04957b9c120d62c8638d0e47439760b1d7b13cea0e2` | Sysbox's 70 exact remote Go module ZIPs, 94 original module notice files and 38 Go 1.25.14 notice files; 17 local replacements bind to the matching component source archive |
| `factorybox-0.1.0-linux-amd64-native-library-sources.tar.zst` | `743be71089374b18baeba7c975c042f99b74b32902d64e30fc2141722f8dca3c` | Exact Ubuntu glibc and libseccomp source packages, Ubuntu patches, original notices and authenticated source/binary package metadata; six compiler files match the corresponding binary packages |

All four archives passed complete decompression and hash verification. The CLI
module inputs also pass `go mod verify` and ZIP CRC checks, and match the
executable's module checksums. The whole Go 1.27.1 toolchain tree matches the
worker's recorded compiler-tree hash. The archive identifies CLI
`d7734394fb92d333c3e08a11dbc8e24da2bc3e6b50ac7b940fe69a5ee34e8247`.
The previously prepared CLI companion is explicitly rebound to this image by
`factorybox-upstream-review-4dd04b8717004785908a2ffdc5a543db/current-cli-companion-binding.json`;
its archive bytes, executable and toolchain identities are unchanged.

Evidence directories under `artifacts/`:

- `factorybox-release-sources-9d0b57842d604893ae9ef8941da47ea9`.
- `factorybox-cli-distribution-068e05ff0c994355abcf22a5509c01b9`.
- `factorybox-distribution-audit-3ef592ff838d4f6bace66e6733e727c2`.
- `factorybox-sysbox-go-distribution-8a43596789904f5186cabb19b12d4d4c`.
- `factorybox-native-library-sources-f44f90644b524824b5c7f42e153b3e5a`.

## Selected Sysbox Go dependencies

The source companion binds the three delivered Sysbox executables to all 70
remote module/version/checksum identities in their actual Go build information.
An owned compiler-image inspection container was never started. It supplied
the exact source ZIPs and Go 1.25.14 notices from the independently reproduced
component build. Its removal preserves all nine retained volume identities.
The original compiler image and qualified worker are unchanged.

Go's unmodified `dirhash.HashZip`/`Hash1` implementation verifies every ZIP
against its executable's `h1:` checksum. All ZIP CRC checks pass. The 17 local
replacement identities have 39 per-binary bindings; every local source-file
hash and notice matches the already prepared component archive. That archive
must accompany this companion. All 212 companion members are independently
checked after assembly and complete decompression. Controlled checksum cases
reject changed or missing source while accepting a different ZIP encoding of
the same content; none of those source fixtures was executed.

The first preparation remains failed because
`gopkg.in/hlandau/service.v1@v1.0.7` has no conventionally named license file.
Its exact README contains the original ISC text, and `service.go` carries its
copyright notice. A source/checksum-bound declaration retains both files
verbatim; no generic missing-notice bypass was added. The original failed
`result.json` stays unchanged, and the completed assembly is separately recorded
as `verified-result.json`. Independent `verification-result.json` has SHA-256
`44239d98481ce26ef58f84a266fd56d325b84725b7489e79262f76f3b62df144`.

This verifies the selected Go source/notice material. It does not cover static
libc/libseccomp or other native linkage, inherited executables, operating-system
sources, the conflicting inherited package notice, or overall distribution
qualification.

## Observed native-library source packages

The companion retains glibc `2.35-0ubuntu3.15` and libseccomp
`2.5.3-2ubuntu3~22.04.1`, the versions recorded in the exact Sysbox compiler
image. Ubuntu's signed `jammy-updates` metadata from the compiler's fixed
`20261018T000000Z` snapshot authenticates the source and AMD64 binary indexes.
The indexes bind every downloaded source file and the two development binary
packages by size and SHA-256. Source package descriptors also match the source
file checksums; their developer signatures were not independently verified.

Reading the binary packages without installing or executing them verifies all
six observed compiler files, including `libc.a` and `libseccomp.a`. The bundle
contains the preferred source packages and Ubuntu patches, original notices,
signed metadata and verification receipts. It contains no binary packages.
All 30 archive members pass independent hash checks after complete decoding.
A controlled change to the signed metadata is rejected by `gpgv`; that altered
metadata was used for no downloads. Inspection cleanup preserves all nine
retained volumes and leaves zero containers.

Independent `verification-result.json` has SHA-256
`87edb5ef7dc7874dede66a3e7f72a4d9022b7bb8198bd83495cafa29d3c87385`.
This verifies the two specified source packages and six binary/source bindings.
It does not establish a complete static linker-input inventory, coverage of
other runtime or operating-system packages, or overall distribution clearance.

## What the image audit establishes

The audit streamed the filesystem of one owned inspection container without
starting it or executing image code. It hashed files, retained bounded package
and license metadata, and read Go build metadata through anonymous memory.
The delivered CLI, CRIU and three Sysbox executables match the verified worker
bundle. Cleanup removed the inspection container and preserved all nine
pre-existing volume identities.

The 258 installed package name/version/architecture tuples exactly match the
native base inventory, whose SHA-256 is
`879eb717f48ca1f846f404d1f77fb3ce4680dea4ef9cc41080fa1aa89c16bf05`.
They map to 141 source-package/version pairs. Fifteen Go executables identify
605 distinct remote module/version pairs and 17 local replacement identities;
the three delivered Factorybox Sysbox executables use 87 distinct dependency
identities. These are inventories, not a statement that every listed package
has the same distribution obligations or that all source/notice coverage is
complete. Go build metadata is also incomplete for the inherited Docker CLI.

The mechanical inventory is `distribution-inventory.json`, SHA-256
`e49ae10f5de80358c8d444b967756e5d051036bd86a9549df25d1f5973f20761`.
Its underlying `image-inventory.json` is
`89a8b1da6efb9a0ab58d5477debf083ac7c2946ed5b8c61287e71528dcbf312b`.
The final verification receipt is
`79d092cb7659b417fd73c2ed8fd0968c7d0058416b79c1fda4bf77325cf1bf1b`;
it rechecks all 514 retained metadata files, both complete archives and the
matching toolchain. The preceding audit's unavailable Python `memfd_create`
wrapper and empty-header parser failure remain preserved under
`factorybox-distribution-audit-a3923f5147d946ea95108a3e16cbddb8` and its linked
failed inspection. Correcting those tools changed no image or native-test bytes.

## Remaining distribution work

- Complete source and notice coverage for Sysbox's static linkage and the
  inherited executables. The selected Sysbox Go dependency sources/notices are
  now verified above, as are the two observed glibc/libseccomp source packages.
  Complete linker-input coverage, CRIU's runtime libraries and operating-system
  packages still need their own exact-package review.
- Bind the inherited base's source and build provenance. It contains an unused
  `/envbox`, older `/usr/bin/sysbox-*`, Docker and containerd alongside the
  Factorybox components under `/opt/factorybox`. Their presence in the image
  remains relevant to distribution even though Factorybox uses its own runtime.
- Resolve the inherited `sysbox-ce` copyright-file discrepancy. It names
  Apache-2.0 and also contains restrictive modification/redistribution wording.
  The same wording exists in the official
  [Sysbox packaging file](https://github.com/nestybox/sysbox-pkgr/blob/master/deb/sysbox-ce/copyright).
  Upstream commit
  [62f6ad0](https://github.com/nestybox/sysbox-pkgr/commit/62f6ad0cdaff712804a00c49bb033f55d1fd583d)
  changed its license field to Apache-2.0 without removing the disclaimer.
  This records the provenance of the conflict; it does not resolve its effect.
  Preserve the original material while establishing the applicable terms.
- Identify complete Docker CE/CLI attribution material. The captured installed
  package metadata provides no copyright file for those two packages, and the
  Docker CLI's build metadata provides no dependency list. Absence from this
  capture is not proof that no other attribution material exists.
- Bind the completed material to each final architecture, image, package and
  source receipt before distributing them.

No source or binary archive here replaces the image-wide review required by
[LICENSING.md](../LICENSING.md). ARM64 qualification, real-provider acceptance,
the historical cleanup attribution, upstream review and final profile selection
remain governed by the [current release checklist](first-release-status.md).
