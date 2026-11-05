//go:build linux

package lifecycle

import (
	"context"
	"encoding/json"
	"testing"
)

// A retained old epoch can hold a Resume that never attempted a restore. Its inputs are
// unchanged, so it is inventoried like a paused Sandbox instead of blocking all retirement.
func TestRetainedUnattemptedResumeDoesNotBlockBackingRetirement(t *testing.T) {
	engine, record, _ := unattemptedResumeFixture(t)
	engine.metadataOnly = true
	config := json.RawMessage(`{"mounts":[]}`)
	data, _ := json.Marshal(config)
	record.ConfigSHA256 = sum(data)
	if err := engine.root.WriteFile("objects/"+record.ID+"/config.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := engine.root.Mkdir("forks", 0700); err != nil {
		t.Fatal(err)
	}
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	if err := engine.retirementPathDependencies(context.Background(), "/elsewhere/backing"); err != nil {
		t.Fatalf("an unattempted Resume blocked unrelated backing retirement: %v", err)
	}
	record.Pause.Resume.Attempted = true
	if err := engine.write(record); err != nil {
		t.Fatal(err)
	}
	if err := engine.retirementPathDependencies(context.Background(), "/elsewhere/backing"); err == nil {
		t.Fatal("an attempted Resume must keep blocking retirement")
	}
}
