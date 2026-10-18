package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type criuTheiaIdentity struct {
	ClientID string `json:"client_id"`
	PID      int    `json:"pid"`
}

type criuTheiaResult struct {
	OK bool `json:"ok"`
	criuTheiaIdentity
	Versions struct {
		Theia          string `json:"theia"`
		SocketIOClient string `json:"socketIoClient"`
	} `json:"versions"`
	Values map[string]struct {
		Marker string `json:"marker"`
		Probe  string `json:"probe"`
	} `json:"values"`
	Markers       []string `json:"markers"`
	RPCDurationMS *float64 `json:"rpc_duration_ms"`
	Transport     string   `json:"transport"`
	Frontend      string   `json:"frontend"`
}

var criuTheiaClient *criuTheiaIdentity
var criuTheiaUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// The bridge runs beside Theia, outside captured cgroups. HTTP success alone
// is insufficient: the same native RPC connection must read and edit the
// selected branch and preserve its identity across capture and both restores.
func criuTheiaRequest(ctx context.Context, client *http.Client, address, operation string, branches []string, phase, marker string, identity *criuTheiaIdentity) (criuTheiaResult, error) {
	var result criuTheiaResult
	payload := asJSON(map[string]any{"branches": branches, "phase": phase, "expected_marker": marker})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address+operation, strings.NewReader(payload))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(data) > 16384 || response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		return result, fmt.Errorf("native Theia bridge response differs: HTTP %d body=%q error=%v", response.StatusCode, data, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, fmt.Errorf("Theia bridge returned trailing data: %v", err)
	}
	if !result.OK || !criuTheiaUUID.MatchString(result.ClientID) || result.PID <= 0 ||
		result.Versions.Theia != "1.76.0" || result.Versions.SocketIOClient != "4.8.4" ||
		result.Transport != "native Theia remote-filesystem RPC" || result.Frontend != "HTTP shell only; browser editing not asserted" ||
		result.RPCDurationMS == nil || *result.RPCDurationMS < 0 || math.IsNaN(*result.RPCDurationMS) || math.IsInf(*result.RPCDurationMS, 0) {
		return result, fmt.Errorf("incomplete native Theia response: %s", data)
	}
	if identity != nil && result.criuTheiaIdentity != *identity {
		return result, fmt.Errorf("native Theia client reconnected or restarted: before=%v after=%v", *identity, result.criuTheiaIdentity)
	}
	switch operation {
	case "/ready":
		if len(result.Values) != len(branches) || len(result.Markers) != 0 {
			return result, fmt.Errorf("Theia returned different selected branches")
		}
		for _, id := range branches {
			value, ok := result.Values[id]
			if !ok || value.Marker != marker || value.Probe != phase+":"+id {
				return result, fmt.Errorf("Theia saved-state read/edit differs for %s: %v", id, value)
			}
		}
	case "/diverge":
		if len(branches) != 2 || len(result.Values) != 0 || len(result.Markers) != 2 || result.Markers[0] == result.Markers[1] {
			return result, fmt.Errorf("Theia independent edit results differ")
		}
		for index, value := range result.Markers {
			prefix := fmt.Sprintf("theia-branch-%d-", index)
			if !strings.HasPrefix(value, prefix) || !criuTheiaUUID.MatchString(strings.TrimPrefix(value, prefix)) {
				return result, fmt.Errorf("invalid Theia selected-branch edit: %q", value)
			}
		}
	default:
		return result, fmt.Errorf("unknown native Theia operation")
	}
	return result, nil
}

func criuSysboxTheiaReady(branches []string, phase string, operationStart time.Time) error {
	if os.Getenv("CRIU_SYSBOX_THEIA") != "1" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := criuTheiaRequest(ctx, http.DefaultClient, "http://127.0.0.1:4401", "/ready", branches, phase, criuComposeMarker, criuTheiaClient)
	if err == nil && criuTheiaClient == nil {
		identity := result.criuTheiaIdentity
		criuTheiaClient = &identity
	}
	record("sysbox-"+phase+"-warm-theia-read-edit", operationStart, asJSON(result), err)
	return err
}

func criuSysboxTheiaProof(sourceID, copyID string) error {
	if os.Getenv("CRIU_SYSBOX_THEIA") != "1" {
		return nil
	}
	start := time.Now()
	if criuTheiaClient == nil {
		return fmt.Errorf("native Theia was not warmed before capture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := criuTheiaRequest(ctx, http.DefaultClient, "http://127.0.0.1:4401", "/diverge", []string{sourceID, copyID}, "", "", criuTheiaClient)
	states := map[string]string{}
	if err == nil {
		for branch, id := range []string{sourceID, copyID} {
			marker, readErr := criuComposeMarkerRead(id)
			states[id] = strings.TrimSpace(marker)
			if readErr != nil || states[id] != result.Markers[branch] {
				err = fmt.Errorf("Theia edit did not reach selected Dev Container %s: %q %v", id, marker, readErr)
				break
			}
		}
	}
	if err == nil {
		err = os.WriteFile(criuRoot+"/theia-proof-result.json", []byte(asJSON(result)+"\n"), 0600)
	}
	record("sysbox-theia-selected-branch-read-edit", start, asJSON(map[string]any{"native_client": result, "devcontainer_readback": states}), err)
	return err
}
