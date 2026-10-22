# Local runtime bundle

This page preserves the original v0.1 VFS bundle and its reconstruction
commands. The new standalone worker/runtime package, independent reproduction
and pilot gate status are in [pilot-validation.md](pilot-validation.md);
its deployment contract is in [worker-contract.md](worker-contract.md).
The original profile and its saved package remain unchanged.

The bundle contains `factorybox`, `sysbox-runc`, `sysbox-fs`, `sysbox-mgr`,
`criu`, a profile, a source/binary manifest and the pinned upstream license
notices. Original-code licensing and distribution review remain open before
public distribution. The bundle contains no IDE, agent,
Factory Connector or Dev Containers dependency. Those applications may run
inside or beside a sandbox as consumers.

The AMD64 profile selects the locally validated v0.1 reference. Production
qualification is separate. The supported tuple is
Linux AMD64, 4096-byte pages, kernel `7.2.8-9-omarchy`. A different kernel,
architecture or worker requires its own native evidence.

## Build locally

Prepare and verify the pinned sources, build the public native base and Sysbox,
then build CRIU with the five ordered retained-context recovery patches:

```sh
mise exec -- python3 tools/sources.py prepare
mise exec -- python3 tools/sources.py verify
mise exec -- python3 tools/build_native_base.py --platform linux/amd64
mise exec -- python3 tools/build_sysbox.py --platform linux/amd64 --network host --bounded-build
mise exec -- python3 tests/criu-retained-candidate/build.py \
  --native-base-build "$base_build" --exit-hold --exit-hold-socket
mise exec -- python3 tools/build_runtime.py \
  --sysbox-build "$sysbox_build" --retained-candidate "$criu_build"
```

Set the three task-specific variables to the exact successful artifact names
printed by those commands. On this Linux host, Docker tools use
`FACTORYBOX_DOCKER_CONTEXT=default FACTORYBOX_DOCKER_ELEVATION=sudo`.
Do not add the nested-overlay candidate options. The reference uses Docker VFS.
Build commands install nothing as a host runtime and publish nothing.
Use the matching successful native-base image as the worker operating system.
The Go CLI is static; Sysbox and CRIU also require the runtime libraries
provided and tested by that base. The tar is not a universal host installer.

The package builder verifies the exact profile, component hashes, extra CRIU
patches and overlay. It compiles the Go CLI with an empty compiler cache and
network access disabled for Go modules. Acquire missing exact Go modules once
with `mise exec -- go mod download`; the repository's existing checksum file
and normal checksum verifier remain enabled during acquisition.

Every build creates its own artifact. Repeat the package command with
`--compare "$previous_runtime_build"` to require an identical whole tar,
including normalized file order, ownership, permissions and timestamps.
The result retains the Go toolchain tree hash, verified modules, compiler
arguments, runtime dependency list, source context and binary hashes.
Cross-compilation does not qualify another worker.

The locally validated package is
`artifacts/factorybox-build-runtime-f3450055b50645c48c43bfeb9638446b/factorybox-v0.1-linux-amd64.tar`.
Its complete SHA-256 is
`f9231951d4b0e332327737ffab86e318bab1af49d4abff8efd094627bb95bf2b`.
The validation report records the independent rebuild and tests of these
exact CLI bytes.

## Run in an owned worker

The local CLI is an administrative interface. It requires worker root for
lifecycle operations, a private mode-0700 state directory, the matching
Sysbox services and a trusted administrator-prepared OCI bundle. Keep those
services, their sockets and manager data inside the same owned worker. Apply
the manager options from `profile.json`. The tuple check is available without
starting a sandbox:

```sh
./factorybox version
./factorybox doctor
```

`doctor` checks the platform tuple only. The component, filesystem and native
lifecycle checks are separate evidence in the validation report.

Pass exact executable identities to each lifecycle command. For example,
inside that worker:

```sh
./factorybox --root /var/lib/factorybox \
  --runtime /opt/factorybox/sysbox-runc --runtime-sha256 "$runc_sha256" \
  --criu /opt/factorybox/criu --criu-sha256 "$criu_sha256" \
  --manager-root /var/lib/sysbox/manager \
  start --bundle /work/trusted-bundle workspace
```

Read the hashes from the verified bundle manifest. The same prefix accepts
`state workspace`, `checkpoint --snapshot-id ID workspace`,
`fork --source workspace --snapshot-id ID branch`,
`pause --snapshot-id ID workspace`, `resume --snapshot-id ID workspace` and
`delete workspace`. Snapshot IDs are immutable 32-character hexadecimal
identities. `prepare-base --source DIRECTORY base-id` imports a complete
immutable filesystem base before starting consumers. Retained JSON outcomes
make completed operations replayable and uncertain attempts fail closed.

The reference bounds the sandbox worker at 3 CPU/3 GiB with at most two live
sandboxes, and separate Theia at 1 CPU/768 MiB. The worker owner must enforce
these outer limits and keep the trust boundary at separate workers per
customer or trust domain. The CLI accepts trusted OCI input; it is not an
untrusted public provisioning service or a worker quota scheduler.

See [native validation](amd64-validation.md) for executed gates, actual
preparation/first-use/memory observations, unsupported attached exec sessions
and remaining production gates. This original v0.1 package has no worker-restart
RAM recovery. The current pilot adds same-host committed-pause recovery through
the separate [worker contract](worker-contract.md). Cross-node migration remains
unsupported.
