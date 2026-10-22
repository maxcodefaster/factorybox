#!/usr/bin/env python3
"""Run unchanged selected ZDTM C assertions with their host-flavor protocol.

The upstream zdtm.py entry point reserves global hugepages even for tests that
do not use them. This adapter instead follows the selected C tests' original
make, daemon-ready, dump/restore, SIGTERM and final PASS protocol. It does not
import zdtm.py, run its cgroup scripts or change a sysctl. Flavor h means the
namespaces of this one owned container, never the worker's PID/net/IPC namespace.
"""
import argparse
import ast
import hashlib
import json
import os
import pathlib
import platform
import re
import select
import signal
import subprocess
import time


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def hugepage_value():
    try:
        return pathlib.Path("/proc/sys/vm/nr_hugepages").read_text().strip()
    except FileNotFoundError:
        return None


def require_platform(target, machine, page_size):
    expected = {"linux/arm64": "aarch64", "linux/amd64": "x86_64"}
    if target not in expected or machine != expected[target] or page_size != 4096:
        raise ValueError("native core platform differs from the explicit 4096-byte-page target")


def verify_descriptor(directory, case):
    name = case["name"]
    if not re.fullmatch(r"[a-z][a-z0-9_-]+", name):
        raise ValueError("invalid selected test name")
    for suffix in [".hook", ".checkskip"]:
        if (directory / (name + suffix)).exists():
            raise ValueError("selected test needs an unadmitted upstream hook: " + name)
    path = directory / (name + ".desc")
    desc = ast.literal_eval(path.read_text()) if path.exists() else {}
    if (desc.keys() - {"flavor", "flags", "opts", "feature"} or
            "h" not in desc.get("flavor", "h ns uns").split() or
            set(desc.get("flags", "").split()) - {"suid", "nolazy", "excl"}):
        raise ValueError("unadmitted upstream descriptor: " + name)
    if desc.get("opts", "").split() != case.get("criu_args", []):
        raise ValueError("selected CRIU arguments differ from upstream: " + name)
    return desc


def require_pass(text):
    lines = text.splitlines()
    if not lines or not re.search(r"(?:^|\s)PASS$", lines[-1]):
        raise ValueError("unchanged upstream test did not end in PASS")
    if any("FAIL" in line or "ERR:" in line for line in lines):
        raise ValueError("upstream test reported failure, including before PASS")


def execute(command, directory, log, timeout=30, environment=None):
    env = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "TMPDIR": "/tmp"}
    env.update(environment or {})
    with log.open("xb") as stream:
        done = subprocess.run(command, cwd=directory, stdout=stream,
                              stderr=subprocess.STDOUT, timeout=timeout,
                              env=env)
    if done.returncode:
        raise ValueError("command failed with exit %d: %s" %
                         (done.returncode, " ".join(map(str, command))))


def wait_exit(pid, descriptor, timeout):
    poll = select.poll()
    poll.register(descriptor, select.POLLIN)
    if not poll.poll(int(timeout * 1000)):
        raise TimeoutError("owned test process did not exit")
    child, status = os.waitpid(pid, 0)
    if child != pid:
        raise ValueError("wait returned another child")
    return os.waitstatus_to_exitcode(status)


def process_tree(pid):
    pending, seen = [pid], set()
    while pending:
        current = pending.pop()
        if current in seen:
            continue
        seen.add(current)
        for thread in pathlib.Path("/proc/%d/task" % current).iterdir():
            pending.extend(int(p) for p in (thread / "children").read_text().split())
    return sorted(seen)


def task_credentials(pid):
    credentials = {}
    for line in pathlib.Path("/proc/%d/status" % pid).read_text().splitlines():
        key, _, value = line.partition(":")
        if key in {"Uid", "Gid", "Groups"}:
            credentials[key] = [int(number) for number in value.split()]
    return credentials


