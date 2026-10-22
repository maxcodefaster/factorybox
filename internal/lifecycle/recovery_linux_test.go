//go:build linux

package lifecycle

import (
	"strings"
	"testing"
)

func TestRecoveryRequestBoundary(t *testing.T) {
	engine := &Engine{config: Config{Root: "/store/new"}}
	valid := RecoveryRequest{SourceRoot: "/store/old", SourceID: "source", SnapshotID: strings.Repeat("a", 32),
		SourceOwner: strings.Repeat("b", 32), CheckpointSHA256: strings.Repeat("c", 64)}
	if err := engine.validateRecoveryRequest(valid, "target"); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"/", "/store", "/store/new", "/store/new/old", "relative", "/store/../old", "/store/old\n"} {
		request := valid
		request.SourceRoot = root
		if err := engine.validateRecoveryRequest(request, "target"); err == nil {
			t.Errorf("accepted source root %q", root)
		}
	}
	if err := engine.validateRecoveryRequest(valid, "source"); err == nil {
		t.Fatal("adopted source ID as target")
	}
	for _, alter := range []func(*RecoveryRequest){
		func(r *RecoveryRequest) { r.SourceID = "../source" },
		func(r *RecoveryRequest) { r.SnapshotID = "" },
		func(r *RecoveryRequest) { r.SourceOwner = "" },
		func(r *RecoveryRequest) { r.CheckpointSHA256 = "" },
	} {
		request := valid
		alter(&request)
		if err := engine.validateRecoveryRequest(request, "target"); err == nil {
			t.Fatal("accepted incomplete request")
		}
	}
}
