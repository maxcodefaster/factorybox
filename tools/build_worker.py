#!/usr/bin/env python3
"""Build a local unselected worker image from exact completed component inputs."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import uuid

import build_config
import criu_candidate
import native_base
import reference
import reference_lifecycle
import sources
import upstream_unit


def image_context_bytes(context):
    """Normalize transport metadata without touching hardlinked frozen inputs."""
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode="w", format=tarfile.USTAR_FORMAT) as transport:
        for path in sorted(context.rglob("*")):
            if path.is_symlink() or not (path.is_dir() or path.is_file()):
                raise ValueError("image build context contains a redirected or special file")
            info = tarfile.TarInfo(str(path.relative_to(context)))
            info.mtime, info.uid, info.gid = 0, 0, 0
            info.mode = 0o755 if path.is_dir() else 0o644
            if path.is_dir():
                info.type = tarfile.DIRTYPE
                transport.addfile(info)
            else:
                info.size = path.stat().st_size
                with path.open("rb") as stream:
                    transport.addfile(info, stream)
    return output.getvalue()


def load(name):
    if not name.startswith("factorybox-build-worker-") or Path(name).name != name:
        raise ValueError("expected one exact completed worker build")
    artifact = reference.ROOT / "artifacts" / name
    if artifact.is_symlink() or (artifact / "result.json").is_symlink():
        raise ValueError("worker build provenance must not be a symlink")
    result = json.loads((artifact / "result.json").read_text())
    if result.get("schema") != 1 or not result.get("passed"):
        raise ValueError("worker build did not complete")
    architecture = build_config.architecture(result.get("platform"))
    if "criu_base_source_lock_sha256" in result:
        base_lock = artifact / "criu-base-source-lock.json"
        if base_lock.is_symlink() or reference.sha(base_lock) != result["criu_base_source_lock_sha256"]:
            raise ValueError("worker frozen CRIU base changed")
        if (json.loads(base_lock.read_text())["components"]["criu"] !=
                json.loads((artifact / "source-lock.json").read_text())["components"]["criu"]):
            raise ValueError("worker CRIU recipe differs from its frozen base")
    bundle = artifact / "image-context/bundle"
    entries = list(bundle.rglob("*"))
    if any(p.is_symlink() for p in entries) or {str(p.relative_to(bundle)) for p in entries if p.is_file()} != set(result["bundle_sha256"]):
        raise ValueError("worker bundle inventory changed")
    for path, digest in result["bundle_sha256"].items():
        source = artifact / "image-context/bundle" / path
        if source.is_symlink() or reference.sha(source) != digest:
            raise ValueError("completed worker input changed: " + path)
    if upstream_unit.context_manifest(artifact / "source") != result["source_context"]:
        raise ValueError("completed worker source context changed")
    recipe = json.loads((bundle / "build.json").read_text())
    if recipe.get("platform", result["platform"]) != result["platform"]:
        raise ValueError("worker recipe has a different target platform")
    for path in [bundle / "factorybox", *(bundle / "components").iterdir()]:
        build_config.require_elf(path, result["platform"])
    if "sysbox_shm_reexec" in result:
        declared = result["sysbox_shm_reexec"]
        if (recipe.get("sysbox_shm_reexec") != declared or
                result["bundle_sha256"].get("source-patches/sysbox-shm-reexec.patch") != declared["patch_sha256"] or
                result["bundle_sha256"].get("source-patches/sysbox-shm-reexec.json") != declared["source_sha256"]):
            raise ValueError("worker SHM source patch differs from its frozen recipe")
    components = json.loads((bundle / "components.json").read_text())
    package = artifact / f'factorybox-worker-linux-{architecture}.tar'
    if 'package_encoding' in result:
        if type(result.get('package_bytes')) is not int or result['package_bytes'] <= 0:
            raise ValueError('encoded worker package has no exact raw byte bound')
        if not (artifact / result['package_encoding']['file']).exists() and package.with_name(package.name+'.bundle-retention.json').is_file():
            import worker_bundle_transport
            encoded = worker_bundle_transport.verify(artifact,result)
        else:
            encoded = reference.artifact_storage.verify_compacted(package, {
                'file': package.name, 'bytes': result['package_bytes'], 'sha256': result['package_sha256']})
        if encoded != result['package_encoding']:
            raise ValueError('encoded worker package differs from build provenance')
        package_sha256 = result['package_sha256']
    elif not package.exists() and package.with_name(package.name + '.retention.json').is_file():
        import closed_worker_packages
        package_sha256 = closed_worker_packages.retained_sha(package, result['package_sha256'])
    else:
        package_sha256 = reference.sha(package)
    if (recipe["source_lock_sha256"] != reference.sha(artifact / "source-lock.json") or
            recipe["go_source_sha256"] != sources.tree_sha(artifact / "source") or
            recipe["factorybox_sha256"] != result["bundle_sha256"]["factorybox"] or
            components["schema"] != 1 or components["binary_sha256"] != recipe["binary_sha256"] or
            {key.removeprefix("components/"): value for key, value in result["bundle_sha256"].items() if key.startswith("components/")} != recipe["binary_sha256"] or
            reference.sha(artifact / "image-context/Dockerfile") != result["dockerfile_sha256"] or
            package_sha256 != result["package_sha256"]):
        raise ValueError("worker build recipe/package differs from its frozen inputs")
    inspected = json.loads(reference.docker(["image", "inspect", result["image"]]))[0]
    if (inspected["Id"] != result["image_id"] or inspected["RootFS"]["Layers"] != result["layers"] or
            inspected.get("Os") != "linux" or inspected.get("Architecture") != architecture):
        raise ValueError("completed worker image identity changed")
    return artifact, result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--platform", choices=build_config.PLATFORMS, required=True,
                        help="Explicit image and executable target; does not qualify runtime execution")
    parser.add_argument("--native-base-build", required=True)
    parser.add_argument("--sysbox-build", required=True)
    parser.add_argument("--retained-candidate", required=True)
    parser.add_argument("--criu-base-lock", type=Path, help="Exact immutable selected-base lock for the retained CRIU build; current CRIU component must match in full")
    parser.add_argument("--compare", help="Require an identical package and image from a separate no-cache build")
    parser.add_argument("--discard-compiler-scratch", action="store_true", help="After a successful verified build, retain its generated compiler-cache inventory and remove only that new build's optional scratch cache")
    parser.add_argument('--compress-package', action='store_true', help='Publish this new package as a completely verified lossless .tar.zst; completed builds stay unchanged')
    parser.add_argument('--component-input-worker', help='Share byte-identical pinned components from one verified completed worker without changing the build recipe')
    options = parser.parse_args()
    base_image, base = native_base.load(options.native_base_build, candidate_platform=options.platform)
    sysbox_path, sysbox = reference_lifecycle.component_build(options.sysbox_build, "factorybox-build-sysbox-")
    criu_path, criu, _, _ = criu_candidate.load(options.retained_candidate, base_lock=options.criu_base_lock)
    architecture = build_config.require_platform(options.platform, base, sysbox, criu)
    comparison = load(options.compare)[1] if options.compare else None
    component_input = load(options.component_input_worker) if options.component_input_worker else None
    if any(row is not None and row["platform"] != options.platform
           for row in [comparison, component_input[1] if component_input else None]):
        raise ValueError("comparison or shared worker has a different target platform")
    for name, digest in sysbox["binary_sha256"].items():
        path = sysbox_path / "output" / name
        if reference.sha(path) != digest:
            raise ValueError("Sysbox component changed: " + name)
        build_config.require_elf(path, options.platform)
    build_config.require_elf(criu_path / "criu", options.platform)
    owner = uuid.uuid4().hex
    name = "factorybox-build-worker-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source, context = artifact / "source", artifact / "image-context"
    bundle = context / "bundle"
    bundle.mkdir(parents=True)
    shutil.copyfile(__file__, artifact / "runner.py")
    for directory in ["internal", "cmd/factorybox"]:
        shutil.copytree(reference.ROOT / directory, source / directory)
    for file in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / file, source / file)
    shutil.copyfile(reference.ROOT / "build/worker.Dockerfile", context / "Dockerfile")
    shutil.copyfile(reference.ROOT / "upstream/lock.json", artifact / "source-lock.json")
    if options.criu_base_lock:
        shutil.copyfile(options.criu_base_lock, artifact / "criu-base-source-lock.json")
    result = {"schema": 1, "owner": owner, "platform": options.platform, "passed": False,
              "production_qualified": False, "native_lifecycle_qualified": False, "selected": False,
              "source_context": upstream_unit.context_manifest(source),
              "source_lock_sha256": reference.sha(artifact / "source-lock.json"),
              "sysbox_build": options.sysbox_build, "criu_build": options.retained_candidate,
              "native_base_build": options.native_base_build, "native_base_image": base_image,
              "base_packages_sha256": base["packages_sha256"], "comparison": options.compare,
              "dockerfile_sha256": reference.sha(context / "Dockerfile"),
              "scope": "Local candidate image assembly and reproducibility against one immutable native base. "
                       "No installation, publishing, platform or lifecycle qualification. Base includes its pinned "
                       "build/test utilities; this does not qualify a reduced production image."}
    result['component_input_worker'] = options.component_input_worker
    result['shared_component_roles'] = []
    if options.criu_base_lock:
        result["criu_base_source_lock_sha256"] = reference.sha(artifact / "criu-base-source-lock.json")
        if result["criu_base_source_lock_sha256"] != criu["base_source_lock_sha256"]:
            raise ValueError("frozen CRIU base changed while copying")
    print(json.dumps({"building": str(artifact)}), flush=True)
    try:
        env = dict(os.environ, GOOS="linux", GOARCH=architecture, GOAMD64="v1", GOARM64="v8.0",
                   GOEXPERIMENT="", GOFLAGS="",
                   GOWORK="off", CGO_ENABLED="0", GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local",
                   GOCACHE=str(artifact / "go-cache"))
        version = subprocess.check_output(["go", "version"], text=True).strip()
        if not version.startswith("go version go1.27.1 "):
            raise ValueError("worker build requires the pinned Go 1.27.1 toolchain")
        goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
        result["go_version"], result["go_toolchain_sha256"] = version, sources.tree_sha(goroot)
        verified = subprocess.run(["go", "mod", "verify"], cwd=source, env=env, capture_output=True, text=True, timeout=60)
        (artifact / "modules.txt").write_text(verified.stdout + verified.stderr)
        verified.check_returncode()
        compile_command = ["go", "build", "-trimpath", "-buildvcs=false", "-o", str(bundle / "factorybox"), "./cmd/factorybox"]
        result["compile_command"] = compile_command
        with (artifact / "go-build.log").open("x") as log:
            subprocess.run(compile_command, cwd=source, env=env, check=True, stdout=log, stderr=subprocess.STDOUT, timeout=180)
        build_config.require_elf(bundle / "factorybox", options.platform)
        dependencies = subprocess.check_output(["go", "list", "-deps", "./cmd/factorybox"], cwd=source, env=env, text=True, timeout=60).splitlines()
        if any(name.startswith("factorybox/tests/") for name in dependencies):
            raise ValueError("worker acquired a consumer test dependency")
        (artifact / "dependencies.txt").write_text("\n".join(dependencies) + "\n")
        components = bundle / "components"
        components.mkdir()
        expected = {**sysbox["binary_sha256"], "criu": criu["binary_sha256"]}
        if set(expected) != {"criu", "sysbox-runc", "sysbox-fs", "sysbox-mgr"}:
            raise ValueError("incomplete component identity set")
        for name, digest in expected.items():
            original = criu_path / "criu" if name == "criu" else sysbox_path / "output" / name
            if component_input is not None and component_input[1]['bundle_sha256']['components/' + name] == digest:
                original = component_input[0] / 'image-context/bundle/components' / name
                result['shared_component_roles'].append(name)
            reference.artifact_storage.share_frozen_input(original, components / name, reference.ROOT, digest)
        notices = json.loads((reference.ROOT / "upstream/licenses.json").read_text())
        for file, notice in notices.items():
            original = reference.ROOT / file
            if reference.sha(original) != notice["sha256"]:
                raise ValueError("upstream license notice changed")
            target = bundle / "licenses" / Path(file).relative_to("upstream/licenses")
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(original, target)
        shutil.copyfile(reference.ROOT / "upstream/licenses.json", bundle / "licenses.json")
        reference_lifecycle.write_json(bundle / "components.json", {"schema": 1, "binary_sha256": expected})
        recipe = {"schema": 1, "platform": options.platform, "selected": False, "production_qualified": False,
                  "source_lock_sha256": result["source_lock_sha256"], "go_source_sha256": sources.tree_sha(source),
                  "go_version": version, "go_toolchain_sha256": result["go_toolchain_sha256"],
                  "factorybox_sha256": reference.sha(bundle / "factorybox"), "binary_sha256": expected,
                  "sysbox_build": options.sysbox_build, "criu_build": options.retained_candidate,
                  "criu_source_lock_sha256": criu["source_lock_sha256"],
                  "criu_patch_sha256": criu["candidate_patch_sha256"],
                  "base_image": base_image, "base_packages_sha256": base["packages_sha256"]}
        if sysbox.get("shm_startup_reexec"):
            patch = sysbox_path / "shm-reexec.patch"
            proof = sysbox_path / "shm-reexec-source.json"
            if (reference.sha(patch) != sysbox["shm_reexec_patch_sha256"] or
                    reference.sha(proof) != sysbox["shm_reexec_source_sha256"]):
                raise ValueError("component SHM patch provenance changed")
            declaration = json.loads(proof.read_text())
            if (not declaration["original_assertions_unchanged"] or not declaration["vet_enabled"] or
                    declaration["patch_sha256"] != reference.sha(patch) or
                    declaration["after"] != upstream_unit.context_manifest(sysbox_path / "context/sysbox-libs/linuxUtils")):
                raise ValueError("component SHM source differs from its declared ordered patch")
            for file, digest in declaration["before"].items():
                if file.endswith("_test.go") and declaration["after"].get(file) != digest:
                    raise ValueError("component SHM patch changed an original assertion")
            patches = bundle / "source-patches"
            patches.mkdir()
            shutil.copyfile(patch, patches / "sysbox-shm-reexec.patch")
            shutil.copyfile(proof, patches / "sysbox-shm-reexec.json")
            result["sysbox_shm_reexec"] = {"patch_sha256": reference.sha(patch),
                                            "source_sha256": reference.sha(proof)}
            recipe["sysbox_shm_reexec"] = result["sysbox_shm_reexec"]
        reference_lifecycle.write_json(bundle / "build.json", recipe)
        result["bundle_sha256"] = {str(p.relative_to(bundle)): reference.sha(p) for p in sorted(bundle.rglob("*")) if p.is_file()}
        package = artifact / f"factorybox-worker-linux-{architecture}.tar"
        with tarfile.open(package, "w", format=tarfile.USTAR_FORMAT) as transport:
            for path in sorted(bundle.rglob("*")):
                relative = str(path.relative_to(bundle))
                info = tarfile.TarInfo(relative)
                info.mode, info.mtime = (0o700 if path.is_dir() else 0o600), 0
                if path.is_dir():
                    info.type = tarfile.DIRTYPE
                    transport.addfile(info)
                else:
                    info.size = path.stat().st_size
                    if relative == "factorybox" or relative.startswith("components/"):
                        info.mode = 0o755
                    with path.open("rb") as stream:
                        transport.addfile(info, stream)
        result["package_sha256"] = reference.sha(package)
        if options.compress_package:
            result['package_bytes'] = package.stat().st_size
            encoding = reference.artifact_storage.compact(package, reference.ROOT,
                        new_worker_package=True, compression_level=19)
            if encoding['original']['sha256'] != result['package_sha256'] or encoding['original']['bytes'] != result['package_bytes']:
                raise ValueError('new encoded package differs from the exact deterministic transport')
            result['package_encoding'] = encoding['encoded']
        image_name = "factorybox-worker-" + owner
        command = [*reference.DOCKER, "build", "--provenance=false", "--progress=plain", "--network=none",
                   "--no-cache", "--platform=" + options.platform, "--build-arg", "NATIVE_BASE=" + base_image,
                   "--build-arg", "SOURCE_DATE_EPOCH=0", "--tag", image_name,
                   "--output", "type=image,compression=uncompressed",
                   "--metadata-file", str(artifact / "image-metadata.json"), "-"]
        # Docker's image exporter unpacks into its local image store and cannot
        # combine that with rewrite-timestamp. A normalized tar transport gives
        # COPY fixed file timestamps without mutating shared component inputs.
        image_input = image_context_bytes(context)
        result["image_context_tar_sha256"] = hashlib.sha256(image_input).hexdigest()
        result["image_context_tar_bytes"] = len(image_input)
        result["image_command"] = command
        reference_lifecycle.write_json(artifact / "intent.json", result)
        with (artifact / "image-build.log").open("x") as log:
            subprocess.run(command, input=image_input, check=True, stdout=log, stderr=subprocess.STDOUT, timeout=180)
        metadata = json.loads((artifact / "image-metadata.json").read_text())
        result["image"] = image_name + "@" + metadata["containerimage.digest"]
        inspected = json.loads(reference.docker(["image", "inspect", result["image"]]))[0]
        if inspected["Os"] != "linux" or inspected["Architecture"] != architecture:
            raise ValueError("built worker image has a different target platform")
        result["image_id"], result["layers"] = inspected["Id"], inspected["RootFS"]["Layers"]
        if upstream_unit.context_manifest(source) != result["source_context"]:
            raise ValueError("worker Go inputs changed")
        if comparison and any(result[key] != comparison[key] for key in ["package_sha256", "image_id", "layers"]):
            raise ValueError("independent worker package/image differs from the comparison")
        if options.discard_compiler_scratch:
            cache = artifact / "go-cache"
            cache_manifest = upstream_unit.context_manifest(cache)
            reference_lifecycle.write_json(artifact / "compiler-scratch.json", {
                "schema": 1, "scope": "Only this successful build's optional generated compiler scratch. Source, runnable bundle, package bytes and image remain available; a declared encoded transport has its full reconstruction receipt.",
                "cache_manifest": cache_manifest, "reconstruction": "Offline fresh compilation from preserved frozen sources with the exact pinned toolchain.",
                "package_sha256": result["package_sha256"], "image_id": result["image_id"]})
            if upstream_unit.context_manifest(cache) != cache_manifest:
                raise ValueError("new compiler scratch changed before disposal")
            shutil.rmtree(cache)
            result["discarded_compiler_scratch_sha256"] = reference.sha(artifact / "compiler-scratch.json")
        result["passed"] = True
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "error": result.get("error"),
                          "image": result.get("image"), "package_sha256": result.get("package_sha256")}), flush=True)


if __name__ == "__main__":
    main()
