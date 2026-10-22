#!/usr/bin/env python3
"""Prove the optional consumer gateway's namespace join and exact stop.

Uses synthetic HTTP through an owned Unix socket. No external inference,
checkpoint/restore, Sysbox guest qualification or runtime selection is claimed.
"""
import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import shutil
import socketserver
import subprocess
import tempfile
import threading
import time
import uuid

import build_worker
import consumer_worker_loss
import reference
import reference_lifecycle
import upstream_unit


class SyntheticRelay(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        size = int(self.headers.get("Content-Length", "0"))
        if self.path != "/v1/chat/completions" or not 0 < size <= 8192:
            self.send_error(400)
            return
        body = self.rfile.read(size)
        self.server.requests.append({"body": body.decode(), "sha256": hashlib.sha256(body).hexdigest()})
        response = json.dumps({"receipt": self.server.receipt, "body_sha256": hashlib.sha256(body).hexdigest()}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--worker-build", required=True)
    options = parser.parse_args()
    _, worker = build_worker.load(options.worker_build)
    owner = uuid.uuid4().hex
    name = "factorybox-provider-gateway-" + owner
    artifact = reference.new_artifact(name)
    artifact.mkdir()
    source = artifact / "source"
    shutil.copytree(reference.ROOT / "tests/provider-gateway", source / "tests/provider-gateway")
    for file in ["go.mod", "go.sum"]:
        shutil.copyfile(reference.ROOT / file, source / file)
    shutil.copyfile(__file__, artifact / "runner.py")
    result = {"schema": 1, "owner": owner, "passed": False, "cleanup": False, "production_qualified": False,
              "lifecycle_qualified": False, "worker_build": options.worker_build, "worker_image_id": worker["image_id"],
              "source_context": upstream_unit.context_manifest(source), "scope": __doc__}
    binary = artifact / "provider-gateway"
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
    command = ["go", "build", "-trimpath", "-buildvcs=false", "-o", str(binary), "./tests/provider-gateway"]
    result["build_command"] = command
    try:
        with (artifact / "build.stdout").open("x") as out, (artifact / "build.stderr").open("x") as err:
            subprocess.run(command, cwd=source, env=env, stdout=out, stderr=err, check=True, timeout=120)
    except Exception as error:
        result.update(error=str(error), cleanup=True)
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": False, "error": result["error"]}), flush=True)
        return 1
    digest = result["binary_sha256"] = reference.sha(binary)
    attempted = False
    holder = gateway = None
    before = consumer_worker_loss.sysctls()
    try:
        with tempfile.TemporaryDirectory(prefix="fbp-") as directory:
            socket = Path(directory) / "provider.sock"
            with SyntheticRelay(str(socket), Handler) as relay:
                relay.receipt, relay.requests = uuid.uuid4().hex, []
                os.chmod(socket, 0o666)
                thread = threading.Thread(target=relay.serve_forever, daemon=True)
                thread.start()
                try:
                    reference.docker(["volume", "create", "--label", "com.factorybox.run=" + owner, name])
                    attempted = True
                    command = ["run", "--detach", "--pull=never", "--name", name, "--label", "com.factorybox.run=" + owner,
                               "--privileged", "--cgroupns=private", "--cpus=3", "--memory=3g", "--pids-limit=128", "--network=none",
                               "--env", "GOMAXPROCS=2", "--env", "GOMEMLIMIT=256MiB",
                               "--mount", f"type=volume,source={name},target=/var/lib/sysbox",
                               "--mount", f"type=bind,source={directory},target=/external-provider,readonly",
                               "--mount", f"type=bind,source={binary},target=/provider-gateway,readonly",
                               "--entrypoint", "/opt/factorybox/factorybox", worker["image"], "worker", "--store-id", owner]
                    result["worker_command"] = command
                    identity = reference.docker(command)
                    result["worker_id"] = identity
                    result["readiness"] = consumer_worker_loss.ready(name, owner, identity, time.monotonic() + 70)
                    print(json.dumps({"running": str(artifact)}), flush=True)
                    with (artifact / "holder.stdout").open("x") as out, (artifact / "holder.stderr").open("x") as err:
                        holder_command = [*reference.DOCKER, "exec", identity, "unshare", "--user", "--map-root-user", "--net", "--",
                                          "/bin/sh", "-ec", 'ip link set lo up\nprintf "%s\\n" "$$"\nexec sleep 110']
                        result["holder_command"] = holder_command
                        holder = subprocess.Popen(holder_command, stdout=out, stderr=err)
                        deadline = time.monotonic() + 5
                        while True:
                            data = (artifact / "holder.stdout").read_text().strip()
                            if data.isdecimal() and int(data) > 1:
                                target = int(data)
                                break
                            if holder.poll() is not None or time.monotonic() >= deadline:
                                raise ValueError("owned user/network namespace holder did not become ready")
                            time.sleep(.05)
                        nonce = uuid.uuid4().hex
                        serve = [*reference.DOCKER, "exec", identity, "nsenter", "--net=/proc/" + str(target) + "/ns/net", "--",
                                 "/provider-gateway", "--nonce", nonce, "--sha256", digest]
                        result["serve_command"] = serve
                        with (artifact / "gateway.stdout").open("x") as gateway_out, (artifact / "gateway.stderr").open("x") as gateway_err:
                            gateway = subprocess.Popen(serve, stdout=gateway_out, stderr=gateway_err)
                            inspect = [*reference.DOCKER, "exec", identity, "/provider-gateway", "--nonce", nonce,
                                       "--sha256", digest, "--namespace-of", str(target), "--operation", "inspect"]
                            deadline = time.monotonic() + 10
                            while True:
                                checked = subprocess.run(inspect, capture_output=True, text=True, timeout=5)
                                if checked.returncode == 0:
                                    saved = json.loads(checked.stdout)
                                    break
                                if gateway.poll() is not None or time.monotonic() >= deadline:
                                    raise ValueError("namespace gateway did not verify: " + checked.stderr[-2048:])
                                time.sleep(.05)
                            result["gateway"] = saved
                            client = """import http.client,json
c=http.client.HTTPConnection('127.0.0.1',4097,timeout=3)
c.request('POST','/v1/chat/completions',body='{"synthetic":true}',headers={'Connection':'keep-alive','Content-Type':'application/json'})
r=c.getresponse(); body=r.read(); assert r.status==200 and r.will_close and c.sock is None
print(body.decode())
c.close()
"""
                            client_command = [*reference.DOCKER, "exec", identity, "nsenter", "--net=/proc/" + str(target) + "/ns/net", "--", "python3", "-c", client]
                            for attempt in range(2):
                                response = subprocess.run(client_command, capture_output=True, text=True, check=True, timeout=5)
                                observed = json.loads(response.stdout)
                                if observed != {"receipt": relay.receipt, "body_sha256": hashlib.sha256(b'{"synthetic":true}').hexdigest()}:
                                    raise ValueError("synthetic gateway changed its actual request/response")
                                (artifact / f"request-{attempt}.json").write_text(response.stdout)
                                if attempt == 0:
                                    refused = subprocess.run([*inspect[:-1], "stop", "--expected-pid", str(saved["pid"]), "--expected-birth", "wrong"], capture_output=True, text=True, timeout=5)
                                    (artifact / "wrong-birth.stdout").write_text(refused.stdout)
                                    (artifact / "wrong-birth.stderr").write_text(refused.stderr)
                                    result["foreign_birth_refused"] = refused.returncode != 0 and "refusing to signal" in refused.stderr
                                    if not result["foreign_birth_refused"] or gateway.poll() is not None:
                                        raise ValueError("wrong-birth stop was accepted or harmed the original helper")
                            stopped = subprocess.run([*inspect[:-1], "stop", "--expected-pid", str(saved["pid"]), "--expected-birth", saved["birth"]], capture_output=True, text=True, check=True, timeout=8)
                            result["stopped"] = json.loads(stopped.stdout)
                            if result["stopped"] != {"nonce": nonce, "pid": saved["pid"], "stopped": True} or gateway.wait(timeout=5) != 0:
                                raise ValueError("original gateway did not exit cleanly through its verified pidfd")
                    result["relay_requests"] = relay.requests
                    if len(relay.requests) != 2:
                        raise ValueError("synthetic requests were repeated or omitted")
                    reference.docker(["container", "stop", "--time=15", identity], timeout=20)
                    state = consumer_worker_loss.exact_worker(name, owner, identity)["State"]
                    if state["Running"] or state["Pid"] != 0 or state["ExitCode"] != 0 or state["OOMKilled"]:
                        raise ValueError("owned worker did not fully exit")
                    holder.wait(timeout=5)
                    result["passed"] = True
                finally:
                    relay.shutdown()
                    thread.join(timeout=2)
    except Exception as error:
        result["error"] = str(error)
    finally:
        if attempted:
            logs = subprocess.run([*reference.DOCKER, "logs", name], capture_output=True, text=True, timeout=5)
            (artifact / "worker.log").write_text(logs.stdout + logs.stderr)
            reference.owned_delete("container", name, owner)
            reference.owned_delete("volume", name, owner)
        for process in [holder, gateway]:
            if process is not None:
                process.wait(timeout=5)
        result["global_sysctls_unchanged"] = before == consumer_worker_loss.sysctls()
        result["cleanup"] = True
        result["inputs_unchanged"] = result["source_context"] == upstream_unit.context_manifest(source)
        result["passed"] = result["passed"] and result["global_sysctls_unchanged"] and result["inputs_unchanged"]
        reference_lifecycle.write_json(artifact / "result.json", result)
        print(json.dumps({"evidence": str(artifact), "passed": result["passed"], "cleanup": result["cleanup"], "error": result.get("error")}), flush=True)
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
