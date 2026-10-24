//go:build linux

package ownedtree

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func BenchmarkCaptureSmallFiles(b *testing.B) {
	directory := b.TempDir()
	if err := os.Mkdir(directory+"/payload", 0700); err != nil {
		b.Fatal(err)
	}
	content := make([]byte, 1024)
	for i := range content {
		content[i] = byte(i)
	}
	for i := range 1000 {
		if err := os.WriteFile(fmt.Sprintf("%s/payload/%04d", directory, i), content, 0600); err != nil {
			b.Fatal(err)
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		b.Fatal(err)
	}
	defer root.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		plan, err := Capture(context.Background(), root, []string{"payload"})
		if err != nil || len(plan.Entries) != 1001 {
			b.Fatal("complete real small-file inventory", len(plan.Entries), err)
		}
	}
}