def run_case(case, static, results, cycles):
    name = case["name"]
    directory = results / name
    directory.mkdir()
    row = {"name": name, "passed": False, "cycles": [], "descriptor": verify_descriptor(static, case),
           "binary_sha256": sha(static / name)}
    write_json(directory / "intent.json", row)
    try:
        if row["descriptor"].get("feature"):
            execute(["/criu", "check", "--feature", row["descriptor"]["feature"]],
                    directory, directory / "feature.log")
        environment = {"ZDTM_THREAD_BOMB": "5"}
        if "suid" not in row["descriptor"].get("flags", "").split():
            # Match upstream zdtm_test.start, including supplementary groups
            # and writable output directory, instead of running every C test
            # with root's permissions.
            environment.update(ZDTM_UID="18943", ZDTM_GID="58467", ZDTM_GROUPS="27495 48244")
            directory.chmod(directory.stat().st_mode | 0o222)
        row["test_environment"] = environment
        execute([str(static / name), "--pidfile=test.pid", "--outfile=test.out",
                 *case.get("args", [])], directory, directory / "start.log", environment=environment)
        pid = int((directory / "test.pid").read_text())
        if pid <= 1:
            raise ValueError("test did not publish an owned daemon PID")
        credentials = task_credentials(pid)
        expected = ({"Uid": [18943] * 4, "Gid": [58467] * 4, "Groups": [27495, 48244]}
                    if "ZDTM_UID" in environment else
                    {"Uid": [0] * 4, "Gid": [0] * 4, "Groups": [0]})
        if credentials != expected:
            raise ValueError("test credentials differ from upstream host-flavor admission: " +
                             repr(credentials))
        row["credentials"] = credentials
        for cycle in range(cycles):
            images = directory / ("images-%d" % cycle)
            images.mkdir()
            # PID 1 must reap every killed descendant. A zombie child can retain
            # its dead session leader's PID and make clone3(set_tid) return EEXIST.
            processes = {child: os.pidfd_open(child) for child in process_tree(pid)}
            try:
                birth = pathlib.Path("/proc/%d/stat" % pid).read_text().rsplit(")", 1)[1].split()[19]
                common = ["--images-dir", str(images), "--shell-job", "-v4",
                          *case.get("criu_args", [])]
                started = time.monotonic()
                execute(["/criu", "dump", "--tree", str(pid), "--log-file", "dump.log",
                         *common], directory, directory / ("dump-%d.log" % cycle))
                # Successful ordinary dump must terminate the original, not leave
                # a process whose final PASS could hide a restore that never ran.
                for child, descriptor in processes.items():
                    wait_exit(child, descriptor, 5)
            finally:
                for descriptor in processes.values():
                    os.close(descriptor)
            dumped = time.monotonic()
            execute(["/criu", "restore", "--restore-detached",
                     "--pidfile", str(directory / ("restored-%d.pid" % cycle)),
                     "--log-file", "restore.log", *common],
                    directory, directory / ("restore-%d.log" % cycle))
            restored = int((directory / ("restored-%d.pid" % cycle)).read_text())
            if restored != pid:
                raise ValueError("restored process lost its namespace PID")
            current = pathlib.Path("/proc/%d/stat" % restored).read_text().rsplit(")", 1)[1].split()[19]
            if current == birth:
                raise ValueError("restoration did not replace the original process")
            if task_credentials(restored) != credentials:
                raise ValueError("restoration changed the original test credentials")
            row["cycles"].append({"index": cycle, "pid": pid, "original_birth": birth,
                                  "terminated_original_processes": list(processes),
                                  "restored_birth": current,
                                  "dump_seconds": dumped - started,
                                  "restore_seconds": time.monotonic() - dumped})
            write_json(directory / "result.json", row)
        descriptor = os.pidfd_open(pid)
        try:
            signal.pidfd_send_signal(descriptor, signal.SIGTERM)
            status = wait_exit(pid, descriptor, 15)
        finally:
            os.close(descriptor)
        text = (directory / "test.out").read_text()
        require_pass(text)
        if status:
            raise ValueError("upstream test exited with status %d" % status)
        if (directory / "test.pid").exists():
            raise ValueError("upstream test did not complete its finalizer")
        row["exit_code"] = status
        row["passed"] = True
    except Exception as error:
        row["error"] = str(error)
        raise
    finally:
        write_json(directory / "result.json", row)
        print(json.dumps(row), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--test")
    parser.add_argument("--platform", choices=["linux/arm64", "linux/amd64"], default="linux/arm64")
    options = parser.parse_args()
    if (os.getpid() != 1 or os.environ.get("FACTORYBOX_CRIU_CORE") != "1" or
            pathlib.Path("/proc/1/cgroup").read_text().strip() != "0::/"):
        raise ValueError("requires the owned private-PID/cgroup container")
    require_platform(options.platform, platform.machine(), os.sysconf("SC_PAGE_SIZE"))
    bound = pathlib.Path("/sys/fs/cgroup/memory.max").read_text().strip()
    if not bound.isdecimal() or int(bound) > 3 * 1024**3:
        raise ValueError("requires the declared finite memory limit")
    spec = json.loads(pathlib.Path("/cases.json").read_text())
    cases = spec["cases"]
    if spec["schema"] != 1 or spec["flavor"] != "h" or spec["cycles"] != 2:
        raise ValueError("unexpected upstream-core recipe")
    if options.test:
        cases = [c for c in cases if c["name"] == options.test]
        if len(cases) != 1:
            raise ValueError("unknown diagnostic case")
    # Read-only /proc/sys/vm makes the rejected global-hugepage setup fail closed.
    # Record the existing global value, without attempting to write it.
    report = {"schema": 1, "passed": False, "flavor": "h", "cycles": spec["cycles"],
              "platform": options.platform, "machine": platform.machine(),
              "page_size": os.sysconf("SC_PAGE_SIZE"),
              "kernel": platform.release(), "hugepages_before": hugepage_value(),
              "cases": [], "scope": "Selected unchanged static C tests; direct host-flavor protocol. "
              "No complete ZDTM matrix, namespace flavors or Sysbox workload claim."}
    work = pathlib.Path("/work")
    results = work / "results"
    results.mkdir()
    try:
        if sha(pathlib.Path("/source.tar")) != os.environ["FACTORYBOX_CRIU_SOURCE_SHA256"]:
            raise ValueError("upstream test source transport changed")
        execute(["tar", "-xf", "/source.tar", "-C", str(work)],
                work, work / "extract.log")
        static = work / "zdtm/static"
        for case in cases:
            verify_descriptor(static, case)
        execute(["make", "-C", str(work / "zdtm/lib"), "-j2"],
                work, work / "build-library.log", timeout=60)
        execute(["make", "-C", str(static), "-j2", *[c["name"] for c in cases]],
                work, work / "build-cases.log", timeout=60)
        for case in cases:
            run_case(case, static, results, spec["cycles"])
            report["cases"].append(case["name"])
        report["passed"] = len(report["cases"]) == len(cases)
    except Exception as error:
        report["error"] = str(error)
        raise
    finally:
        report["hugepages_after"] = hugepage_value()
        if report["hugepages_after"] != report["hugepages_before"]:
            report["passed"] = False
            report["error"] = "observed global hugepage value changed during fixture"
        write_json(work / "result.json", report)
        write_json(pathlib.Path("/output/native-result.json"), report)


if __name__ == "__main__":
    main()
