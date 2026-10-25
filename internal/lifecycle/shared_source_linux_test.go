//go:build linux

package lifecycle

import (
	"encoding/json"
	"testing"

	"factorybox/internal/sysboxcapture"
)

func TestSharedSourceRequiresItsExactIndependentBind(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	engine := &Engine{config: Config{Root: root}}
	for _, test := range []struct {
		name, source, destination string
		options                   []string
		count                     int
		admitted                  bool
	}{
		{"exact", project, "/home/coder/project", []string{"rbind", "rw", "rprivate", "nodev", "nosuid"}, 1, true},
		{"missing", project, "/home/coder/project", nil, 0, false},
		{"duplicate", project, "/home/coder/project", nil, 2, false},
		{"different-source", root, "/home/coder/project", nil, 1, false},
		{"different-destination", project, "/elsewhere", nil, 1, false},
		{"propagation", project, "/home/coder/project", []string{"rshared"}, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			annotation, _ := json.Marshal(map[string]string{"source": project, "destination": "/home/coder/project"})
			annotations, _ := json.Marshal(map[string]string{sysboxcapture.SharedSourceAnnotation: string(annotation)})
			mounts := []map[string]any{}
			for range test.count {
				mounts = append(mounts, map[string]any{"type": "bind", "source": test.source, "destination": test.destination, "options": test.options})
			}
			encoded, _ := json.Marshal(mounts)
			shared, err := engine.admitSharedSource(map[string]json.RawMessage{"annotations": annotations, "mounts": encoded})
			if (err == nil) != test.admitted {
				t.Fatal("shared-source bind admission differed", shared, err)
			}
			if test.admitted && (shared.Inode == 0 || shared.MountID == 0 || shared.Device == 0) {
				t.Fatal("admission omitted the original kernel directory identity")
			}
		})
	}
}
