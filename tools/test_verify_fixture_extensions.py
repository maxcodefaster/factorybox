import hashlib
import json
from pathlib import Path
import tempfile
import unittest

import verify_fixture_extensions


def digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


class OrderedFixtureHashes(unittest.TestCase):
    def fixture(self,path):
        (path/"tests").mkdir()
        (path/"tests/changed").write_text("after")
        previous={"files":{"tests/changed":{"before_sha256":digest("original"),"after_sha256":digest("before")}}}
        (path/"tests/previous.json").write_text(json.dumps(previous))
        declaration={"schema":1,"previous_manifest":"tests/previous.json","assertions_preserved":True,"upstream_assertions_modified":False,
                     "baseline_source_manifest_sha256":digest("manifest"),
                     "files":{"tests/changed":{"before_sha256":digest("before"),"after_sha256":digest("after")}}}
        (path/"tests/pilot-fixture-extensions.json").write_text(json.dumps(declaration))
        return declaration

    def save(self,path,declaration):
        (path/"tests/pilot-fixture-extensions.json").write_text(json.dumps(declaration))

    def test_declared_chain_verifies_current_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory); self.fixture(path)
            self.assertEqual(verify_fixture_extensions.verify(path)["ordered_extensions"],1)
            (path/"tests/changed").write_text("undeclared")
            with self.assertRaises(ValueError):verify_fixture_extensions.verify(path)

    def test_extension_cannot_bypass_the_previous_after_hash(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory); value=self.fixture(path)
            value["files"]["tests/changed"]["before_sha256"]=digest("another input")
            self.save(path,value)
            with self.assertRaises(ValueError):verify_fixture_extensions.verify(path)

    def test_frozen_before_bytes_and_original_declaration_are_checked(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory); self.fixture(path)
            baseline=path/"baseline"; (baseline/"context/tests").mkdir(parents=True)
            (baseline/"source-manifest.json").write_text("manifest")
            (baseline/"context/tests/changed").write_text("before")
            (baseline/"context/tests/previous.json").write_bytes((path/"tests/previous.json").read_bytes())
            self.assertTrue(verify_fixture_extensions.verify(path,baseline)["before_files_verified"])
            (baseline/"context/tests/changed").write_text("foreign")
            with self.assertRaises(ValueError):verify_fixture_extensions.verify(path,baseline)

    def test_declared_assertion_weakening_is_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory); value=self.fixture(path)
            value["upstream_assertions_modified"]=True; self.save(path,value)
            with self.assertRaises(ValueError):verify_fixture_extensions.verify(path)

    def production_fixture(self, path):
        self.fixture(path)
        (path/"tests/actor.go").write_text("corrected authority")
        (path/"tests/actor.patch").write_text("ordered one-line protocol fix")
        baseline=path/"artifacts/factorybox-production-baseline-test"
        baseline.mkdir(parents=True)
        original=path/"artifacts/regression/main.go"
        original.parent.mkdir()
        original.write_text("original authority")
        manifest={"tests/actor.go":{"sha256":digest("original authority")}}
        (baseline/"source-manifest.json").write_text(json.dumps(manifest))
        declaration={"schema":1,"previous_manifest":"tests/pilot-fixture-extensions.json",
                     "previous_manifest_sha256":digest((path/"tests/pilot-fixture-extensions.json").read_text()),
                     "baseline_artifact":baseline.name,"baseline_source_manifest_sha256":digest(json.dumps(manifest)),
                     "assertions_preserved":True,"upstream_assertions_modified":False,
                     "files":{"tests/actor.go":{"before_sha256":digest("original authority"),"after_sha256":digest("corrected authority"),
                              "patch":"tests/actor.patch","patch_sha256":digest("ordered one-line protocol fix"),"before_file":"regression/main.go"}}}
        (path/"tests/production-fixture-extensions.json").write_text(json.dumps(declaration))
        return declaration

    def test_production_extension_binds_original_manifest_before_bytes_and_patch(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory);self.production_fixture(path)
            result=verify_fixture_extensions.verify(path)
            self.assertTrue(result["production_before_files_verified"])
            self.assertEqual(result["production_extensions"],1)
            (path/"artifacts/regression/main.go").write_text("changed before")
            with self.assertRaises(ValueError):verify_fixture_extensions.verify(path)
            (path/"artifacts/regression/main.go").write_text("original authority")
            (path/"tests/actor.patch").write_text("undeclared patch")
            with self.assertRaises(ValueError):verify_fixture_extensions.verify(path)

    def test_production_extension_cannot_replace_the_original_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory);self.production_fixture(path)
            previous=json.loads((path/"tests/pilot-fixture-extensions.json").read_text())
            previous["unrelated_field"]="changed without breaking legacy fixtures"
            (path/"tests/pilot-fixture-extensions.json").write_text(json.dumps(previous))
            with self.assertRaises(ValueError):verify_fixture_extensions.verify(path)
