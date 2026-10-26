# Licensing and source distribution

Original Factorybox code is licensed under the Apache License, Version 2.0 in
`LICENSE`. This includes the original Go lifecycle controller, build tools and
tests. `NOTICE` records attribution and the nature of the modifications.
Existing third-party copyright, license and notice statements remain in force.

| Component | Applicable terms |
| --- | --- |
| Sysbox runc, fs, mgr, ipc and libs | Apache-2.0, with the original component and dependency notices |
| Sysbox FUSE dependency | Its original BSD and other file-specific terms in `upstream/licenses/sysbox-fuse/LICENSE` |
| CRIU executable and modifications outside `lib/` | GPL-2.0-only, unless the particular source explicitly provides otherwise |
| CRIU `lib/` | LGPL-2.1 under the pinned CRIU `COPYING`, with file-specific exceptions preserved |
| Go dependencies and worker operating-system packages | Their respective original licenses |

The Apache license at the repository root does not replace these component
licenses. In particular, patches to CRIU retain the license applicable to the
modified upstream files. Imported reference inputs and overlays keep their
existing provenance and notices; adding the root license does not rewrite them.
The component pins and original license hashes are in `upstream/lock.json` and
`upstream/licenses.json`. Modified source and ordered patches must travel with
the appropriate notices. Apache-2.0 and GPL-2.0-only code must not be combined
into a derivative work under a blanket Apache license.

Each worker package includes the exact approved Factorybox license, notices and
the pinned upstream license texts. Its build manifest records hashes of those
files. A matching source archive accompanies the release: the frozen Factorybox
sources and recipes, exact prepared Sysbox and CRIU sources, original component
archives, ordered patches and component build receipts. This supplies the
preferred source form of the modified CRIU component, including its build
scripts. The binary and source receipts must identify the same worker build.

The complete worker image also includes its pinned operating-system base and
runtime libraries. Their original licenses and package inventory remain part
of that image; the component source archive does not claim to replace the
distribution obligations for every operating-system package. Public image
distribution requires checking those obligations for the exact base as well.
