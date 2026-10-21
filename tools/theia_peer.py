"""Optional, exactly owned Theia sidecar for the native developer fixture."""
import json
import re
import subprocess
import time

import reference
import reference_lifecycle
import theia_native
import upstream_bats


class Peer:
    def __init__(self, worker, owner, image, archive, output, archive_image):
        self.worker, self.owner, self.image = worker, owner, image
        self.archive, self.output, self.archive_image = archive / "theia", output, archive_image
        self.archive.mkdir()
        self.client = archive / "theia-client.cjs"
        self.name, self.volume, self.setup = worker + "-ide", worker + "-ide-work", worker + "-ide-setup"
        self.volume_created = self.attempted = self.started = self.closed = False
        self.result = {"owner": owner, "image": image, "passed": False, "cleanup": False,
                       "scope": "Separate IDE peer for a trusted, private native fixture. "
                                "Lifecycle filesystem RPC assertions belong to its Go test."}

    def start(self, request):
        if (self.attempted or request.get("version") != 1 or request.get("run_owner") != self.owner or
                any(type(request.get(key)) is not int or not 0 < request[key] < 2**31
                    for key in ["uid", "gid"])):
            raise ValueError("Theia request differs from this exact developer fixture")
        worker = json.loads(reference.docker(["container", "inspect", self.worker]))[0]
        if (worker["Name"] != "/" + self.worker or not worker["State"]["Running"] or
                worker["Config"].get("Labels", {}).get("com.factorybox.run") != self.owner):
            raise ValueError("Theia target is not the original running owned worker")
        uid, gid = request["uid"], request["gid"]
        self.result.update({"container_name": self.name, "volume_name": self.volume,
                            "setup_name": self.setup, "request": request})
        reference_lifecycle.write_json(self.archive / "peer-intent.json", self.result)
        self.volume_created = True
        reference.docker(["volume", "create", "--label", "com.factorybox.run=" + self.owner, self.volume])
        prepare = """
const fs=require('node:fs');const [uid,gid]=JSON.parse(process.argv[1]);
fs.mkdirSync('/experiment/theia/home',{recursive:true});
for(const path of ['/experiment','/experiment/theia','/experiment/theia/home'])fs.chownSync(path,uid,gid);
"""
        setup = [
            *reference.DOCKER, "run", "--pull=never", "--name", self.setup,
            "--label", "com.factorybox.run=" + self.owner, "--cpus=1", "--memory=256m",
            "--pids-limit=32", "--network=none", "--read-only", "--user=0:0",
            "--cap-drop=ALL", "--cap-add=CHOWN", "--cap-add=DAC_OVERRIDE",
            "--security-opt=no-new-privileges",
            "--mount", f"type=volume,source={self.volume},target=/experiment",
            "--entrypoint", "node", self.image["image"], "-e", prepare, json.dumps([uid, gid]),
        ]
        self.result["setup_command"] = setup
        setup_started = time.monotonic()
        try:
            subprocess.run(setup, check=True, capture_output=True, timeout=30)
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
            self.result["setup_failure"] = {
                "type": type(error).__name__, "returncode": getattr(error, "returncode", None),
            }
            for stream in ["stdout", "stderr"]:
                content = getattr(error, stream, None) or b""
                if isinstance(content, str):
                    content = content.encode()
                (self.archive / ("setup-" + stream + ".log")).write_bytes(content[-8192:])
            raise
        finally:
            self.result["setup_seconds"] = time.monotonic() - setup_started
        reference.owned_delete("container", self.setup, self.owner)
        command = [
            *reference.DOCKER, "run", "--detach", "--pull=never", "--name", self.name,
            "--label", "com.factorybox.run=" + self.owner,
            "--pid=container:" + self.worker, "--network=container:" + self.worker,
            "--cpus=1", "--memory=768m", "--pids-limit=128", "--read-only",
            f"--user={uid}:{gid}", "--cap-drop=ALL", "--security-opt=no-new-privileges",
            "--env", "HOME=/experiment/theia/home",
            "--env", "XDG_CONFIG_HOME=/experiment/theia/home/.config",
            "--tmpfs", "/tmp:rw,nosuid,nodev,size=67108864,mode=1777",
            "--mount", f"type=volume,source={self.volume},target=/experiment",
            "--mount", f"type=bind,source={self.client},target=/experiment/theia-client.cjs,readonly",
            "--entrypoint", "/bin/sh", self.image["image"], "-c",
            "node /experiment/theia-client.cjs http://127.0.0.1:4400 4401 "
            "> /experiment/theia/client.log 2>&1 & "
            "exec node /opt/factory/theia/lib/backend/main.js /experiment "
            "--hostname 127.0.0.1 --port 4400 "
            "--session-preference security.workspace.trust.enabled=false",
        ]
        self.result["command"] = command
        reference_lifecycle.write_json(self.archive / "intent.json", self.result)
        self.attempted = True
        subprocess.run(command, check=True, capture_output=True, timeout=30)
        observed = json.loads(reference.docker(["container", "inspect", self.name]))[0]
        host = observed["HostConfig"]
        self.result["constraints"] = {
            "image": observed["Image"], "user": observed["Config"]["User"],
            **{key: host.get(key) for key in [
                "NanoCpus", "Memory", "PidsLimit", "ReadonlyRootfs", "NetworkMode",
                "PidMode", "Privileged", "CapAdd", "CapDrop", "SecurityOpt",
            ]},
        }
        namespace_targets = {"container:" + self.worker, "container:" + worker["Id"]}
        if (observed["Image"] != self.image["id"] or observed["Config"]["User"] != f"{uid}:{gid}" or
                host["NanoCpus"] != 1_000_000_000 or host["Memory"] != 768 << 20 or
                host["PidsLimit"] != 128 or not host["ReadonlyRootfs"] or host["Privileged"] or
                host.get("CapAdd") or host["CapDrop"] != ["ALL"] or
                "no-new-privileges" not in host["SecurityOpt"] or
                host["PidMode"] not in namespace_targets or host["NetworkMode"] not in namespace_targets):
            raise ValueError("Theia did not receive the original private namespaces and exact limits")
        deadline = time.monotonic() + 45
        peer = None
        while peer is None:
            try:
                peer = theia_native.execute(self.name, """
const fs=require('node:fs');const pid=Number(fs.readFileSync('/experiment/theia/client.pid','utf8'));
const stat=fs.readFileSync('/proc/'+pid+'/stat','utf8');
console.log(JSON.stringify({pid,start:stat.slice(stat.lastIndexOf(')')+1).trim().split(/\\s+/)[19],
client_id:fs.readFileSync('/experiment/theia/client-ready','utf8').trim()}));
""", timeout=5)
            except (ValueError, subprocess.SubprocessError):
                if time.monotonic() >= deadline:
                    raise
                time.sleep(0.2)
        if (type(peer["pid"]) is not int or peer["pid"] <= 1 or
                not re.fullmatch(r"[0-9]+", peer["start"]) or
                not re.fullmatch(r"[0-9a-f-]{36}", peer["client_id"])):
            raise ValueError("Theia peer lacks its native client identity")
        peer.update({"version": 1, "run_owner": self.owner, "uid": uid, "gid": gid})
        temporary = self.output / "theia-peer.json.partial"
        reference_lifecycle.write_json(temporary, peer)
        temporary.replace(self.output / "theia-peer.json")
        self.result["peer"], self.started = peer, True
        print(json.dumps({"theia_ready": self.name, "uid": uid, "gid": gid}), flush=True)

    def close(self, failed):
        if self.closed:
            return
        preserve = False
        try:
            if self.attempted:
                logs = subprocess.run([*reference.DOCKER, "logs", self.name],
                                      capture_output=True, text=True, timeout=15)
                (self.archive / "server.log").write_text(logs.stdout + logs.stderr)
                state = json.loads(reference.docker(["container", "inspect", self.name]))[0]["State"]
                self.result["container_state"] = state
                if state["OOMKilled"] or not state["Running"]:
                    raise ValueError("Theia exited before the lifecycle fixture released its peer")
                self.result["client_log"] = theia_native.execute(self.name, """
console.log(JSON.stringify(require('node:fs').readFileSync('/experiment/theia/client.log','utf8').slice(-8192)));
""")
                self.result["passed"] = self.started and not failed
        except Exception as error:
            self.result["error"] = str(error)
        try:
            reference.owned_delete("container", self.setup, self.owner)
            if self.attempted:
                reference.owned_delete("container", self.name, self.owner)
            if self.volume_created and (failed or self.result.get("error")):
                try:
                    upstream_bats.archive_stopped_volumes(
                        self.archive, self.name, self.owner, [self.volume],
                        self.archive_image, self.result, timeout=60)
                except Exception as error:
                    preserve = True
                    self.result["archive_error"] = str(error)
                    self.result["retained_volumes"] = [self.volume]
            if self.volume_created and not preserve:
                reference.owned_delete("volume", self.volume, self.owner)
            self.result["cleanup"] = not preserve
        except Exception as error:
            self.result["cleanup_error"] = str(error)
        self.result["passed"] = self.result["passed"] and self.result["cleanup"] and not self.result.get("error")
        self.closed = True
        reference_lifecycle.write_json(self.archive / "result.json", self.result)
