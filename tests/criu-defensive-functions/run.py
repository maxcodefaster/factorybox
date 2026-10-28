"""Check actual patched CRIU helpers with inert filesystem/process services."""
import hashlib
import json
import os
from pathlib import Path
import resource
import subprocess
import sys
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
import reference
import sources


def extract(text, signature):
    marker = signature + "\n{\n"
    if text.count(marker) != 1:
        raise ValueError("missing or ambiguous function: " + signature)
    start = text.index(marker)
    return text[start:text.index("\n}\n", start) + 3]


def bounded():
    resource.setrlimit(resource.RLIMIT_AS, (1 << 30, 1 << 30))
    resource.setrlimit(resource.RLIMIT_CPU, (30, 30))
    resource.setrlimit(resource.RLIMIT_FSIZE, (32 << 20, 32 << 20))
    os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])


def main():
    artifact = reference.new_artifact("factorybox-criu-defensive-functions-" + uuid.uuid4().hex)
    artifact.mkdir()
    result = {"schema": 1, "passed": False, "production_qualified": False,
              "scope": "Extracted patched CRIU functions with inert service stubs; not checkpoint/restore qualification.",
              "unpatched_bounds_code_executed": False,
              "limits": {"cpus": 2, "address_space_bytes": 1 << 30,
                         "cpu_seconds": 30, "file_bytes": 32 << 20}}
    try:
        component = json.loads((ROOT / "upstream/lock.json").read_text())["components"]["criu"]
        sources.validate_inputs(component)
        tree = ROOT / ".cache/sources/criu"
        result["upstream_test_files_verified"] = sources.check_source(
            tree, ROOT / ".cache/downloads" / component["archive"], component)
        proc = (tree / "criu/proc_parse.c").read_text()
        cgroup = (tree / "criu/cgroup.c").read_text()
        # Refuse the unpatched bounds implementation before compilation. This
        # runner never attempts to reproduce an overflow in historical code.
        if "if (nr > *_n)" not in proc or "if (ret < 0 || ret >= rem)" not in cgroup:
            raise ValueError("patched bounds guards are required before execution")
        functions = [extract(cgroup, "static int ctrl_dir_and_opt(CgControllerEntry *ctl, char *dir, int ds, char *opt, int os)"),
                     extract(proc, "int parse_threads(int pid, struct pid **_t, int *_n)"),
                     extract(proc, "int prepare_loginuid(unsigned int value)"),
                     extract(proc, "static int parse_file_lock_buf(char *buf, struct file_lock *fl, bool is_blocked)")]
        result["source_lock_sha256"] = reference.sha(ROOT / "upstream/lock.json")
        result["source_files_sha256"] = {name: reference.sha(tree / name)
                                          for name in ["criu/cgroup.c", "criu/proc_parse.c"]}
        result["function_sha256"] = [hashlib.sha256(f.encode()).hexdigest() for f in functions]
        fixture = Path(__file__).with_name("fixture.c")
        text = fixture.read_text()
        assert text.count("/* @CRIU_FUNCTIONS@ */") == 1
        source = artifact / "functions.c"
        source.write_text(text.replace("/* @CRIU_FUNCTIONS@ */", "\n".join(functions)))
        result["fixture_sha256"] = reference.sha(fixture)
        result["generated_source_sha256"] = reference.sha(source)
        commands = [["cc", "-std=gnu11", "-O2", "-Wall", "-Werror", str(source), "-o", str(artifact / "functions")],
                    [str(artifact / "functions")]]
        result["checks"] = []
        for i, command in enumerate(commands):
            with (artifact / f"{i}.stdout").open("wb") as out, (artifact / f"{i}.stderr").open("wb") as err:
                done = subprocess.run(command, cwd=ROOT, stdout=out, stderr=err,
                                      timeout=30, preexec_fn=bounded)
            result["checks"].append({"command": command, "exit_code": done.returncode})
            done.check_returncode()
        result["executable_sha256"] = reference.sha(artifact / "functions")
        result["passed"] = True
        return 0
    finally:
        (artifact / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps({"evidence": str(artifact), **result}), flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
