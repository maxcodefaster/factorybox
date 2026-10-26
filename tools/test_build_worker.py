import json
import io
import os
import shutil
from pathlib import Path
import tempfile
import tarfile
import unittest
from unittest.mock import patch

import build_worker
import reference
import sources
import upstream_unit
import artifact_storage
import worker_bundle_transport


def elf(architecture, payload=b""):
    header = bytearray(64)
    header[:7] = b"\x7fELF\x02\x01\x01"
    header[16:18] = (2).to_bytes(2, "little")
    header[18:20] = {"amd64": 62, "arm64": 183}[architecture].to_bytes(2, "little")
    return bytes(header) + payload


class WorkerBuildInputs(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.name = "factorybox-build-worker-" + "d"*32
        self.artifact = self.root/"artifacts"/self.name
        self.bundle = self.artifact/"image-context/bundle"
        self.bundle.mkdir(parents=True)
        source = self.artifact/"source"; source.mkdir()
        (source/"go.mod").write_text("module fixture\n")
        (self.artifact/"source-lock.json").write_text("frozen lock\n")
        (self.artifact/"image-context/Dockerfile").write_text("FROM pinned\n")
        (self.artifact/"factorybox-worker-linux-amd64.tar").write_bytes(b"frozen package")
        (self.bundle/"factorybox").write_bytes(elf("amd64", b"delivered CLI"))
        (self.bundle/"components").mkdir()
        binaries = {}
        for name in ["criu","sysbox-runc","sysbox-fs","sysbox-mgr"]:
            path = self.bundle/"components"/name; path.write_bytes(elf("amd64", name.encode()))
            binaries[name] = reference.sha(path)
        (self.bundle/"components.json").write_text(json.dumps({"schema":1,"binary_sha256":binaries}))
        recipe = {"source_lock_sha256":reference.sha(self.artifact/"source-lock.json"),
                  "go_source_sha256":sources.tree_sha(source),"factorybox_sha256":reference.sha(self.bundle/"factorybox"),
                  "binary_sha256":binaries}
        (self.bundle/"build.json").write_text(json.dumps(recipe))
        result = {"schema":1,"passed":True,"platform":"linux/amd64", "source_context":upstream_unit.context_manifest(source),
                  "bundle_sha256":{str(p.relative_to(self.bundle)):reference.sha(p) for p in self.bundle.rglob("*") if p.is_file()},
                  "dockerfile_sha256":reference.sha(self.artifact/"image-context/Dockerfile"),
                  "package_sha256":reference.sha(self.artifact/"factorybox-worker-linux-amd64.tar"),
                  "image":"owned@sha256:fixture","image_id":"sha256:fixture","layers":["sha256:layer"]}
        (self.artifact/"result.json").write_text(json.dumps(result))
        self.addCleanup(patch.stopall)
        patch.object(reference,"ROOT",self.root).start()
        self.docker = patch.object(reference,"docker",return_value=json.dumps([{
            "Id":"sha256:fixture", "Os":"linux", "Architecture":"amd64",
            "RootFS":{"Layers":["sha256:layer"]}}])).start()

    def test_arm64_package_and_image_use_the_same_explicit_platform(self):
        result_file = self.artifact / "result.json"
        result = json.loads(result_file.read_text())
        result["platform"] = "linux/arm64"
        (self.artifact / "factorybox-worker-linux-amd64.tar").rename(
            self.artifact / "factorybox-worker-linux-arm64.tar")
        for path in [self.bundle / "factorybox", *(self.bundle / "components").iterdir()]:
            path.write_bytes(elf("arm64", path.name.encode()))
        binaries = {p.name: reference.sha(p) for p in (self.bundle / "components").iterdir()}
        (self.bundle / "components.json").write_text(json.dumps({"schema": 1, "binary_sha256": binaries}))
        recipe_path = self.bundle / "build.json"
        recipe = json.loads(recipe_path.read_text())
        recipe.update(platform="linux/arm64", binary_sha256=binaries,
                      factorybox_sha256=reference.sha(self.bundle / "factorybox"))
        recipe_path.write_text(json.dumps(recipe))
        result["bundle_sha256"] = {str(p.relative_to(self.bundle)): reference.sha(p)
                                  for p in self.bundle.rglob("*") if p.is_file()}
        result_file.write_text(json.dumps(result))
        image = {"Id": "sha256:fixture", "Os": "linux", "Architecture": "arm64",
                 "RootFS": {"Layers": ["sha256:layer"]}}
        self.docker.return_value = json.dumps([image])
        self.assertEqual(build_worker.load(self.name)[1]["platform"], "linux/arm64")
        self.assertFalse(build_worker.load(self.name)[1].get("native_lifecycle_qualified", False))
        image["Architecture"] = "amd64"
        self.docker.return_value = json.dumps([image])
        with self.assertRaisesRegex(ValueError, "image identity"):
            build_worker.load(self.name)

    def test_image_only_still_verifies_every_transport_byte_from_the_frozen_bundle(self):
        result_path = self.artifact / "result.json"
        result = json.loads(result_path.read_text())
        raw = worker_bundle_transport.transport(self.bundle)
        (self.artifact / "factorybox-worker-linux-amd64.tar").unlink()
        result.update(image_only=True, package_bytes=raw["bytes"], package_sha256=raw["sha256"])
        result_path.write_text(json.dumps(result))
        self.assertEqual(build_worker.load(self.name)[0], self.artifact)
        for changed in [{"package_bytes": raw["bytes"] + 1},
                        {"package_sha256": "0" * 64}, {"package_encoding": {}}]:
            with self.subTest(changed=changed):
                result_path.write_text(json.dumps({**result, **changed}))
                with self.assertRaisesRegex(ValueError, "image-only transport"):
                    build_worker.load(self.name)

    def test_native_candidate_requires_matching_native_host_and_exact_kernel(self):
        for platform, architecture in [("linux/arm64", "aarch64"), ("linux/amd64", "x86_64")]:
            host = {"OSType": "linux", "Architecture": architecture, "KernelVersion": "7.0.14-test"}
            candidate = build_worker.native_candidate_profile(platform, host)
            self.assertEqual(candidate["platform"], platform)
            self.assertIn("candidateKernel=7.0.14-test", candidate["compile_options"][1])
            with self.assertRaisesRegex(ValueError, "matching Linux"):
                build_worker.native_candidate_profile(platform, {**host, "Architecture": "foreign"})
            with self.assertRaisesRegex(ValueError, "kernel"):
                build_worker.native_candidate_profile(platform, {**host, "KernelVersion": "7.0 -X=foreign"})

    def test_candidate_recipe_cannot_be_silently_qualified_or_changed(self):
        candidate = build_worker.native_candidate_profile("linux/amd64", {
            "OSType": "linux", "Architecture": "x86_64", "KernelVersion": "7.0.14-test"})
        result = {"platform": "linux/amd64", "selected": False, "production_qualified": False,
                  "native_lifecycle_qualified": False, "native_candidate": candidate,
                  "compile_command": ["go", "build", "-trimpath", "-buildvcs=false",
                                      *candidate["compile_options"], "-o", "owned", "./cmd/factorybox"]}
        recipe = {"selected": False, "production_qualified": False, "native_candidate": candidate}
        build_worker.verify_native_candidate(result, recipe)
        for key in ["selected", "production_qualified", "native_lifecycle_qualified"]:
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "unqualified"):
                build_worker.verify_native_candidate({**result, key: True}, recipe)
        with self.assertRaisesRegex(ValueError, "recipe"):
            build_worker.verify_native_candidate(result, {**recipe, "native_candidate": None})
        with self.assertRaisesRegex(ValueError, "compile options"):
            build_worker.verify_native_candidate({**result, "compile_command": []}, recipe)
        with self.assertRaisesRegex(ValueError, "compile options"):
            build_worker.verify_native_candidate({
                **result, "compile_command": [*result["compile_command"][:6],
                                             "-tags=foreign", *result["compile_command"][6:]]}, recipe)
        with self.assertRaisesRegex(ValueError, "recipe"):
            build_worker.verify_native_candidate({k: v for k, v in result.items() if k != "native_candidate"}, recipe)

    def test_false_target_is_rejected_even_when_all_hashes_are_redeclared(self):
        path = self.bundle / "components/criu"
        path.write_bytes(elf("arm64"))
        recipe = json.loads((self.bundle / "build.json").read_text())
        recipe["binary_sha256"]["criu"] = reference.sha(path)
        (self.bundle / "build.json").write_text(json.dumps(recipe))
        (self.bundle / "components.json").write_text(json.dumps({
            "schema": 1, "binary_sha256": recipe["binary_sha256"]}))
        result = json.loads((self.artifact / "result.json").read_text())
        result["bundle_sha256"] = {str(p.relative_to(self.bundle)): reference.sha(p)
                                  for p in self.bundle.rglob("*") if p.is_file()}
        (self.artifact / "result.json").write_text(json.dumps(result))
        with self.assertRaisesRegex(ValueError, "executable does not match linux/amd64"):
            build_worker.load(self.name)

    def test_completed_inputs_and_image(self):
        self.assertEqual(build_worker.load(self.name)[0],self.artifact)
        self.docker.return_value = json.dumps([{"Id":"sha256:other","RootFS":{"Layers":["sha256:layer"]}}])
        with self.assertRaisesRegex(ValueError,"image identity"):
            build_worker.load(self.name)

    @unittest.skipUnless(shutil.which('zstd'), 'encoded transport requires zstd')
    def test_encoded_package_preserves_complete_transport_and_rejects_corruption(self):
        result_file = self.artifact / 'result.json'
        result = json.loads(result_file.read_text())
        # Model publication of a fresh build, not mutation of a closed one.
        result_file.unlink()
        package = self.artifact / 'factorybox-worker-linux-amd64.tar'
        result['package_bytes'] = package.stat().st_size
        receipt = artifact_storage.compact(package, self.root, new_worker_package=True)
        result['package_encoding'] = receipt['encoded']
        result_file.write_text(json.dumps(result))
        self.assertEqual(build_worker.load(self.name)[0], self.artifact)
        encoded = self.artifact / receipt['encoded']['file']
        encoded.write_bytes(encoded.read_bytes() + b'corruption')
        with self.assertRaisesRegex(ValueError, 'encoded payload'):
            build_worker.load(self.name)

    def test_explicit_criu_base_stays_immutable_and_matches_worker_recipe(self):
        component = {"archive": "pinned.tar.gz", "patches": ["original.patch"]}
        lock = {"components": {"criu": component}}
        encoded = json.dumps(lock)
        base = self.artifact/"criu-base-source-lock.json"
        base.write_text(encoded)
        (self.artifact/"source-lock.json").write_text(encoded)
        recipe = json.loads((self.bundle/"build.json").read_text())
        recipe["source_lock_sha256"] = reference.sha(self.artifact/"source-lock.json")
        (self.bundle/"build.json").write_text(json.dumps(recipe))
        result = json.loads((self.artifact/"result.json").read_text())
        result["bundle_sha256"]["build.json"] = reference.sha(self.bundle/"build.json")
        result["criu_base_source_lock_sha256"] = reference.sha(base)
        (self.artifact/"result.json").write_text(json.dumps(result))
        self.assertEqual(build_worker.load(self.name)[0], self.artifact)
        base.write_text(encoded+"\n")
        with self.assertRaisesRegex(ValueError, "frozen CRIU base changed"):
            build_worker.load(self.name)
        base.write_text(encoded)
        changed = {"components": {"criu": {"archive": "changed.tar.gz"}}}
        (self.artifact/"source-lock.json").write_text(json.dumps(changed))
        with self.assertRaisesRegex(ValueError, "CRIU recipe differs"):
            build_worker.load(self.name)

    def test_changed_or_extra_bundle_file(self):
        path = self.bundle/"components/criu"
        original = path.read_bytes()
        path.write_bytes(b"changed")
        with self.assertRaisesRegex(ValueError,"input changed"):
            build_worker.load(self.name)
        path.write_bytes(original)
        (self.bundle/"extra").write_bytes(b"unrecorded")
        with self.assertRaisesRegex(ValueError,"inventory"):
            build_worker.load(self.name)

    def test_shm_patch_recipe_must_match_the_delivered_patch_and_declaration(self):
        patches = self.bundle / "source-patches"
        patches.mkdir()
        (patches / "sysbox-shm-reexec.patch").write_bytes(b"declared ordered source patch\n")
        (patches / "sysbox-shm-reexec.json").write_bytes(b"declared source identities\n")
        declared = {"patch_sha256": reference.sha(patches / "sysbox-shm-reexec.patch"),
                    "source_sha256": reference.sha(patches / "sysbox-shm-reexec.json")}
        recipe = json.loads((self.bundle / "build.json").read_text())
        recipe["sysbox_shm_reexec"] = declared.copy()
        (self.bundle / "build.json").write_text(json.dumps(recipe))
        result = json.loads((self.artifact / "result.json").read_text())
        result["sysbox_shm_reexec"] = declared.copy()
        result["bundle_sha256"] = {str(p.relative_to(self.bundle)): reference.sha(p)
                                  for p in self.bundle.rglob("*") if p.is_file()}
        (self.artifact / "result.json").write_text(json.dumps(result))
        self.assertEqual(build_worker.load(self.name)[0], self.artifact)

        # Re-declaring a changed recipe's file hash cannot substitute a new
        # patch identity for the one bound by the completed worker result.
        recipe["sysbox_shm_reexec"]["patch_sha256"] = "0" * 64
        (self.bundle / "build.json").write_text(json.dumps(recipe))
        result["bundle_sha256"]["build.json"] = reference.sha(self.bundle / "build.json")
        (self.artifact / "result.json").write_text(json.dumps(result))
        with self.assertRaisesRegex(ValueError, "SHM source patch"):
            build_worker.load(self.name)

        # Matching declarations still require the actual delivered patch.
        result["sysbox_shm_reexec"]["patch_sha256"] = "0" * 64
        (self.artifact / "result.json").write_text(json.dumps(result))
        with self.assertRaisesRegex(ValueError, "SHM source patch"):
            build_worker.load(self.name)

    def test_changed_source_or_package(self):
        (self.artifact/"source/extra.go").write_text("package fixture\n")
        with self.assertRaisesRegex(ValueError,"source context"):
            build_worker.load(self.name)
        (self.artifact/"source/extra.go").unlink()
        (self.artifact/"factorybox-worker-linux-amd64.tar").write_bytes(b"changed package")
        with self.assertRaisesRegex(ValueError,"recipe/package"):
            build_worker.load(self.name)

    def test_bundle_symlink_rejected(self):
        (self.bundle/"redirect").symlink_to(self.artifact/"source")
        with self.assertRaisesRegex(ValueError,"inventory"):
            build_worker.load(self.name)

    def test_image_context_transport_is_deterministic_without_rewriting_source_metadata(self):
        context = self.artifact / "image-context"
        first = build_worker.image_context_bytes(context)
        before = {}
        for path in context.rglob("*"):
            os.utime(path, (123456789, 123456789))
            before[path] = path.stat().st_mtime_ns
        self.assertEqual(build_worker.image_context_bytes(context), first)
        self.assertEqual({path: path.stat().st_mtime_ns for path in before}, before)
        with tarfile.open(fileobj=io.BytesIO(first)) as transport:
            self.assertTrue(all(row.mtime == 0 and row.uid == 0 and row.gid == 0
                                for row in transport.getmembers()))
            self.assertEqual(transport.extractfile("bundle/factorybox").read(),
                             (self.bundle / "factorybox").read_bytes())
        (context / "redirect").symlink_to(self.artifact / "source")
        with self.assertRaisesRegex(ValueError, "redirected"):
            build_worker.image_context_bytes(context)


if __name__ == "__main__":
    unittest.main()
