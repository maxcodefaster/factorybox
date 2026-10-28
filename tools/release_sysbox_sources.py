#!/usr/bin/env python3
"""Prepare hash-bound Go source inputs for the three delivered Sysbox binaries."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import uuid

import build_config
import build_worker
import reference
import reference_lifecycle
import release_cli_sources
import release_sources
import upstream_unit

ROLES = {
    "sysbox-runc": ("github.com/nestybox/sysbox-runc", "github.com/nestybox/sysbox-runc"),
    "sysbox-fs": ("github.com/nestybox/sysbox-fs", "github.com/nestybox/sysbox-fs/cmd/sysbox-fs"),
    "sysbox-mgr": ("github.com/nestybox/sysbox-mgr", "github.com/nestybox/sysbox-mgr"),
}


def tree_identity(directory):
    manifest = upstream_unit.context_manifest(directory)
    return {"files": manifest,
            "sha256": hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest()}


def dependencies(info, role, platform, compiler_version):
    settings = {row["Key"]: row["Value"] for row in info.get("Settings", [])}
    if (role not in ROLES or info.get("Main", {}).get("Path") != ROLES[role][0] or
            info.get("Path") != ROLES[role][1] or info.get("GoVersion") != compiler_version or
            settings.get("GOOS") != "linux" or settings.get("GOARCH") != build_config.architecture(platform) or
            settings.get("CGO_ENABLED") != "1"):
        raise ValueError("Sysbox build information differs from the delivered role/target/compiler")
    remote, local, seen = [], [], set()
    for row in info.get("Deps", []):
        if row.get("Path") in seen:
            raise ValueError("Sysbox dependency observations contain duplicate identities")
        seen.add(row.get("Path"))
        replacement = row.get("Replace")
        if replacement is not None:
            if (replacement.get("Version") != "(devel)" or replacement.get("Sum") or
                    not isinstance(replacement.get("Path"), str) or
                    not replacement["Path"].startswith(".")):
                raise ValueError("Sysbox dependency needs an exact supported local-source binding")
            local.append({"path": row["Path"], "replacement": replacement["Path"]})
            continue
        # Reuse the strict remote identity validation without granting CLI target authority.
        module = {"path": row.get("Path"), "version": row.get("Version"), "sum": row.get("Sum")}
        if (not isinstance(module["path"], str) or
                not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._~+/-]*", module["path"]) or
                ".." in Path(module["path"]).parts or
                not isinstance(module["version"], str) or
                not re.fullmatch(r"v[0-9][A-Za-z0-9.+-]*", module["version"]) or
                not isinstance(module["sum"], str) or
                not re.fullmatch(r"h1:[A-Za-z0-9+/]{43}=", module["sum"])):
            raise ValueError("Sysbox remote module has no exact checksum identity")
        remote.append(module)
    if not remote or not local:
        raise ValueError("Sysbox dependency observations are incomplete")
    return remote, local


def bind_local(row, role, context, pristine, replacements):
    original = row["path"]
    declarations = [item for item in replacements
                    if item["Old"]["Path"] == original and item["New"].get("Path") == row["replacement"]
                    and not item["New"].get("Version")]
    if len(declarations) != 1:
        raise ValueError("local dependency is not bound by the exact source go.mod")
    context = Path(context).resolve()
    target = (context / role / row["replacement"]).resolve()
    if not target.is_relative_to(context) or not (target / "go.mod").is_file():
        raise ValueError("local module leaves the verified Sysbox source context")
    relative = target.relative_to(context)
    # The pinned Dockerfile restores pristine runc before compiling fs/mgr.
    # The private experiment CLI must never be attributed to those consumers.
    if relative == Path("sysbox-runc") and role != "sysbox-runc":
        target, prefix, variant = Path(pristine), "sysbox/pristine-runc", "pristine-runc"
    else:
        prefix, variant = "sysbox/context/" + str(relative), "frozen-context"
    return {**row, "sourcePrefix": prefix, "variant": variant,
            **tree_identity(target)}


def compiler_image(dockerfile):
    match = re.match(r"FROM (golang@sha256:[0-9a-f]{64}) AS go\n", dockerfile)
    if not match:
        raise ValueError("Sysbox compiler source image is not pinned in its frozen recipe")
    return match[1]


def collect_compiler_sources(artifact, image, platform, version):
    def docker(*arguments):
        return reference.docker(list(arguments), timeout=180)
    with (artifact / "compiler-image.log").open("w") as log:
        subprocess.run([*reference.DOCKER, "pull", "--platform", platform, image],
                       check=True, stdout=log, stderr=subprocess.STDOUT, timeout=180)
    metadata = json.loads(docker("image", "inspect", image))[0]
    if (metadata["Os"] != "linux" or metadata["Architecture"] != build_config.architecture(platform) or
            metadata["Config"].get("Volumes")):
        raise ValueError("pinned compiler image has an unexpected target or volume declaration")
    owner = uuid.uuid4().hex
    name = "factorybox-sysbox-source-inspect-" + owner
    reference_lifecycle.write_json(artifact / "compiler-inspection-intent.json",
        {"owner": owner, "name": name, "image": image, "image_id": metadata["Id"],
         "scope": "Read Go sources from a never-started pinned compiler container"})
    identity = docker("create", "--name", name, "--label", "com.factorybox.run=" + owner,
                      "--network=none", "--read-only", "--cap-drop=ALL",
                      "--entrypoint", "/bin/true", image)
    output = artifact / "go"
    output.mkdir()
    try:
        for path in ["src", "LICENSE", "PATENTS", "VERSION"]:
            docker("cp", identity + ":/usr/local/go/" + path, str(output / path))
        if (output / "VERSION").read_text().splitlines()[0] != version:
            raise ValueError("pinned compiler image source version differs from the Sysbox executables")
    finally:
        current = json.loads(docker("inspect", identity))[0]
        if (current["Id"] != identity or current["Image"] != metadata["Id"] or
                current["Config"]["Labels"].get("com.factorybox.run") != owner or
                current["State"]["Status"] != "created"):
            raise ValueError("compiler inspection identity/state changed; preserve it for observation")
        docker("rm", identity)
        reference_lifecycle.write_json(artifact / "compiler-inspection-cleanup.json",
            {"passed": True, "owner": owner, "container_id": identity,
             "image_id": metadata["Id"], "neverStarted": True})
    return {"image": image, "image_id": metadata["Id"], "version": version,
            "source": tree_identity(output)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    options = parser.parse_args()
    worker_root, worker = build_worker.load(options.worker_build)
    component, compiled = reference_lifecycle.component_build(worker["sysbox_build"], "factorybox-build-sysbox-")
    context = component / "context"
    if (tree_identity(context)["sha256"] != compiled["context_sha256"] or
            reference.sha(component / "Dockerfile") != compiled["dockerfile_sha256"] or
            any(compiled["binary_sha256"][role] != worker["bundle_sha256"]["components/" + role]
                for role in ROLES)):
        raise ValueError("delivered Sysbox binaries differ from their exact source/recipe inputs")
    image = compiler_image((component / "Dockerfile").read_text())
    version = compiled["toolchain"].splitlines()[0].split()[2]
    artifact = reference.new_artifact("factorybox-release-sysbox-sources-" + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    shutil.copyfile(release_sources.__file__, artifact / "emit.py")
    result = {"schema": 1, "passed": False, "worker_build": options.worker_build,
              "version": worker["version"], "platform": worker["platform"],
              "worker_image_id": worker["image_id"], "sysbox_build": worker["sysbox_build"],
              "sysbox_binary_sha256": compiled["binary_sha256"],
              "worker_result_sha256": reference.sha(worker_root / "result.json"),
              "sysbox_result_sha256": reference.sha(component / "result.json"),
              "scope": "Sysbox Go source-input/module/notice and pinned compiler-source coverage. "
                       "No native-linker, inherited-image, operating-system or image-wide distribution clearance."}
    try:
        # The Go tool verifies data only. It never builds or runs a dependency.
        environment = {**os.environ, "GOENV": "off", "GO111MODULE": "on", "GOWORK": "off",
                       "GOTOOLCHAIN": "local", "GOPROXY": "https://proxy.golang.org",
                       "GOSUMDB": "sum.golang.org", "GONOSUMDB": "", "GONOPROXY": "",
                       "GOPRIVATE": "", "GOINSECURE": "", "GOFLAGS": "",
                       "GOMODCACHE": str(artifact / "module-cache")}
        shadow = artifact / "shadow"
        shutil.copytree(context, shadow, symlinks=True)
        with tarfile.open(context / "sysbox-runc-pristine.tar.gz") as source:
            source.extractall(artifact / "pristine-input", filter="data")
        pristine_roots = list((artifact / "pristine-input").iterdir())
        if len(pristine_roots) != 1 or not (pristine_roots[0] / "go.mod").is_file():
            raise ValueError("pristine runc source has an unexpected root")
        pristine = pristine_roots[0]
        declarations = json.loads((reference.ROOT / "upstream/sysbox-go-notices.json").read_text())
        notice_map = {(row["path"], row["version"], row["sum"]): row
                      for row in declarations["declarations"]}
        entries, per_binary, modules = {}, {}, {}
        log = artifact / "go-verification.log"
        for role in ROLES:
            if role == "sysbox-fs":
                shutil.rmtree(shadow / "sysbox-runc")
                shutil.copytree(pristine, shadow / "sysbox-runc", symlinks=True)
            info = json.loads(release_cli_sources.run_go(
                ["version", "-m", "-json", str(worker_root / "image-context/bundle/components" / role)],
                artifact, environment, log))
            remote, local = dependencies(info, role, worker["platform"], version)
            directory = shadow / role
            before = {name: reference.sha(directory / name) for name in ["go.mod", "go.sum"]}
            module_file = json.loads(release_cli_sources.run_go(["mod", "edit", "-json"],
                                                             directory, environment, log))
            local_bindings = [bind_local(row, role, context, pristine, module_file["Replace"]) for row in local]
            for module in remote:
                key = (module["path"], module["version"], module["sum"])
                downloaded = json.loads(release_cli_sources.run_go(
                    ["mod", "download", "-json", module["path"] + "@" + module["version"]],
                    directory, environment, log))
                files = release_cli_sources.downloaded_files(module, downloaded, artifact / "module-cache")
                modules[key] = (module, files)
            release_cli_sources.run_go(["mod", "verify"], directory, environment, log)
            if before != {name: reference.sha(directory / name) for name in before}:
                raise ValueError("Go verification changed a frozen source module manifest")
            per_binary[role] = {"sha256": compiled["binary_sha256"][role], "compiler": version,
                                "mainSource": "sysbox/context/" + role,
                                "remote": remote, "local": local_bindings}
        observations = []
        for index, (key, (module, files)) in enumerate(sorted(modules.items())):
            observations.append(release_cli_sources.add_module(
                entries, module, files, index, artifact / "notices", notice_map.get(key)))
        # Recheck every role after taking notice/source observations.
        shutil.rmtree(shadow / "sysbox-runc")
        shutil.copytree(context / "sysbox-runc", shadow / "sysbox-runc", symlinks=True)
        for role in ROLES:
            if role == "sysbox-fs":
                shutil.rmtree(shadow / "sysbox-runc")
                shutil.copytree(pristine, shadow / "sysbox-runc", symlinks=True)
            release_cli_sources.run_go(["mod", "verify"], shadow / role, environment, log)
        for (_, files), observation in zip((value for _, value in sorted(modules.items())),
                                          observations, strict=True):
            if reference.sha(files["Zip"]) != observation["zipSHA256"]:
                raise ValueError("Sysbox module source ZIP changed during collection")
        compiler = collect_compiler_sources(artifact, image, worker["platform"], version)
        release_sources.add_tree(entries, "go", artifact / "go")
        release_sources.add_tree(entries, "sysbox/context", context)
        release_sources.add_tree(entries, "sysbox/pristine-runc", pristine)
        for prefix, directory, names in [
                ("sysbox/build", component, ["Dockerfile", "result.json", "context-manifest.json"]),
                ("tooling", artifact, ["runner.py", "emit.py"])]:
            for name in names:
                release_sources.add_entry(entries, prefix + "/" + name, directory / name)
        release_sources.add_entry(entries, "sysbox/notice-declarations.json",
                                  reference.ROOT / "upstream/sysbox-go-notices.json")
        public = {name: {key: value for key, value in row.items() if key != "path"} for name, row in entries.items()}
        manifest = {**{key: value for key, value in result.items() if key != "passed"},
                    "compiler": compiler, "binaries": per_binary, "modules": observations, "members": public}
        reference_lifecycle.write_json(artifact / "sysbox-go-dependencies.json", manifest)
        release_sources.add_entry(entries, "sysbox-go-dependencies.json", artifact / "sysbox-go-dependencies.json")
        reference_lifecycle.write_json(artifact / "inputs.json", entries)
        package = artifact / f"factorybox-{worker['version']}-{worker['platform'].replace('/', '-')}-sysbox-go-dependencies.tar"
        with (artifact / "archive.log").open("xb") as output:
            encoded = reference.artifact_storage.archive_stream(
                [sys.executable, str(artifact / "emit.py"), "--emit", str(artifact / "inputs.json")],
                package, reference.ROOT, output, maximum=1 << 30, timeout=600, decoded_limit=4 << 30)
        with subprocess.Popen(["zstd", "-d", "-q", "-c", str(artifact / encoded["encoded"]["file"])],
                              stdout=subprocess.PIPE) as decoder:
            try:
                count = release_cli_sources.verify_members(decoder.stdout, entries)
                while decoder.stdout.read(1024 * 1024):
                    pass
                if decoder.wait(timeout=600):
                    raise ValueError("Sysbox source companion did not fully decode")
            except BaseException:
                decoder.kill()
                decoder.wait()
                raise
        if (tree_identity(context)["sha256"] != compiled["context_sha256"] or
                build_worker.load(options.worker_build)[1] != worker):
            raise ValueError("Sysbox source inputs changed during collection")
        result.update(passed=True, modules=len(modules),
                      localIdentities=len({(row["path"], row["replacement"]) for x in per_binary.values()
                                           for row in x["local"]}),
                      localBindings=sum(len(x["local"]) for x in per_binary.values()),
                      notices=sum(len(x["notices"]) for x in observations),
                      verifiedMembers=count, archive_encoding=encoded,
                      source_inventory_sha256=reference.sha(artifact / "sysbox-go-dependencies.json"))
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"]}), flush=True)


if __name__ == "__main__":
    main()
