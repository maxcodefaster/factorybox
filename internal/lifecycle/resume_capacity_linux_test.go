//go:build linux

package lifecycle

import (
	"math"
	"testing"
)

func TestResumeImageBytesIncludesEveryFileWithoutOverflow(t *testing.T) {
	files := []CheckpointFile{{Name: "images/metadata.img", Bytes: 4097},
		{Name: "images/pages-1.img", Bytes: 32 << 20}, {Name: "images/empty.img"}}
	if actual, err := resumeImageBytes(files); err != nil || actual != (32<<20)+4097 {
		t.Fatal("Resume omitted part of its complete private image set", actual, err)
	}
	for _, invalid := range [][]CheckpointFile{
		nil, {{Bytes: -1}}, {{Bytes: math.MaxInt64}, {Bytes: math.MaxInt64}, {Bytes: 2}},
	} {
		if _, err := resumeImageBytes(invalid); err == nil {
			t.Fatal("Resume admitted an absent, negative or overflowed byte inventory", invalid)
		}
	}
}
