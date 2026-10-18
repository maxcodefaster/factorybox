package main

import (
	"testing"

	"github.com/checkpoint-restore/go-criu/v6/crit/images"
	"google.golang.org/protobuf/proto"
)

func TestNativePagemapSchemaVersions(t *testing.T) {
	// Raw protobuf fixtures: field 2 was uint32 nr_pages. New CRIU uses
	// field 2 as compatibility zero and field 5 as uint64 nr_pages.
	for _, test := range []struct {
		name string
		wire []byte
		want uint64
	}{
		{"legacy", []byte{8, 0, 16, 3}, 3},
		{"current", []byte{8, 0, 16, 0, 40, 7}, 7},
		{"above_uint32", []byte{8, 0, 16, 0, 40, 128, 128, 128, 128, 16}, 1 << 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mapping images.PagemapEntry
			if err := proto.Unmarshal(test.wire, &mapping); err != nil {
				t.Fatal(err)
			}
			got, err := pageCount(&mapping)
			if err != nil || got != test.want {
				t.Fatalf("count=%d want=%d error=%v", got, test.want, err)
			}
		})
	}
}
