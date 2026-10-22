//go:build linux

package lifecycle

import (
	"strings"
	"testing"
)

func TestCheckpointPreparationAfterRestore(t *testing.T) {
	image := CheckpointFile{Name: "images/pages-1.img", Bytes: 38158336, SHA256: strings.Repeat("a", 64)}
	log := CheckpointFile{Name: "images/restore.log", Bytes: 557, SHA256: strings.Repeat("b", 64)}
	stats := CheckpointFile{Name: "images/stats-restore", Bytes: 27, SHA256: strings.Repeat("c", 64)}
	for _, tc := range []struct {
		name             string
		prepared, actual []CheckpointFile
		intact           bool
	}{
		{"before restore", []CheckpointFile{image}, []CheckpointFile{image}, true},
		{"observed restore diagnostics", []CheckpointFile{image}, []CheckpointFile{image, log, stats}, true},
		{"changed original", []CheckpointFile{image}, []CheckpointFile{{Name: image.Name, Bytes: image.Bytes, SHA256: log.SHA256}, log, stats}, false},
		{"missing original", []CheckpointFile{image}, []CheckpointFile{log, stats}, false},
		{"unknown addition", []CheckpointFile{image}, []CheckpointFile{image, {Name: "images/unknown", Bytes: 1, SHA256: log.SHA256}}, false},
		{"oversized log", []CheckpointFile{image}, []CheckpointFile{image, {Name: log.Name, Bytes: (1 << 20) + 1, SHA256: log.SHA256}}, false},
		{"oversized stats", []CheckpointFile{image}, []CheckpointFile{image, {Name: stats.Name, Bytes: (64 << 10) + 1, SHA256: stats.SHA256}}, false},
		{"invalid diagnostic hash", []CheckpointFile{image}, []CheckpointFile{image, {Name: log.Name, Bytes: 557}}, false},
		{"duplicate original", []CheckpointFile{image}, []CheckpointFile{image, image}, false},
		{"duplicate diagnostic", []CheckpointFile{image}, []CheckpointFile{image, log, log}, false},
		{"changed preexisting diagnostic", []CheckpointFile{image, log}, []CheckpointFile{image, {Name: log.Name, Bytes: log.Bytes + 1, SHA256: log.SHA256}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkpointPreparationIntact(tc.prepared, tc.actual); got != tc.intact {
				t.Fatalf("intact=%v want %v", got, tc.intact)
			}
		})
	}
}
