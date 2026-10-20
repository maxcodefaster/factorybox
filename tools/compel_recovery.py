#!/usr/bin/env python3
"""Prove retained Compel context recovery in an owned native fixture."""
import argparse
import datetime
import json
import shutil
import subprocess
import uuid

import native_base
import reference
import sources

ROOT = reference.ROOT
REPEATS = 8
SCOPE = (
    "One 32-MiB C actor and actual killed Compel tracer. Compare no retained "
    "context with a forked C process retaining the live ctl/RPC socket. "
    "Eight freezer repeats, eight three-thread repeats and eight outstanding-RPC "
    "repeats require original PID/RAM/FP/descriptor state and useful work, "
    "default unblocked SIGPIPE, removal of injected mapping, and a frozen "
    "source after cure before the coordinator permits useful work. "
    "No Sysbox, real page-dump RPC, TCP, multiple processes or full lifecycle qualification."
)


def observations(archive):
    def read(path):
        row = {}
        if path.exists():
            for line in path.read_text().splitlines():
                if line.startswith(('{"guardian":', '{"frozen_guardian":')):
                    row.update(json.loads(line))
        return row
    return {
        "observations": {mode: read(archive / (mode + ".log")) for mode in ["baseline", "guardian"]},
        **{prefix + "_repeats": [
            read(archive / f"{prefix}-guardian-{index}.log") for index in range(1, REPEATS + 1)]
           for prefix in ["frozen", "threaded", "busy"]},
    }


def complete_source(row):
    return (
        row.get("guardian") is True and row.get("original_pid", 0) > 1 and
        row.get("ram_preserved") is True and row.get("useful_work") is True and
        row.get("fp_preserved") is True and row.get("descriptors_preserved") is True and
        row.get("sigpipe_default_unblocked") is True and
        row.get("injected_maps_before", 0) > 0 and row.get("injected_maps_after") == 0)


