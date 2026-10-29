//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func retirementPreparationFixture(t *testing.T) (*Engine, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root ownership; exercised in the native worker lane")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "preparation")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &Engine{root: root, config: Config{Root: directory}}, path
}

func TestRetirementPreparationRecoversPartialENOSPCMetadata(t *testing.T) {
	engine, path := retirementPreparationFixture(t)
	for _, name := range []string{"retirement-payload.json.gz", ".retirement-reserve", ".checkpoint-" + strings.Repeat("a", 32)} {
		if err := os.WriteFile(path+"/"+name, []byte("incomplete private metadata"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	peer := engine.config.Root + "/canonical-source"
	if err := os.WriteFile(peer, []byte("retained source"), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := engine.resetUnacceptedRetirementPreparation(context.Background(), "preparation"); err != nil {
			t.Fatal("ordinary exact retry could not clear unaccepted preparation", err)
		}
	}
	if entries, err := os.ReadDir(path); err != nil || len(entries) != 0 {
		t.Fatal("interrupted preparation still blocks the next inventory", entries, err)
	}
	if data, err := os.ReadFile(peer); err != nil || string(data) != "retained source" {
		t.Fatal("preparation recovery changed payload data", err)
	}
}

func TestRetirementPreparationRefusesAcceptanceAndForeignMetadata(t *testing.T) {
	for _, name := range []string{
		"retirement-intent.json", "retirement-complete.json", "source",
		"retirement-payload.json.gz/child", ".checkpoint-invalid",
	} {
		t.Run(name, func(t *testing.T) {
			engine, path := retirementPreparationFixture(t)
			good := path + "/.retirement-reserve"
			if err := os.WriteFile(good, []byte("preserve original metadata"), 0600); err != nil {
				t.Fatal(err)
			}
			foreign := path + "/" + name
			if err := os.MkdirAll(filepath.Dir(foreign), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(foreign, []byte("preserve foreign metadata"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := engine.resetUnacceptedRetirementPreparation(context.Background(), "preparation"); err == nil {
				t.Fatal("accepted/foreign preparation was reset")
			}
			for file, expected := range map[string]string{good: "preserve original metadata", foreign: "preserve foreign metadata"} {
				if data, err := os.ReadFile(file); err != nil || string(data) != expected {
					t.Fatal("refusal changed original or foreign bytes", file, err)
				}
			}
		})
	}
}

func TestRetirementPreparationRefusesUnsafeObjectsBeforeAnyRemoval(t *testing.T) {
	for _, shape := range []string{"symlink", "dangling-acceptance", "hardlink", "public-mode", "foreign-owner", "oversize", "multiple-temporaries"} {
		t.Run(shape, func(t *testing.T) {
			engine, path := retirementPreparationFixture(t)
			good := path + "/.retirement-reserve"
			if err := os.WriteFile(good, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			foreign := path + "/retirement-payload.json.gz"
			var err error
			switch shape {
			case "symlink":
				err = os.Symlink(good, foreign)
			case "dangling-acceptance":
				err = os.Symlink("missing", path+"/retirement-intent.json")
			case "hardlink":
				err = os.Link(good, engine.config.Root+"/external-link")
			case "public-mode":
				err = os.Chmod(good, 0644)
			case "foreign-owner":
				err = os.Chown(good, 1000, 1000)
			case "oversize":
				err = os.Truncate(good, retirementReserveBytes+1)
			case "multiple-temporaries":
				for _, token := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32)} {
					if err = os.WriteFile(path+"/.checkpoint-"+token, nil, 0600); err != nil {
						break
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(good)
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.resetUnacceptedRetirementPreparation(context.Background(), "preparation"); err == nil {
				t.Fatal("unsafe preparation was reset")
			}
			after, err := os.ReadFile(good)
			if err != nil || string(after) != string(before) {
				t.Fatal("refusal changed original preparation", err)
			}
		})
	}
}

func TestRetirementPreparationCancellationPreservesMetadata(t *testing.T) {
	engine, path := retirementPreparationFixture(t)
	file := path + "/retirement-payload.json.gz"
	if err := os.WriteFile(file, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := engine.resetUnacceptedRetirementPreparation(ctx, "preparation"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled retry did not stop", err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "partial" {
		t.Fatal("cancelled retry changed metadata", err)
	}
}
