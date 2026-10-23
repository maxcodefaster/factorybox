"""Optional native consumer gateway owner; never part of the Go runtime.

Only a currently owned live sandbox can receive an inference gateway. Every
normal release requires the same helper executable/birth/netns and pidfd exit.
The consumer cannot publish a completed capture until that reply is received.
"""
import json
import os
import re
import stat
import subprocess
import time

import reference
import reference_lifecycle


TOKEN = re.compile(r"[0-9a-f]{32}")
SANDBOX = re.compile(r"[a-z][a-z0-9-]{0,62}")
HELPER = "/provider-gateway"


class Peer:
    def __init__(self, worker, identity, owner, archive, output, helper_sha256, *, effect_only=False):
        self.worker, self.identity, self.owner = worker, identity, owner
        self.archive, self.output = archive / "provider-gateway", output
        self.archive.mkdir()
        self.sha = helper_sha256
        self.effect_only = effect_only
        self.entries, self.requests = {}, {}
        self.closed = False
        self.result = {"owner": owner, "worker_id": identity, "helper_sha256": helper_sha256,
                       "passed": False, "cleanup": False, "gateways": [], "production_qualified": False}

    def exact_worker(self):
        data = json.loads(reference.docker(["container", "inspect", self.identity]))[0]
        if (data["Id"] != self.identity or data["Name"] != "/" + self.worker or
                data["Config"].get("Labels", {}).get("com.factorybox.run") != self.owner or not data["State"]["Running"]):
            raise ValueError("provider target is not the exact live owned worker")
        return data

    def target(self, request):
        target = request.get("target", {})
        if (request.get("schema") != 1 or not TOKEN.fullmatch(request.get("nonce", "")) or
                request.get("phase") not in {"attach", "release"} or request.get("operation") not in {"arm", "finish"} or
                target.get("run_owner") != self.owner or not TOKEN.fullmatch(target.get("owner", "")) or
                not SANDBOX.fullmatch(target.get("sandbox", "")) or type(target.get("pid")) is not int or target["pid"] <= 1):
            raise ValueError("provider request lacks its exact native target")
        self.exact_worker()
        state = json.loads(reference.docker(["exec", self.identity, "/opt/factorybox/factorybox", "--worker-run-root", "/run/factorybox", "state", target["sandbox"]], timeout=5))
        live = state.get("runtime", {})
        record = state["record"]
        if (record["id"] != target["sandbox"] or record["owner"] != target["owner"] or record["runtime_id"] != target.get("runtime_id") or
                record["phase"] != "running" or live.get("status") != "running" or live.get("pid") != target["pid"] or
                live.get("id") != target["runtime_id"] or live.get("created") != target.get("runtime_created")):
            raise ValueError("provider request changed the actual sandbox identity")
        return target

    def command(self, operation, nonce, pid):
        command = [*reference.DOCKER, "exec", self.identity, HELPER, "--operation", operation,
                   "--nonce", nonce, "--sha256", self.sha, "--namespace-of", str(pid)]
        if self.effect_only:
            command += ["--effect-only"]
        if operation == "stop":
            saved = self.entries[nonce]["record"]["identity"]
            command += ["--expected-pid", str(saved["pid"]), "--expected-birth", saved["birth"]]
        return command

    def attach(self, request):
        nonce = request["nonce"]
        if nonce in self.entries:
            raise ValueError("refusing another attach for an existing gateway nonce")
        target = self.target(request)
        command = [*reference.DOCKER, "exec", self.identity, "nsenter", "--net=/proc/" + str(target["pid"]) + "/ns/net", "--",
                   HELPER, "--nonce", nonce, "--sha256", self.sha, "--lifetime-seconds", "110"]
        if self.effect_only:
            command += ["--effect-only"]
        record = {"request": request, "command": command, "released": False}
        self.result["gateways"].append(record)
        stdout = (self.archive / (nonce + ".stdout")).open("x")
        stderr = (self.archive / (nonce + ".stderr")).open("x")
        process = subprocess.Popen(command, stdout=stdout, stderr=stderr)
        entry = {"process": process, "stdout": stdout, "stderr": stderr, "record": record}
        self.entries[nonce] = entry
        deadline = time.monotonic() + 15
        while True:
            if process.poll() is not None:
                raise ValueError("owned consumer gateway exited before readiness")
            done = subprocess.run(self.command("inspect", nonce, target["pid"]), capture_output=True, text=True, timeout=5)
            if done.returncode == 0:
                saved = json.loads(done.stdout)
                break
            if "no such file or directory" not in done.stderr.lower() or time.monotonic() >= deadline:
                raise ValueError("gateway identity verification failed: " + done.stderr[-2048:])
            time.sleep(.05)
        self.target(request)
        endpoint = "127.0.0.1:4098" if self.effect_only else "127.0.0.1:4097"
        if saved["nonce"] != nonce or saved["executable_sha256"] != self.sha or saved["pid"] <= 1 or saved["schema"] != 1 or saved["address"] != endpoint:
            raise ValueError("gateway process differs from its actual frozen helper")
        record["identity"] = saved
        return {"gateway_pid": saved["pid"], "gateway_birth": saved["birth"], "gateway_sha256": self.sha, "gateway_network": saved["network_namespace"]}

    def release(self, request):
        nonce = request["nonce"]
        entry = self.entries.get(nonce)
        if entry is None or entry["record"]["released"]:
            raise ValueError("provider release lacks its active exact attach")
        original = entry["record"]["request"]
        if request != {**original, "phase": "release"}:
            raise ValueError("provider release changed its target or operation")
        target = self.target(request)
        done = subprocess.run(self.command("stop", nonce, target["pid"]), capture_output=True, text=True, timeout=8)
        (self.archive / (nonce + ".stop.stdout")).write_text(done.stdout)
        (self.archive / (nonce + ".stop.stderr")).write_text(done.stderr)
        done.check_returncode()
        stopped = json.loads(done.stdout)
        saved = entry["record"]["identity"]
        if stopped != {"nonce": nonce, "pid": saved["pid"], "stopped": True}:
            raise ValueError("gateway stop did not confirm its exact pidfd exit")
        code = entry["process"].wait(timeout=5)
        entry["stdout"].close()
        entry["stderr"].close()
        entry["record"].update(released=True, exit_code=code)
        if code != 0:
            raise ValueError("gateway did not quiesce before normal release")
        return {"gateway_pid": saved["pid"], "gateway_birth": saved["birth"], "gateway_sha256": self.sha, "gateway_network": saved["network_namespace"]}

    def pump(self):
        if self.closed:
            raise ValueError("provider peer was already closed")
        for path in sorted(self.output.glob("provider-request-*.json")):
            descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
            with os.fdopen(descriptor, "rb") as stream:
                saved = os.fstat(stream.fileno())
                if (not stat.S_ISREG(saved.st_mode) or saved.st_size > 8192 or saved.st_nlink != 1 or
                        stat.S_IMODE(saved.st_mode) != 0o600 or saved.st_uid != self.output.stat().st_uid):
                    raise ValueError("provider request is not a bounded private regular file")
                body = stream.read(8193)
                if len(body) > 8192:
                    raise ValueError("provider request grew beyond its bound")
            request = json.loads(body)
            nonce, phase = request.get("nonce", ""), request.get("phase", "")
            if not TOKEN.fullmatch(nonce) or phase not in {"attach", "release"} or path.name != f"provider-request-{nonce}-{phase}.json":
                raise ValueError("provider request name differs from its exact body")
            key = (nonce, phase)
            if key in self.requests:
                if self.requests[key] != body:
                    raise ValueError("completed provider intent was rewritten")
                continue
            self.requests[key] = body
            reply = {**request, "accepted": False, "stopped": phase == "release"}
            try:
                reply.update(self.attach(request) if phase == "attach" else self.release(request))
                reply["accepted"] = True
            except Exception as error:
                reply["error"] = str(error)
                raise
            finally:
                target = self.output / f"provider-reply-{nonce}-{phase}.json"
                temporary = target.with_suffix(".partial")
                descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                with os.fdopen(descriptor, "w") as stream:
                    json.dump(reply, stream)
                    stream.flush()
                    os.fsync(stream.fileno())
                if target.exists() or target.is_symlink():
                    raise ValueError("refusing to replace a previous provider reply")
                temporary.rename(target)
                reference_lifecycle.write_json(self.archive / "result.json", self.result)

    def close(self, aborted=False):
        if self.closed:
            return
        try:
            for nonce, entry in self.entries.items():
                if not entry["record"]["released"]:
                    if not aborted:
                        raise ValueError("an inference gateway still exists at fixture completion")
                    try:
                        self.release({**entry["record"]["request"], "phase": "release"})
                    except Exception as error:
                        # The owning worker may already have been removed by
                        # failure cleanup. Never signal a PID in another worker.
                        entry["record"]["aborted_cleanup_error"] = str(error)
                        entry["process"].wait(timeout=5)
                    finally:
                        entry["stdout"].close()
                        entry["stderr"].close()
            self.result["cleanup"] = all(entry["process"].poll() is not None for entry in self.entries.values())
            self.result["passed"] = not aborted and self.result["cleanup"] and bool(self.entries) and all(entry["record"]["released"] for entry in self.entries.values())
            self.closed = self.result["cleanup"]
        finally:
            reference_lifecycle.write_json(self.archive / "result.json", self.result)