def qualified(result):
    baseline = result["observations"]["baseline"]
    guardian = result["observations"]["guardian"]
    return bool(
        result.get("exit_code") == 0 and not result["container_state"]["OOMKilled"] and
        result.get("outcomes") == {
            "baseline_exit": 1, "guardian_exit": 0, "frozen_exit": 0,
            "threaded_exit": 0, "busy_exit": 0} and
        baseline.get("guardian") is False and baseline.get("injected_maps_before", 0) > 0 and
        baseline.get("injected_maps_after", 0) > 0 and
        baseline.get("ram_preserved") is True and baseline.get("useful_work") is True and
        complete_source(guardian) and
        all(complete_source(row) and row.get("held_after_cure") is True and row.get("threads") == 1
            for row in result["frozen_repeats"]) and
        all(complete_source(row) and row.get("held_after_cure") is True and
            row.get("threads") == 3 and row.get("background_work") is True
            for row in result["threaded_repeats"]) and
        all(complete_source(row) and row.get("held_after_cure") is True and
            row.get("threads") == 3 and row.get("background_work") is True and
            row.get("busy_rpc") is True for row in result["busy_repeats"]) and
        result.get("source_unchanged") and result.get("inputs_unchanged"))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native-base-build", required=True)
    options = parser.parse_args()
    lock = ROOT / "upstream/lock.json"
    component = json.loads(lock.read_text())["components"]["criu"]
    recipe_path = ROOT / "upstream/criu-build.json"
    recipe = json.loads(recipe_path.read_text())
    if recipe["source_recipe_sha256"] != sources.recipe_sha(component):
        raise ValueError("CRIU source recipe changed")
    sources.validate_inputs(component)
    source = ROOT / ".cache/sources/criu"
    source_sha = sources.tree_sha(source)
    unchanged_tests = sources.check_source(
        source, ROOT / ".cache/downloads" / component["archive"], component)
    image, base = native_base.load(options.native_base_build)
    owner = uuid.uuid4().hex
    name = "factorybox-compel-recovery-" + owner
    archive = reference.new_artifact(name)
    archive.mkdir()
    for source_file, target in [
        (ROOT / "tests/compel-recovery/probe.c", "recovery.c"),
        (ROOT / "tests/compel-recovery/parasite.c", "parasite.c"),
        (ROOT / "tests/private-cgroup-delegation.sh", "delegate.sh"),
        (lock, "source-lock.json"), (recipe_path, "criu-build.json"),
        (ROOT / "tools/compel_recovery.py", "runner.py"),
    ]:
        shutil.copyfile(source_file, archive / target)
    inputs = {p.name: reference.sha(p) for p in archive.iterdir() if p.is_file()}
    command = [
        *reference.DOCKER, "run", "--pull=never", "--name", name,
        "--label", "com.factorybox.run=" + owner, "--cpus=3", "--memory=3g",
        "--pids-limit=64", "--network=none", "--read-only", "--cgroupns=private",
        "--privileged", "--env", "FACTORYBOX_UPSTREAM_CONTAINER=1",
        "--env", "FACTORYBOX_COMPEL_RECOVERY=1",
        "--env", "SOURCE_DATE_EPOCH=" + str(recipe["source_date_epoch"]),
        "--security-opt=no-new-privileges", "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=384m",
        "--mount", f"type=bind,source={source},target=/source,readonly",
        "--mount", f"type=bind,source={archive},target=/output",
        "--entrypoint", "/bin/bash", image, "-ec",
        "mount --make-rprivate /\n"
        ". /output/delegate.sh\n"
        "cc --version > /output/toolchain.txt\n"
        "cp -R /source /tmp/source\n"
        "make -C /tmp/source -j2 compel/compel-host > /output/build.log 2>&1\n"
        "mkdir /tmp/probe\n"
        "cd /tmp/probe\n"
        "cp /output/recovery.c /output/parasite.c .\n"
        "factorybox_compel=/tmp/source/compel/compel-host\n"
        "cc -O2 -g -Wall -Werror -c $($factorybox_compel cflags) -o parasite.o parasite.c >> /output/build.log 2>&1\n"
        "ld $($factorybox_compel ldflags) -o parasite.po parasite.o $($factorybox_compel plugins) >> /output/build.log 2>&1\n"
        "$factorybox_compel hgen -o parasite.h -f parasite.po >> /output/build.log 2>&1\n"
        "cc -O2 -g -Wall -Wextra -Werror -pthread $($factorybox_compel includes) "
        "-o recovery recovery.c $($factorybox_compel --static libs) >> /output/build.log 2>&1\n"
        "cp recovery /output/recovery\n"
        "set +e\n"
        "timeout 20 ./recovery baseline > /output/baseline.log 2>&1\n"
        "baseline=$?\n"
        "timeout 20 ./recovery guardian > /output/guardian.log 2>&1\n"
        "guardian=$?\n" +
        "".join(
            f"{prefix}=0\n"
            f"for index in $(seq 1 {REPEATS}); do\n"
            f"  timeout 20 ./recovery {mode} > \"/output/{prefix}-guardian-$index.log\" 2>&1\n"
            f"  {prefix}=$?\n"
            f"  if test \"${prefix}\" != 0; then break; fi\n"
            "done\n"
            for prefix, mode in [
                ("frozen", "frozen-guardian"), ("threaded", "threaded-guardian"), ("busy", "busy-guardian")]) +
        "printf '{\"baseline_exit\":%d,\"guardian_exit\":%d,\"frozen_exit\":%d,"
        "\"threaded_exit\":%d,\"busy_exit\":%d}\\n' "
        "\"$baseline\" \"$guardian\" \"$frozen\" \"$threaded\" \"$busy\" > /output/outcomes.json\n"
        "test \"$baseline\" = 1 && test \"$guardian\" = 0 && test \"$frozen\" = 0 "
        "&& test \"$threaded\" = 0 && test \"$busy\" = 0",
    ]
    result = {
        "schema": 1, "owner": owner, "command": command, "passed": False, "cleanup": False,
        "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_sha256": source_sha, "inputs_sha256": inputs, "scope": SCOPE,
        "native_base_build": options.native_base_build, "image": image,
        "packages_sha256": base["packages_sha256"], "unchanged_upstream_tests": unchanged_tests,
        "production_qualified": False,
    }
    (archive / "intent.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"running": str(archive)}), flush=True)
    try:
        with (archive / "stdout.txt").open("x") as stdout, (archive / "stderr.txt").open("x") as stderr:
            done = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=240)
        result["exit_code"] = done.returncode
        result["container_state"] = json.loads(reference.docker(["container", "inspect", name]))[0]["State"]
        if (archive / "outcomes.json").exists():
            result["outcomes"] = json.loads((archive / "outcomes.json").read_text())
        result.update(observations(archive))
        result["source_unchanged"] = sources.tree_sha(source) == source_sha
        result["inputs_unchanged"] = all(reference.sha(archive / p) == digest for p, digest in inputs.items())
        if (archive / "recovery").exists():
            result["binary_sha256"] = reference.sha(archive / "recovery")
        result["passed"] = qualified(result)
    except Exception as error:
        result["error"] = str(error)
    finally:
        try:
            reference.owned_delete("container", name, owner)
            result["cleanup"] = True
        except Exception as error:
            result["cleanup_error"] = str(error)
        result["passed"] = result["passed"] and result["cleanup"]
        (archive / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(archive), "passed": result["passed"],
                          "cleanup": result["cleanup"]}), flush=True)
    if not result["passed"]:
        raise RuntimeError("retained Compel mechanism proof failed: " + str(archive))


if __name__ == "__main__":
    main()
