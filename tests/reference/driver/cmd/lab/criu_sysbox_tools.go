package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const criuToolsClient = "import {OpenCode} from '/opt/opencode/node_modules/@opencode/client/dist/promise/index.js';const c=OpenCode.make({baseUrl:'http://127.0.0.1:4096',headers:{Authorization:'Basic '+Buffer.from('opencode:local-experiment-only').toString('base64')}});"

var criuToolsBrowserNonce string
var criuToolsServer map[string]any
var criuToolsSessionID string

func criuOpenCodeState(id, script string) (map[string]any, error) {
	out, err := criuRunc("exec", id, "/opt/opencode/bun", "-e", criuToolsClient+script)
	if err != nil {
		return nil, fmt.Errorf("OpenCode client: %s: %w", out, err)
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return nil, fmt.Errorf("OpenCode client response: %s: %w", out, err)
	}
	return state, nil
}

func criuToolsDiagnostic(id, phase string) {
	start := time.Now()
	out, err := criuRunc("exec", id, "/bin/sh", "-c", "tail -n 20 /tmp/tool-log-*")
	record("sysbox-"+id+"-"+phase+"-tools-diagnostic", start, out, err)
}

func criuSysboxToolsPrepare(id string) error {
	if os.Getenv("CRIU_SYSBOX_TOOLS") != "1" {
		return nil
	}
	start := time.Now()
	if err := criuSysboxIOPolicyProof(id, "source"); err != nil {
		return err
	}
	out, err := criuRunc("exec", id, "touch", "/workspace/tools-start")
	if err == nil {
		out, err = criuRunc("exec", id, "/opt/opencode/bun", "/opt/opencode/health.mjs")
	}
	if err == nil {
		err = json.Unmarshal([]byte(out), &criuToolsServer)
	}
	if err == nil && criuToolsServer["version"] != "2.0.27" {
		err = fmt.Errorf("unexpected OpenCode server version: %v", criuToolsServer)
	}
	if err == nil {
		out, err = criuEpochBrowserBootstrap(id)
		criuToolsBrowserNonce = strings.TrimSpace(out)
	}
	var session map[string]any
	if err == nil {
		session, err = criuOpenCodeState(id, "const active=await c.session.active();if(Object.keys(active).length)throw Error('Agent already active');const session=await c.session.create({title:'captured-before-snapshot',location:{directory:'/workspace/fixture'}});console.log(JSON.stringify(session));")
		if err == nil {
			criuToolsSessionID, _ = session["id"].(string)
			if criuToolsSessionID == "" || criuToolsBrowserNonce == "" {
				err = fmt.Errorf("source tools did not return browser RAM and native session identities")
			}
		}
	}
	record("sysbox-source-tools-ram-and-native-session", start, asJSON(map[string]any{"server": criuToolsServer, "browser_nonce": criuToolsBrowserNonce, "session": session})+out, err)
	if err != nil {
		criuToolsDiagnostic(id, "source")
	}
	if err == nil {
		err = criuSysboxServiceResponses(id, "source", time.Now())
	}
	return err
}

func criuSysboxToolsProof(id, phase string) error {
	if os.Getenv("CRIU_SYSBOX_TOOLS") != "1" {
		return nil
	}
	start := time.Now()
	if err := criuSysboxIOPolicyProof(id, phase); err != nil {
		return err
	}
	browser, err := criuRunc("exec", id, "node", "/opt/browser-state.mjs", "get")
	if err == nil && strings.TrimSpace(browser) != criuToolsBrowserNonce {
		err = fmt.Errorf("retained Chromium page RAM changed for %s: %q", id, browser)
	}
	var state map[string]any
	if err == nil {
		state, err = criuOpenCodeState(id, "const active=await c.session.active();if(Object.keys(active).length)throw Error('Restored idle agent became active');console.log(JSON.stringify({server:await c.server.info(),session:await c.session.get({sessionID:"+asJSON(criuToolsSessionID)+"}),active}));")
	}
	if err == nil {
		server, _ := state["server"].(map[string]any)
		session, _ := state["session"].(map[string]any)
		if server["version"] != criuToolsServer["version"] || server["pid"] != criuToolsServer["pid"] ||
			session["id"] != criuToolsSessionID || session["title"] != "captured-before-snapshot" {
			err = fmt.Errorf("restored OpenCode process/session differs for %s: %v", id, state)
		}
	}
	record("sysbox-"+id+"-"+phase+"-browser-opencode", start, browser+asJSON(state), err)
	if err != nil {
		criuToolsDiagnostic(id, phase)
	}
	return err
}

