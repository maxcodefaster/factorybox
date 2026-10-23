//go:build linux

package lifecycle

import (
	"strings"
	"testing"

	"factorybox/internal/storefs"
)

func TestRetainedControllerStaysInItsExactStore(t *testing.T) {
	prefix := "/fixed/data/store/epochs/"
	current, previous := strings.Repeat("a", 32), strings.Repeat("b", 32)
	config := Config{Root: prefix + current + "/controller", ManagerRoot: "/fixed/data/store/manager", BoundedStore: &storefs.Active{}}
	if err := retainedControllerPath(config, prefix+previous+"/controller"); err != nil {
		t.Fatal("distinct earlier controller in the same store refused", err)
	}
	for _, source := range []string{"", config.Root, prefix + previous, prefix + previous + "/controller/",
		prefix + previous + "/controller/../controller", prefix + previous + "/controller/objects",
		prefix + previous[:31] + "/controller", "/another/data/store/epochs/" + previous + "/controller",
		"/fixed/data/store-other/epochs/" + previous + "/controller", "/controller"} {
		if err := retainedControllerPath(config, source); err == nil {
			t.Fatalf("maintenance escaped its exact earlier epoch: %q", source)
		}
	}
	config.BoundedStore = nil
	if err := retainedControllerPath(config, prefix+previous+"/controller"); err == nil {
		t.Fatal("unbounded controller admitted retained-epoch mutation")
	}
}
