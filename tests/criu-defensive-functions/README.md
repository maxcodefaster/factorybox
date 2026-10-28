# Patched CRIU helper checks

Run after preparing the exact CRIU source lock:

```sh
mise exec -- python3 tools/sources.py prepare --component criu
mise exec -- python3 tests/criu-defensive-functions/run.py
```

The runner compiles the actual prepared controller-string, thread-list,
loginuid-write and file-lock-parser functions. Inert services replace procfs
writes and process enumeration. It checks ordinary controller encodings,
failure propagation, thread allocation/reuse, a reduced logical thread
capacity with sufficient physical storage, exact numeric write lengths and
both lock record formats. Historical bounds code is refused before compilation.
It does not attempt to trigger an overflow or write any host procfs setting.
It also runs the unchanged seven-case `tests/criu-pstree-dump.c` regression
against the actual prepared process-tree writer, including failure after an
earlier successful record. A truncated tree must never be reported as success.

Every run keeps the extracted source, executable, logs, source and fixture
hashes, limits and result under a newly admitted `artifacts/` directory.
The compile and test children each have two CPUs, a 1-GiB address-space limit,
30 CPU seconds, a 30-second wall deadline and a 32-MiB file limit. Failed runs
remain failed. Both native CI architectures run these checks.

These are function checks. The complete CRIU core, worker lifecycle,
replacement and Factory consumer suites still qualify the delivered image.
