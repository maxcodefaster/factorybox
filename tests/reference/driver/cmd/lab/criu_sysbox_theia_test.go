package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWarmTheiaRejectsRestartAndWrongSelectedState(t *testing.T) {
	identity := criuTheiaIdentity{"424fef20-9c02-4d52-8ade-b286d68a67b7", 91}
	for _, change := range []string{"none", "client", "pid", "version", "missing-duration", "negative-duration", "marker", "probe", "branch", "extra-branch", "false-success", "html", "trailing-json"} {
		t.Run(change, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct {
					Branches       []string `json:"branches"`
					Phase          string   `json:"phase"`
					ExpectedMarker string   `json:"expected_marker"`
				}
				if r.Method != http.MethodPost || r.URL.Path != "/ready" || json.NewDecoder(r.Body).Decode(&input) != nil ||
					len(input.Branches) != 1 || input.Branches[0] != "source-sysbox" || input.Phase != "first-resume" || input.ExpectedMarker != "saved-marker\n" {
					t.Error("readiness request did not select saved state")
				}
				value := map[string]any{"marker": "saved-marker\n", "probe": "first-resume:source-sysbox"}
				values := map[string]any{"source-sysbox": value}
				versions := map[string]any{"theia": "1.76.0", "socketIoClient": "4.8.4"}
				result := map[string]any{"ok": true, "client_id": identity.ClientID, "pid": identity.PID, "versions": versions,
					"values": values, "markers": []string{}, "rpc_duration_ms": 0.4,
					"transport": "native Theia remote-filesystem RPC", "frontend": "HTTP shell only; browser editing not asserted"}
				switch change {
				case "client":
					result["client_id"] = "651ba6bd-5c5c-418b-b136-294130f5f5f7"
				case "pid":
					result["pid"] = identity.PID + 1
				case "version":
					versions["theia"] = "1.77.0"
				case "missing-duration":
					delete(result, "rpc_duration_ms")
				case "negative-duration":
					result["rpc_duration_ms"] = -1
				case "marker":
					value["marker"] = "recreated-state"
				case "probe":
					value["probe"] = "source:source-sysbox"
				case "branch":
					delete(values, "source-sysbox")
					values["copy-sysbox"] = value
				case "extra-branch":
					values["copy-sysbox"] = value
				case "false-success":
					result["ok"] = false
				case "html":
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, "<html>ready</html>")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(result)
				if change == "trailing-json" {
					fmt.Fprint(w, "{}")
				}
			}))
			defer server.Close()
			_, err := criuTheiaRequest(context.Background(), server.Client(), server.URL, "/ready", []string{"source-sysbox"}, "first-resume", "saved-marker\n", &identity)
			if (err == nil) != (change == "none") {
				t.Fatalf("wrong native IDE state accepted: change=%s err=%v", change, err)
			}
		})
	}
}

func TestWarmTheiaRejectsSharedOrSwappedEdits(t *testing.T) {
	for _, change := range []string{"none", "shared", "swapped", "missing", "restarted"} {
		t.Run(change, func(t *testing.T) {
			identity := criuTheiaIdentity{"424fef20-9c02-4d52-8ade-b286d68a67b7", 91}
			markers := []string{"theia-branch-0-1b57c746-ae3a-4a64-a5e8-f06a0c5ce53a", "theia-branch-1-651ba6bd-5c5c-418b-b136-294130f5f5f7"}
			switch change {
			case "shared":
				markers[1] = markers[0]
			case "swapped":
				markers[0], markers[1] = markers[1], markers[0]
			case "missing":
				markers = markers[:1]
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				pid := identity.PID
				if change == "restarted" {
					pid++
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "client_id": identity.ClientID, "pid": pid,
					"versions": map[string]string{"theia": "1.76.0", "socketIoClient": "4.8.4"}, "values": map[string]any{}, "markers": markers,
					"rpc_duration_ms": 1.2, "transport": "native Theia remote-filesystem RPC", "frontend": "HTTP shell only; browser editing not asserted"})
			}))
			defer server.Close()
			_, err := criuTheiaRequest(context.Background(), server.Client(), server.URL, "/diverge", []string{"source-sysbox", "copy-sysbox"}, "", "", &identity)
			if (err == nil) != (change == "none") {
				t.Fatalf("independent edits not verified: change=%s err=%v", change, err)
			}
		})
	}
}