// Admission remains outside guest snapshots. This gate covers idle sessions;
// it neither starts inference nor claims to fence arbitrary external effects.
func criuSysboxToolsDivergence(sourceID, copyID string) error {
	if os.Getenv("CRIU_SYSBOX_TOOLS") != "1" {
		return nil
	}
	start := time.Now()
	g := newAssignmentGate()
	parent, err := g.register("snapshot-parent")
	if err != nil {
		return err
	}
	ids := []string{sourceID, copyID}
	browserNonces := make([]string, 2)
	generatedNonces := make([]string, 2)
	sessions := make([]map[string]any, 2)
	for branch, id := range ids {
		lease, leaseErr := g.register(id)
		if leaseErr != nil {
			err = leaseErr
			break
		}
		if allowed, rejected := g.admit(id, parent, "copied-parent-work"); allowed || rejected == nil {
			err = fmt.Errorf("copied parent assignment was admitted to %s", id)
			break
		}
		commandID := "new-task-" + id
		if allowed, admitErr := g.admit(id, lease, commandID); !allowed || admitErr != nil {
			err = fmt.Errorf("fresh assignment was rejected for %s: %v", id, admitErr)
			break
		}
		out, browserErr := criuRunc("exec", id, "node", "/opt/browser-state.mjs", "set")
		if browserErr != nil {
			err = browserErr
			break
		}
		generatedNonces[branch] = strings.TrimSpace(out)
		// A complete fork also copies cached user-space randomness. Use the
		// controller's fresh lease to test independent writes, recording the
		// application's generated values instead of assuming they diverge.
		out, browserErr = criuRunc("exec", id, "node", "/opt/browser-state.mjs", "set", lease)
		if browserErr != nil {
			err = browserErr
			break
		}
		browserNonces[branch] = strings.TrimSpace(out)
		if browserNonces[branch] != lease {
			err = fmt.Errorf("browser did not retain the assigned branch marker for %s", id)
			break
		}
		sessions[branch], err = criuOpenCodeState(id, "const active=await c.session.active();if(Object.keys(active).length)throw Error('Agent active before assignment');console.log(JSON.stringify(await c.session.create({title:"+asJSON(commandID)+",location:{directory:'/workspace/fixture'}})));")
		if err != nil {
			break
		}
		if allowed, replayErr := g.admit(id, lease, commandID); allowed || replayErr != nil {
			err = fmt.Errorf("assignment replay was not suppressed for %s: %v", id, replayErr)
			break
		}
	}
	record("sysbox-copy-tools-generated-and-assigned-identities", start, asJSON(map[string]any{
		"browser_generated": generatedNonces, "browser_assigned": browserNonces, "opencode_sessions": sessions,
	}), err)
	if err == nil && (browserNonces[0] == "" || browserNonces[0] == browserNonces[1] ||
		browserNonces[0] == criuToolsBrowserNonce || browserNonces[1] == criuToolsBrowserNonce ||
		sessions[0]["id"] == nil || sessions[0]["id"] == sessions[1]["id"]) {
		err = fmt.Errorf("browser/OpenCode branch mutations did not produce distinct identities")
	}
	states := map[string]any{}
	if err == nil {
		for branch, id := range ids {
			browser, readErr := criuRunc("exec", id, "node", "/opt/browser-state.mjs", "get")
			if readErr != nil || strings.TrimSpace(browser) != browserNonces[branch] {
				err = fmt.Errorf("Chromium page mutation crossed branches for %s: %s %v", id, browser, readErr)
				break
			}
			state, readErr := criuOpenCodeState(id, "const rows=await c.session.list({limit:100});const ids=rows.data.map(s=>s.id);if(!ids.includes("+asJSON(criuToolsSessionID)+")||!ids.includes("+asJSON(sessions[branch]["id"])+")||ids.includes("+asJSON(sessions[1-branch]["id"])+"))throw Error('OpenCode session storage crossed branches');console.log(JSON.stringify({sessions:rows.data.map(({id,title})=>({id,title})),active:await c.session.active()}));")
			states[id] = map[string]any{"browser_nonce": strings.TrimSpace(browser), "opencode": state}
			if readErr != nil {
				err = readErr
				break
			}
		}
	}
	record("sysbox-copy-browser-opencode-idle-assignment-diverge", start, asJSON(states), err)
	return err
}
