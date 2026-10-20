import json
from pathlib import Path
import shutil
import subprocess
import unittest


@unittest.skipUnless(shutil.which("node"), "consumer protocol regression requires Node")
class OpenCodeProbeTests(unittest.TestCase):
    def run_probe(self, *, wrong_pid=False):
        # Exercise the real probe against a real HTTP server implementing the
        # supplied OpenCode 2 /api/info and authenticated session-read contract.
        # Only its container-specific filesystem paths are substituted.
        script = r"""
const fs = require("node:fs"), http = require("node:http"), vm = require("node:vm");
const source = fs.readFileSync(process.argv[1], "utf8");
const password = "owned-protocol-fixture";
const expected = "Basic " + Buffer.from("opencode:" + password).toString("base64");
const server = http.createServer((request, response) => {
  if (request.headers.connection !== "close") {
    response.writeHead(503); response.end("{}"); return;
  }
  if (request.headers.authorization !== expected) {
    response.writeHead(401); response.end("{}"); return;
  }
  response.setHeader("content-type", "application/json");
  if (request.url === "/api/info") {
    response.end(JSON.stringify({
      pid: process.pid + (process.argv[2] === "wrong" ? 1 : 0), version: "2.0.27"
    })); return;
  }
  if (request.url === "/api/session") {
    response.end("[]");
    setImmediate(() => {server.close(); server.closeIdleConnections();});
    return;
  }
  response.writeHead(404); response.end("{}");
});
server.listen(0, "127.0.0.1", () => {
  const containerFS = {
    realpathSync: () => "/fixture/bun",
    readFileSync: path => {
      if (path.endsWith("/package.json")) return JSON.stringify({dependencies:{"@opencode/server":"2.0.27"}});
      if (path.endsWith("opencode.pid")) return String(process.pid);
      if (path.endsWith("opencode-password")) return password;
      throw new Error("unexpected native path");
    }
  };
  vm.runInNewContext(source, {
    require: name => {
      if (name === "node:fs") return containerFS;
      if (name === "node:http") return {
        request: (url, options, callback) => http.request(
          "http://127.0.0.1:" + server.address().port + new URL(url).pathname, options, callback)
      };
      throw new Error(name);
    },
    process, console, Buffer, URL
  });
});
"""
        probe = Path(__file__).resolve().parents[1] / "tests/opencode-probe.cjs"
        return subprocess.run(
            ["node", "-e", script, str(probe), "wrong" if wrong_pid else "original"],
            capture_output=True, text=True, timeout=5)

    def test_probe_uses_the_supplied_v2_info_protocol_and_checks_authentication(self):
        result = self.run_probe()
        self.assertEqual(result.returncode, 0, result.stderr)
        observed = json.loads(result.stdout)
        self.assertEqual(observed["info_status"], 200)
        self.assertEqual(observed["unauthorized_status"], 401)
        self.assertEqual(observed["session_status"], 200)
        self.assertEqual(observed["server_version"], "2.0.27")

    def test_probe_refuses_a_server_with_a_different_original_process_identity(self):
        result = self.run_probe(wrong_pid=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("OpenCode info differs from its original native process", result.stderr)
