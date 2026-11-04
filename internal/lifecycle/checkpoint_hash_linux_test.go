//go:build linux

package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
)

func hashFixture(t *testing.T, directory string, sizes map[string]int) *Engine {
	t.Helper()
	if err := os.MkdirAll(directory+"/generation/images", 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	random := rand.New(rand.NewSource(7))
	for _, name := range []string{"criu.conf", "filesystem.json", "source-state.json", "sysbox-state.json"} {
		if err := root.WriteFile("generation/"+name, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, size := range sizes {
		data := make([]byte, size)
		random.Read(data)
		if err := root.WriteFile("generation/images/"+name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return &Engine{root: root, config: Config{Root: directory}}
}

func TestParallelCheckpointHashesMatchASequentialPassInSortedOrder(t *testing.T) {
	sizes := map[string]int{"pages-1.img": 5<<20 + 17, "core-1.img": 1, "mm-1.img": 0, "fdinfo-2.img": 4096}
	for index := 0; index < 40; index++ {
		sizes[fmt.Sprintf("small-%02d.img", index)] = 100 + index
	}
	engine := hashFixture(t, t.TempDir(), sizes)
	names, err := engine.checkpointFileNames("generation")
	if err != nil {
		t.Fatal(err)
	}
	sequential, err := engine.hashCheckpointFiles(context.Background(), "generation", names, 1)
	if err != nil || len(sequential) != len(sizes)+4 {
		t.Fatal("sequential pass failed", err)
	}
	for _, workers := range []int{2, 4, 64} {
		parallel, err := engine.hashCheckpointFiles(context.Background(), "generation", names, workers)
		if err != nil || !reflect.DeepEqual(sequential, parallel) {
			t.Fatal("parallel manifest differs from the sequential manifest", workers, err)
		}
	}
	for index, file := range sequential {
		if index > 0 && sequential[index-1].Name >= file.Name {
			t.Fatal("manifest is not sorted", file.Name)
		}
		data, err := engine.root.ReadFile("generation/" + file.Name)
		sum := sha256.Sum256(data)
		if err != nil || file.SHA256 != hex.EncodeToString(sum[:]) || file.Bytes != int64(len(data)) {
			t.Fatal("hash is not the content hash", file.Name, err)
		}
	}
}

func TestParallelCheckpointHashKeepsEveryIdentityCheck(t *testing.T) {
	engine := hashFixture(t, t.TempDir(), map[string]int{"pages-1.img": 1 << 20, "core-1.img": 10})
	if err := os.Link(engine.config.Root+"/generation/images/core-1.img", engine.config.Root+"/generation/images/alias.img"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.checkpointFiles(context.Background(), "generation"); err == nil || !strings.Contains(err.Error(), "regular-file identity") {
		t.Fatal("a hardlinked image was hashed", err)
	}
	if err := os.Remove(engine.config.Root + "/generation/images/alias.img"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("core-1.img", engine.config.Root+"/generation/images/link.img"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.checkpointFiles(context.Background(), "generation"); err == nil {
		t.Fatal("a symlinked image was admitted")
	}
	if err := os.Remove(engine.config.Root + "/generation/images/link.img"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.checkpointFiles(cancelled, "generation"); err == nil {
		t.Fatal("a cancelled context still produced a manifest")
	}
	if files, err := engine.checkpointFiles(context.Background(), "generation"); err != nil || len(files) != 6 {
		t.Fatal(err)
	}
}
