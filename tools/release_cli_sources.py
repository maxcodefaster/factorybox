#!/usr/bin/env python3
"""Prepare exact Go dependency sources for one delivered Factorybox CLI."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import uuid
import zipfile

import build_config
import build_worker
import reference
import reference_lifecycle
import release_sources
import sources


def linked_modules(info, platform):
    settings = {item["Key"]: item["Value"] for item in info.get("Settings", [])}
    if (info.get("Path") != "factorybox/cmd/factorybox" or
            info.get("Main", {}).get("Path") != "factorybox" or
            settings.get("GOOS") != "linux" or
            settings.get("GOARCH") != build_config.architecture(platform) or
            settings.get("CGO_ENABLED") != "0"):
        raise ValueError("CLI build information does not match the delivered static target")
    modules = []
    seen = set()
    for item in info.get("Deps", []):
        path, version, checksum = (item.get(key, "") for key in ["Path", "Version", "Sum"])
        if (item.get("Replace") is not None or not isinstance(path, str) or
                not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._~+/-]*", path) or
                ".." in PurePosixPath(path).parts or path in seen or
                not isinstance(version, str) or not re.fullmatch(r"v[0-9][A-Za-z0-9.+-]*", version) or
                not isinstance(checksum, str) or not re.fullmatch(r"h1:[A-Za-z0-9+/]{43}=", checksum)):
            raise ValueError("CLI dependency lacks one exact remote module/checksum identity")
        seen.add(path)
        modules.append({"path": path, "version": version, "sum": checksum})
    if not modules:
        raise ValueError("CLI build information contains no dependency observations")
    return sorted(modules, key=lambda item: item["path"])


def downloaded_files(module, downloaded, cache):
    if (downloaded.get("Error") or downloaded.get("Path") != module["path"] or
            downloaded.get("Version") != module["version"] or downloaded.get("Sum") != module["sum"]):
        raise ValueError("downloaded dependency differs from the CLI's embedded checksum")
    cache = Path(cache).resolve()
    result = {}
    for key in ["Zip", "GoMod", "Info"]:
        value = downloaded.get(key)
        if not isinstance(value, str):
            raise ValueError("dependency download has no exact source file")
        path = Path(value).absolute()
        if (not path.is_relative_to(cache) or path.resolve() != path or
                path.is_symlink() or not path.is_file()):
            raise ValueError("dependency source left its owned module cache")
        result[key] = path
    # go mod verify compares the actual ZIP/directory with this cache checksum.
    # Bind that checksum to the delivered binary before invoking its verifier.
    checksum = result["Zip"].with_suffix(".ziphash")
    if checksum.is_symlink() or checksum.read_text().strip() != module["sum"]:
        raise ValueError("module cache checksum differs from the delivered CLI")
    result["ZipHash"] = checksum
    return result


def add_module(entries, module, files, index, notices):
    prefix = f"{module['path']}@{module['version']}/"
    archive_path = f"modules/{index:03d}.zip"
    release_sources.add_entry(entries, archive_path, files["Zip"])
    release_sources.add_entry(entries, f"modules/{index:03d}.mod", files["GoMod"])
    release_sources.add_entry(entries, f"modules/{index:03d}.info", files["Info"])
    with zipfile.ZipFile(files["Zip"]) as archive:
        names = archive.namelist()
        if (len(set(names)) != len(names) or
                any(not name.startswith(prefix) for name in names) or
                archive.testzip() is not None):
            raise ValueError("module source ZIP has changed or ambiguous members")
        for item in archive.infolist():
            relative = item.filename[len(prefix):].rstrip("/")
            path = PurePosixPath(relative)
            if (not relative and not item.is_dir() or path.is_absolute() or
                    ".." in path.parts or "\\" in relative or
                    relative and str(path) != relative):
                raise ValueError("module source ZIP member leaves its declared root")
        found = []
        for name in sorted(names):
            relative = PurePosixPath(name[len(prefix):])
            if not re.match(r"^(licen[cs]es?|copying|notice|copyright|patents)(?:$|[._-])",
                            relative.name, re.IGNORECASE):
                continue
            if name.endswith("/"):
                continue
            target = Path(notices) / f"{index:03d}" / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(archive.read(name))
            member = f"notices/modules/{index:03d}/{relative}"
            release_sources.add_entry(entries, member, target)
            found.append({"member": member, "moduleMember": name,
                          "sha256": reference.sha(target)})
        if not found:
            raise ValueError("dependency has no observed named notice: " + module["path"])
    return {**module, "archive": archive_path, "zipSHA256": reference.sha(files["Zip"]),
            "notices": found}


def run_go(arguments, directory, environment, log):
    result = subprocess.run(["go", *arguments], cwd=directory, env=environment,
                            capture_output=True, text=True, timeout=180)
    with Path(log).open("a") as output:
        output.write("$ go " + " ".join(arguments) + "\n")
        output.write(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError("Go source verification failed; see its owned command log")
    return result.stdout


def verify_members(stream, expected):
    seen = set()
    with tarfile.open(fileobj=stream, mode="r|") as archive:
        for member in archive:
            row = expected.get(member.name)
            if row is None or member.name in seen:
                raise ValueError("source archive has an extra or repeated member")
            seen.add(member.name)
            if row.get("directory"):
                valid, mode = member.isdir(), 0o755
            elif "symlink" in row:
                valid, mode = member.issym() and member.linkname == row["symlink"], 0o777
            else:
                valid = (member.isfile() and member.size == row["bytes"] and
                         hashlib.file_digest(archive.extractfile(member), "sha256").hexdigest() == row["sha256"])
                mode = 0o755 if row["executable"] else 0o644
            if not valid or (member.mode, member.uid, member.gid, member.mtime) != (mode, 0, 0, 0):
                raise ValueError("source archive member differs from its inventory: " + member.name)
    if seen != set(expected):
        raise ValueError("source archive is missing declared source members")
    return len(seen)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    options = parser.parse_args()
    worker_path, worker = build_worker.load(options.worker_build)
    artifact = reference.new_artifact("factorybox-release-cli-sources-" + uuid.uuid4().hex)
    artifact.mkdir()
    shutil.copyfile(__file__, artifact / "runner.py")
    shutil.copyfile(release_sources.__file__, artifact / "emit.py")
    result = {"schema": 1, "passed": False, "worker_build": options.worker_build,
              "version": worker["version"], "platform": worker["platform"],
              "worker_image_id": worker["image_id"], "worker_package_sha256": worker["package_sha256"],
              "cli_sha256": worker["bundle_sha256"]["factorybox"],
              "worker_result_sha256": reference.sha(worker_path / "result.json"),
              "scope": "Exact linked CLI Go module ZIPs, verbatim named notices and matching Go sources. "
                       "No Sysbox, native-library, inherited-image or image-wide distribution clearance."}
    try:
        environment = {**os.environ, "GOENV": "off", "GOTOOLCHAIN": "local", "GO111MODULE": "on", "GOWORK": "off",
                       "GOPROXY": "https://proxy.golang.org", "GOSUMDB": "sum.golang.org",
                       "GOPRIVATE": "", "GONOSUMDB": "", "GONOPROXY": "", "GOINSECURE": "", "GOFLAGS": "",
                       "GOMODCACHE": str(artifact / "module-cache")}
        log = artifact / "go-verification.log"
        version = run_go(["version"], artifact, environment, log).strip()
        if version != worker["go_version"]:
            raise ValueError("source companion requires the worker's exact compiler version")
        goroot = Path(run_go(["env", "GOROOT"], artifact, environment, log).strip()).resolve()
        if sources.tree_sha(goroot) != worker["go_toolchain_sha256"]:
            raise ValueError("source companion compiler tree differs from the frozen build")
        cli = worker_path / "image-context/bundle/factorybox"
        info = json.loads(run_go(["version", "-m", "-json", str(cli)], artifact, environment, log))
        if info.get("GoVersion") != version.split()[2]:
            raise ValueError("CLI compiler version differs from its source companion")
        modules = linked_modules(info, worker["platform"])
        shadow = artifact / "module"
        shadow.mkdir()
        for name in ["go.mod", "go.sum"]:
            shutil.copyfile(worker_path / "source" / name, shadow / name)
        before = {name: reference.sha(shadow / name) for name in ["go.mod", "go.sum"]}
        entries = {}
        observations = []
        downloads = []
        for module in modules:
            downloaded = json.loads(run_go(
                ["mod", "download", "-json", module["path"] + "@" + module["version"]],
                shadow, environment, log))
            files = downloaded_files(module, downloaded, artifact / "module-cache")
            downloads.append((module, files))
        run_go(["mod", "verify"], shadow, environment, log)
        for index, (module, files) in enumerate(downloads):
            observations.append(add_module(entries, module, files, index, artifact / "notices"))
        run_go(["mod", "verify"], shadow, environment, log)
        if any(files["ZipHash"].read_text().strip() != module["sum"] or
               reference.sha(files["Zip"]) != observation["zipSHA256"]
               for (module, files), observation in zip(downloads, observations, strict=True)):
            raise ValueError("verified dependency ZIP changed during notice collection")
        if before != {name: reference.sha(shadow / name) for name in before}:
            raise ValueError("dependency verification changed the frozen module manifest")
        release_sources.add_tree(entries, "go/src", goroot / "src")
        for name in ["LICENSE", "PATENTS", "VERSION"]:
            release_sources.add_entry(entries, "go/" + name, goroot / name)
        for name in ["go.mod", "go.sum"]:
            release_sources.add_entry(entries, "cli/" + name, shadow / name)
        for name in ["runner.py", "emit.py"]:
            release_sources.add_entry(entries, "tooling/" + name, artifact / name)
        public = {name: {key: value for key, value in row.items() if key != "path"}
                  for name, row in entries.items()}
        manifest = {**{key: value for key, value in result.items() if key != "passed"},
                    "go_version": version, "go_toolchain_sha256": worker["go_toolchain_sha256"],
                    "modules": observations, "members": public}
        reference_lifecycle.write_json(artifact / "cli-dependencies.json", manifest)
        release_sources.add_entry(entries, "cli-dependencies.json", artifact / "cli-dependencies.json")
        reference_lifecycle.write_json(artifact / "inputs.json", entries)
        package = artifact / f"factorybox-{worker['version']}-{worker['platform'].replace('/', '-')}-cli-dependencies.tar"
        with (artifact / "archive.log").open("xb") as output:
            encoded = reference.artifact_storage.archive_stream(
                [sys.executable, str(artifact / "emit.py"), "--emit", str(artifact / "inputs.json")],
                package, reference.ROOT, output, maximum=1 << 30, timeout=600, decoded_limit=4 << 30)
        with subprocess.Popen(["zstd", "-d", "-q", "-c", str(artifact / encoded["encoded"]["file"])],
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE) as decoder:
            try:
                verified = verify_members(decoder.stdout, entries)
                while decoder.stdout.read(1024 * 1024):
                    pass
                if decoder.wait(timeout=600):
                    raise ValueError("source companion did not fully decode")
            except BaseException:
                decoder.kill()
                decoder.wait()
                raise
        if (sources.tree_sha(goroot) != worker["go_toolchain_sha256"] or
                build_worker.load(options.worker_build)[1] != worker):
            raise ValueError("source companion inputs changed during collection")
        result.update(passed=True, modules=len(modules),
                      notices=sum(len(item["notices"]) for item in observations),
                      independently_verified_members=verified,
                      archive_encoding=encoded,
                      source_inventory_sha256=reference.sha(artifact / "cli-dependencies.json"))
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"]}), flush=True)


if __name__ == "__main__":
    main()
