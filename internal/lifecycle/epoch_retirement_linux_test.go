//go:build linux

package lifecycle

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestEpochRetirementCompletionFitsAllocatedReserve(t *testing.T) {
	prepared := epochRetirementPreparation{Request: EpochRetirementRequest{
		SourceRoot: "/var/lib/sysbox/fixed-store/data/store/epochs/" + strings.Repeat("a", 32) + "/controller",
	}}
	if err := epochRetirementCompletionBound(prepared); err != nil {
		t.Fatal("small completion was refused", err)
	}
	for i := 0; i < 2048; i++ {
		source := CheckpointRequest{fmt.Sprintf("source-%04d", i), strings.Repeat("b", 32), fmt.Sprintf("%032x", i+1), strings.Repeat("c", 64)}
		target := CheckpointRequest{fmt.Sprintf("successor-%04d", i), strings.Repeat("d", 32), fmt.Sprintf("%032x", i+2049), strings.Repeat("e", 64)}
		prepared.Request.Handovers = append(prepared.Request.Handovers, EpochRecoveryHandover{Source: source,
			Successor: RetainedCheckpointRequest{SourceRoot: "/var/lib/sysbox/fixed-store/data/store/epochs/" + strings.Repeat("f", 32) + "/controller", CheckpointRequest: target}})
	}
	encoded, err := json.Marshal(EpochRetirement{Version: 1, Request: prepared.Request})
	if err != nil || len(encoded) <= retirementReserveBytes || len(encoded) >= checkpointManifestLimit {
		t.Fatal("fixture must fit the record limit while exceeding its allocated reserve", len(encoded), err)
	}
	if err := epochRetirementCompletionBound(prepared); err == nil || !strings.Contains(err.Error(), "completion reserve") {
		t.Fatal("oversized completion was admitted despite its smaller allocated reserve", err)
	}
}
