import json
import io
from pathlib import Path
import tempfile
import unittest
import zipfile

import release_cli_sources
import release_sources


def build_info():
    return {
        "Path": "factorybox/cmd/factorybox", "Main": {"Path": "factorybox"},
        "Settings": [{"Key": key, "Value": value} for key, value in
                     [("GOOS", "linux"), ("GOARCH", "arm64"), ("CGO_ENABLED", "0")]],
        "Deps": [{"Path": "example.com/fixture", "Version": "v1.0.0", "Sum": "h1:" + "a" * 43 + "="}],
    }


class CliDependencySources(unittest.TestCase):
    def test_only_exact_embedded_remote_dependencies_for_the_delivered_target_are_collected(self):
        info = build_info()
        self.assertEqual(release_cli_sources.linked_modules(info, "linux/arm64"),
                         [{"path": "example.com/fixture", "version": "v1.0.0", "sum": "h1:" + "a" * 43 + "="}])
        for change in [
                {"Path": "not-the-cli"}, {"Deps": []},
                {"Deps": [*info["Deps"], *info["Deps"]]},
                {"Deps": [{**info["Deps"][0], "Replace": {"Path": "../local"}}]},
                {"Deps": [{**info["Deps"][0], "Sum": ""}]},
                {"Deps": [{**info["Deps"][0], "Path": "../../outside"}]}]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                release_cli_sources.linked_modules({**info, **change}, "linux/arm64")
        with self.assertRaisesRegex(ValueError, "target"):
            release_cli_sources.linked_modules(info, "linux/amd64")

    def test_sources_and_notices_use_actual_go_zip_checksums_without_executing_module_code(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            proxy = root / "proxy/example.com/fixture/@v"
            proxy.mkdir(parents=True)
            files = {"go.mod": b"module example.com/fixture\n\ngo 1.20\n",
                     "LICENSE": b"Original fixture license and copyright\n",
                     "NOTICE.txt": b"Original attribution\n",
                     "fixture.go": b'package fixture\nfunc init() { panic("source must never execute") }\n'}
            (proxy / "v1.0.0.mod").write_bytes(files["go.mod"])
            (proxy / "v1.0.0.info").write_text('{"Version":"v1.0.0","Time":"2020-01-01T00:00:00Z"}')
            (proxy / "list").write_text("v1.0.0\n")
            with zipfile.ZipFile(proxy / "v1.0.0.zip", "w") as archive:
                for name, body in files.items():
                    archive.writestr("example.com/fixture@v1.0.0/" + name, body)
            module = root / "module"
            module.mkdir()
            (module / "go.mod").write_text(
                "module source-test\n\ngo 1.27.1\n\nrequire example.com/fixture v1.0.0\n")
            import os
            environment = {**os.environ, "GOENV": "off", "GOTOOLCHAIN": "local", "GOWORK": "off", "GO111MODULE": "on",
                           "GOPROXY": (root / "proxy").as_uri(), "GOSUMDB": "off",
                           "GOMODCACHE": str(root / "cache")}
            # The fixture proxy is entirely local. Production keeps checksum DB verification on.
            downloaded = json.loads(release_cli_sources.run_go(
                ["mod", "download", "-json", "example.com/fixture@v1.0.0"],
                module, environment, root / "go.log"))
            identity = {"path": "example.com/fixture", "version": "v1.0.0", "sum": downloaded["Sum"]}
            paths = release_cli_sources.downloaded_files(identity, downloaded, root / "cache")
            entries = {}
            observation = release_cli_sources.add_module(entries, identity, paths, 0, root / "notices")
            self.assertEqual(len(observation["notices"]), 2)
            for notice in observation["notices"]:
                self.assertEqual(Path(entries[notice["member"]]["path"]).read_bytes(),
                                 files[Path(notice["moduleMember"]).name])
            release_cli_sources.run_go(["mod", "verify"], module, environment, root / "go.log")
            with self.assertRaisesRegex(ValueError, "embedded checksum"):
                release_cli_sources.downloaded_files(
                    {**identity, "sum": "h1:" + "b" * 43 + "="}, downloaded, root / "cache")
            for changed in [{"Version": "v2.0.0"}, {"Zip": str(proxy / "v1.0.0.zip")}]:
                with self.subTest(changed=changed), self.assertRaises(ValueError):
                    release_cli_sources.downloaded_files(identity, {**downloaded, **changed}, root / "cache")
            # A poisoned local ZIP cannot hide behind an unchanged download receipt/ziphash.
            paths["Zip"].chmod(0o600)
            with zipfile.ZipFile(paths["Zip"], "w") as archive:
                for name, body in files.items():
                    archive.writestr("example.com/fixture@v1.0.0/" + name,
                                     b"changed" if name == "fixture.go" else body)
            with self.assertRaisesRegex(ValueError, "Go source verification failed"):
                release_cli_sources.run_go(["mod", "verify"], module, environment, root / "go.log")

    def test_notice_omission_or_ambiguous_source_members_are_not_silently_accepted(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            for names in [
                    ["example.com/fixture@v1.0.0/file.go"],
                    ["example.com/fixture@v1.0.0/LICENSE", "other/module@v2.0.0/LICENSE"],
                    ["example.com/fixture@v1.0.0/../LICENSE"],
                    ["example.com/fixture@v1.0.0/" + (root / "escaped/LICENSE").as_posix()]]:
                source = root / "fixture.zip"
                with zipfile.ZipFile(source, "w") as archive:
                    for name in names:
                        archive.writestr(name, "verbatim")
                for suffix in [".mod", ".info"]:
                    source.with_suffix(suffix).write_text("fixture")
                with self.subTest(names=names), self.assertRaises(ValueError):
                    release_cli_sources.add_module(
                        {}, {"path": "example.com/fixture", "version": "v1.0.0"},
                        {"Zip": source, "GoMod": source.with_suffix(".mod"),
                         "Info": source.with_suffix(".info")}, 0, root / "notices")

    def test_completed_archive_verification_checks_all_source_bytes_and_missing_members(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            path = root / "LICENSE"
            path.write_bytes(b"verbatim original notice")
            entries = {}
            release_sources.add_entry(entries, "notices/LICENSE", path)
            output = io.BytesIO()
            release_sources.emit(entries, output)
            self.assertEqual(release_cli_sources.verify_members(io.BytesIO(output.getvalue()), entries), 1)
            changed = {"notices/LICENSE": {**entries["notices/LICENSE"], "sha256": "f" * 64}}
            with self.assertRaisesRegex(ValueError, "differs"):
                release_cli_sources.verify_members(io.BytesIO(output.getvalue()), changed)
            with self.assertRaisesRegex(ValueError, "extra"):
                release_cli_sources.verify_members(io.BytesIO(output.getvalue()), {})
            with self.assertRaisesRegex(ValueError, "missing"):
                release_cli_sources.verify_members(io.BytesIO(output.getvalue()),
                    {**entries, "notices/missing": entries["notices/LICENSE"]})


if __name__ == "__main__":
    unittest.main()
