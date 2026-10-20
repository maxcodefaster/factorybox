const fs = require("node:fs");
const http = require("node:http");
const { realpathSync } = fs;
const packageInfo = JSON.parse(fs.readFileSync("/opt/factory/opencode/server/package.json", "utf8"));
const pid = Number(fs.readFileSync("/run/fixture-opencode.pid", "utf8").trim());
if (!Number.isSafeInteger(pid) || pid <= 1) throw new Error("missing owned OpenCode process");
process.kill(pid, 0);
if (realpathSync(`/proc/${pid}/exe`) !== realpathSync("/opt/factory/opencode/musl/bun")) {
  throw new Error("OpenCode process no longer uses its supplied native Bun executable");
}
const password = fs.readFileSync("/run/fixture-opencode-password", "utf8");
const base = "http://127.0.0.1:4096";
function request(path, headers = {}) {
  return new Promise((resolve, reject) => {
    let result;
    const operation = http.request(new URL(path, base), {
      agent: false, headers: { ...headers, connection: "close" },
    }, response => {
      const chunks = [];
      let bytes = 0;
      response.on("data", chunk => {
        bytes += chunk.length;
        if (bytes > 65536) operation.destroy(new Error("OpenCode probe response exceeds its bound"));
        else chunks.push(chunk);
      });
      response.on("error", reject);
      response.on("end", () => {
        result = { status: response.statusCode, body: Buffer.concat(chunks).toString("utf8") };
      });
    });
    operation.on("socket", socket => socket.once("close", () => {
      if (result) resolve(result);
      else reject(new Error("OpenCode probe socket closed before its complete response"));
    }));
    operation.on("error", reject);
    operation.setTimeout(3000, () => operation.destroy(new Error("OpenCode probe request timed out")));
    operation.end();
  });
}
async function observe() {
  const unauthenticated = await request("/api/session");
  if (unauthenticated.status !== 401) throw new Error("OpenCode accepted an unauthenticated session read");
  const headers = { authorization: "Basic " + Buffer.from("opencode:" + password).toString("base64") };
  const info = await request("/api/info", headers);
  if (info.status !== 200) throw new Error("OpenCode info refused its owned credential");
  const server = JSON.parse(info.body);
  if (server.pid !== pid || server.version !== packageInfo.dependencies["@opencode/server"]) {
    throw new Error("OpenCode info differs from its original native process");
  }
  const sessions = await request("/api/session", headers);
  if (sessions.status !== 200) throw new Error("OpenCode session read refused its owned credential");
  JSON.parse(sessions.body);
  console.log(JSON.stringify({
    pid, server_version: packageInfo.dependencies["@opencode/server"],
    unauthorized_status: unauthenticated.status, info_status: info.status,
    session_status: sessions.status, server,
  }));
}
observe().catch(error => { console.error(String(error)); process.exit(1); });
