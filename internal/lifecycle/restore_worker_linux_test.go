//go:build linux

package lifecycle

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRestoreWorkerSealedRequest(t *testing.T) {
	request := restoreWorkerRequest{Version: 1, BootID: "original-boot", TargetID: "target",
		TargetSHA: strings.Repeat("a", 64), RequestSHA: strings.Repeat("b", 64), Operation: "restore"}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
		seal bool
		pass bool
	}{
		{"unsealed", encoded, false, false},
		{"valid", encoded, true, true},
		{"trailing-object", append(append([]byte{}, encoded...), []byte(" {}")...), true, false},
		{"unknown-field", append(append([]byte{}, encoded[:len(encoded)-1]...), []byte(",\"unknown\":true}")...), true, false},
		{"oversized", []byte(strings.Repeat(" ", captureRequestLimit+1)), true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fd, err := unix.MemfdCreate("restore-worker-test", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
			if err != nil {
				t.Fatal(err)
			}
			file := os.NewFile(uintptr(fd), "restore worker request test")
			defer file.Close()
			if _, err := file.Write(test.data); err != nil {
				t.Fatal(err)
			}
			if test.seal {
				if _, err := unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, captureRequestSeals); err != nil {
					t.Fatal(err)
				}
			}
			got, err := decodeRestoreWorkerRequest(file)
			if (err == nil) != test.pass {
				t.Fatal("sealed request verdict", got, err)
			}
			position, err := file.Seek(0, 1)
			if err != nil || position != int64(len(test.data)) {
				t.Fatal("reading the request changed its inherited offset", position, err)
			}
		})
	}
}
