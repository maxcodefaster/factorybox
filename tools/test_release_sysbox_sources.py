import copy
import hashlib
from pathlib import Path
import tempfile
import unittest
import zipfile

import release_cli_sources
import release_sysbox_sources


def build_info(role="sysbox-runc", arch="arm64"):
    module, package = release_sysbox_sources.ROLES[role]
    return {
        "Path": package, "Main": {"Path": module}, "GoVersion": "go1.27.1",
        "Settings": [{"Key": key, "Value": value} for key, value in
                     [("GOOS", "linux"), ("GOARCH", arch), ("CGO_ENABLED", "1")]],
        "Deps": [
            {"Path": "example.com/remote", "Version": "v1.0.0", "Sum": "h1:" + "a" * 43 + "="},
            {"Path": "example.com/local",
             "Replace": {"Path": "../sysbox-libs", "Version": "(devel)"}},
        ],
    }


class SysboxDependencySources(unittest.TestCase):
    def test_role_platform_compiler_and_dependency_identities_are_bound_to_delivered_binaries(self):
        for role in release_sysbox_sources.ROLES:
            for arch in ["amd64", "arm64"]:
                with self.subTest(role=role, arch=arch):
                    remote, local = release_sysbox_sources.dependencies(
                        build_info(role, arch), role, "linux/" + arch, "go1.27.1")
                    self.assertEqual(remote, [
                        {"path": "example.com/remote", "version": "v1.0.0", "sum": "h1:" + "a" * 43 + "="}])
                    self.assertEqual(local, [
                        {"path": "example.com/local", "replacement": "../sysbox-libs"}])
        info = build_info()
        changes = [
            {"Path": "unrelated/package"}, {"Main": {"Path": "unrelated/module"}},
            {"GoVersion": "go1.27.2"}, {"Deps": []},
            {"Deps": [info["Deps"][0]]}, {"Deps": [info["Deps"][1]]},
            {"Deps": [*info["Deps"], info["Deps"][0]]},
            {"Deps": [{**info["Deps"][0], "Sum": ""}, info["Deps"][1]]},
            {"Deps": [{**info["Deps"][0], "Path": "../../outside"}, info["Deps"][1]]},
            {"Deps": [info["Deps"][0], {"Path": "example.com/local",
                                      "Replace": {"Path": "/outside", "Version": "(devel)"}}]},
            {"Deps": [info["Deps"][0], {"Path": "example.com/local",
                                      "Replace": {"Path": "../local", "Version": "v1.0.0"}}]},
        ]
        for change in changes:
            with self.subTest(change=change), self.assertRaises(ValueError):
                release_sysbox_sources.dependencies(
                    {**info, **change}, "sysbox-runc", "linux/arm64", "go1.27.1")
        for key, value in [("GOOS", "darwin"), ("GOARCH", "amd64"), ("CGO_ENABLED", "0")]:
            changed = copy.deepcopy(info)
            next(row for row in changed["Settings"] if row["Key"] == key)["Value"] = value
            with self.subTest(setting=key), self.assertRaises(ValueError):
                release_sysbox_sources.dependencies(
                    changed, "sysbox-runc", "linux/arm64", "go1.27.1")

    def test_local_replacements_bind_exact_manifests_and_distinguish_pristine_runc(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            context, pristine = root / "context", root / "pristine"
            for directory in [context / role for role in release_sysbox_sources.ROLES] + [
                    context / "sysbox-libs", pristine]:
                directory.mkdir(parents=True)
                (directory / "go.mod").write_text("module owned-fixture\n")
                (directory / "source.go").write_text("package owned\n")
            (pristine / "source.go").write_text("package pristine\n")
            row = {"path": "example.com/local", "replacement": "../sysbox-runc"}
            declarations = [{"Old": {"Path": row["path"]},
                             "New": {"Path": row["replacement"]}}]
            for role in ["sysbox-fs", "sysbox-mgr"]:
                binding = release_sysbox_sources.bind_local(
                    row, role, context, pristine, declarations)
                self.assertEqual(binding["sourcePrefix"], "sysbox/pristine-runc")
                self.assertEqual(binding["variant"], "pristine-runc")
                self.assertEqual(binding["sha256"],
                                 release_sysbox_sources.tree_identity(pristine)["sha256"])
                self.assertNotEqual(binding["sha256"],
                                    release_sysbox_sources.tree_identity(context / "sysbox-runc")["sha256"])
            row = {**row, "replacement": "../sysbox-libs"}
            declarations[0]["New"]["Path"] = row["replacement"]
            binding = release_sysbox_sources.bind_local(
                row, "sysbox-runc", context, pristine, declarations)
            self.assertEqual(binding["sourcePrefix"], "sysbox/context/sysbox-libs")
            self.assertEqual(binding["variant"], "frozen-context")
            for invalid in [[], declarations * 2,
                            [{"Old": {"Path": row["path"]},
                              "New": {"Path": row["replacement"], "Version": "v1.0.0"}}]]:
                with self.subTest(declarations=invalid), self.assertRaises(ValueError):
                    release_sysbox_sources.bind_local(
                        row, "sysbox-runc", context, pristine, invalid)
            outside = root / "outside"
            outside.mkdir()
            (outside / "go.mod").write_text("module outside\n")
            (context / "escape").symlink_to(outside, target_is_directory=True)
            for replacement in ["../../outside", "../escape", "../missing"]:
                with self.subTest(replacement=replacement), self.assertRaises(ValueError):
                    release_sysbox_sources.bind_local(
                        {**row, "replacement": replacement}, "sysbox-runc", context, pristine,
                        [{"Old": {"Path": row["path"]}, "New": {"Path": replacement}}])

    def test_compiler_sources_require_the_frozen_digest_recipe(self):
        image = "golang@sha256:" + "a" * 64
        self.assertEqual(release_sysbox_sources.compiler_image(
            "FROM " + image + " AS go\nRUN true\n"), image)
        for recipe in [
                "FROM golang:1.27 AS go\n", "FROM " + image + " AS tools\n",
                "# changed\nFROM " + image + " AS go\n"]:
            with self.subTest(recipe=recipe), self.assertRaises(ValueError):
                release_sysbox_sources.compiler_image(recipe)

    def test_nonstandard_notices_require_exact_module_and_verbatim_file_checksums(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            module = {"path": "example.com/fixture", "version": "v1.0.0", "sum": "h1:" + "a" * 43 + "="}
            contents = {"README.md": b"Original ISC license text\n",
                        "service.go": b"// Original copyright\npackage fixture\n"}
            source = root / "module.zip"
            with zipfile.ZipFile(source, "w") as archive:
                for name, body in contents.items():
                    archive.writestr("example.com/fixture@v1.0.0/" + name, body)
            files = {"Zip": source, "GoMod": root / "go.mod", "Info": root / "module.info"}
            files["GoMod"].write_text("module example.com/fixture\n")
            files["Info"].write_text('{"Version":"v1.0.0"}\n')
            declaration = {**module, "files": {
                name: hashlib.sha256(body).hexdigest() for name, body in contents.items()}}
            with self.assertRaisesRegex(ValueError, "no observed named notice"):
                release_cli_sources.add_module({}, module, files, 0, root / "notices")
            entries = {}
            observation = release_cli_sources.add_module(
                entries, module, files, 0, root / "notices", declaration)
            self.assertEqual(len(observation["notices"]), 2)
            for notice in observation["notices"]:
                self.assertEqual(Path(entries[notice["member"]]["path"]).read_bytes(),
                                 contents[Path(notice["moduleMember"]).name])
            for changed in [
                    {**declaration, "sum": "h1:" + "b" * 43 + "="},
                    {**declaration, "version": "v2.0.0"},
                    {**declaration, "files": {"README.md": "0" * 64}},
                    {**declaration, "files": {"absent.md": "0" * 64}}]:
                with self.subTest(declaration=changed), self.assertRaises(ValueError):
                    release_cli_sources.add_module(
                        {}, module, files, 0, root / "notices", changed)


if __name__ == "__main__":
    unittest.main()
