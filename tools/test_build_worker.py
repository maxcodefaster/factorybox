import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import build_worker
import reference
import sources
import upstream_unit


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
        (self.bundle/"factorybox").write_bytes(b"delivered CLI")
        (self.bundle/"components").mkdir()
        binaries = {}
        for name in ["criu","sysbox-runc","sysbox-fs","sysbox-mgr"]:
            path = self.bundle/"components"/name; path.write_bytes(name.encode())
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
        self.docker = patch.object(reference,"docker",return_value=json.dumps([{"Id":"sha256:fixture","RootFS":{"Layers":["sha256:layer"]}}])).start()

    def test_completed_inputs_and_image(self):
        self.assertEqual(build_worker.load(self.name)[0],self.artifact)
        self.docker.return_value = json.dumps([{"Id":"sha256:other","RootFS":{"Layers":["sha256:layer"]}}])
        with self.assertRaisesRegex(ValueError,"image identity"):
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


if __name__ == "__main__":
    unittest.main()
