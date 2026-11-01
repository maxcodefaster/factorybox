//go:build linux

package fdstore

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRootPID1CRIUConsumesCompleteLargeGeneration(t *testing.T) {
	client := os.Getenv("FACTORYBOX_FDSTORE_CRIU_CLIENT")
	if client == "" || os.Geteuid() != 0 || os.Getpid() != 1 {
		t.Skip("explicit native PID 1 CRIU client interoperability lane")
	}
	for _, mode := range []string{"regular", "directories"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			socket := directory + "/store.sock"
			server, err := Start(socket)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			var retained []*os.File
			var names []string
			var mappings []Directory
			key := strings.Repeat("a", 32) + ":" + strings.Repeat("b", 32)
			expectedPath := "-"
			count := 257
			if mode == "regular" {
				path := filepath.Join(directory, "original")
				original, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				defer original.Close()
				if _, err := original.WriteString("pinned description"); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("replacement must not be reopened"), 0600); err != nil {
					t.Fatal(err)
				}
				for index := 0; index < count; index++ {
					retained = append(retained, original)
					names = append(names, fmt.Sprintf("factorybox-reg:%d", index+1))
				}
			} else {
				count = 129
				project := filepath.Join(directory, "project")
				oldPath := filepath.Join(project, "before")
				if err := os.MkdirAll(oldPath, 0700); err != nil {
					t.Fatal(err)
				}
				root, err := os.Open(directory)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				original, err := os.Open(oldPath)
				if err != nil {
					t.Fatal(err)
				}
				defer original.Close()
				for index := 0; index < count; index++ {
					name := fmt.Sprintf("factorybox-dir:%d", index+1)
					retained = append(retained, original)
					names = append(names, name)
					mappings = append(mappings, Directory{
						Name: name, MountPoint: "/workspace/project", MountRootRelative: "project",
					})
				}
				names, retained = append(names, SourceRootName), append(retained, root)
				renamed := strings.Repeat("d", 180)
				if err := os.Rename(oldPath, filepath.Join(project, renamed)); err != nil {
					t.Fatal(err)
				}
				expectedPath = "/workspace/project/" + renamed
			}
			if err := PutDirectories(socket, key, names, retained, mappings); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(client, socket, key, mode, fmt.Sprint(count), expectedPath).CombinedOutput()
			if err != nil || !strings.Contains(string(output), fmt.Sprintf("criu-shared-description-client=%d mode=%s", count, mode)) {
				t.Fatalf("actual CRIU client did not consume the exact retained generation: %v\n%s", err, output)
			}
			if err := Release(socket, key); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRootPID1CRIURejectsPartialGenerationWithoutLeakingOrPublishing(t *testing.T) {
	client := os.Getenv("FACTORYBOX_FDSTORE_CRIU_CLIENT")
	if client == "" || os.Geteuid() != 0 || os.Getpid() != 1 {
		t.Skip("explicit native PID 1 CRIU client interoperability lane")
	}
	for _, variant := range []string{"missing-final", "changed-version", "changed-key", "changed-total", "duplicate-offset", "over-budget"} {
		t.Run(variant, func(t *testing.T) {
			directory, err := os.MkdirTemp("", "factorybox-criu-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(directory) })
			path := directory + "/original.sock"
			listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			original, err := os.Open("/dev/null")
			if err != nil {
				t.Fatal(err)
			}
			defer original.Close()
			key := strings.Repeat("c", 32) + ":" + strings.Repeat("d", 32)
			done := make(chan error, 1)
			go func() {
				connection, err := listener.AcceptUnix()
				if err != nil {
					done <- err
					return
				}
				defer connection.Close()
				connection.SetDeadline(time.Now().Add(5 * time.Second))
				request, files, err := receive(connection)
				closeFiles(files)
				if err != nil || request.Key != key || request.Operation != "restore" {
					done <- fmt.Errorf("CRIU request differs: %v", err)
					return
				}
				first := packet{Version: TransferVersion, Total: 2,
					Message: message{Operation: "restore", Key: key, Names: []string{"factorybox-reg:1"}}}
				second := packet{Version: TransferVersion, Offset: 1, Total: 2, Final: true,
					Message: message{Operation: "restore", Key: key, Names: []string{"factorybox-reg:2"}}}
				switch variant {
				case "changed-version":
					second.Version++
				case "changed-key":
					second.Message.Key = strings.Repeat("e", 32) + ":" + strings.Repeat("f", 32)
				case "changed-total":
					second.Total, second.Final = 3, false
				case "duplicate-offset":
					second.Offset, second.Final = 0, false
				case "over-budget":
					first.Total = 1 << 30
				}
				body, _ := json.Marshal(first)
				if err := sendPacket(connection, body, []*os.File{original}); err != nil {
					done <- err
					return
				}
				if variant == "missing-final" || variant == "over-budget" {
					done <- connection.CloseWrite()
					return
				}
				body, _ = json.Marshal(second)
				done <- sendPacket(connection, body, []*os.File{original})
			}()
			output, err := exec.Command(client, path, key, "reject", "0", "-").CombinedOutput()
			if sendErr := <-done; sendErr != nil {
				t.Fatal(sendErr)
			}
			if err != nil || !strings.Contains(string(output), "criu-rejected-generation-before-publication=0 leaked=0") {
				t.Fatalf("CRIU published or leaked a partial descriptor generation: %v\n%s", err, output)
			}
		})
	}
}
