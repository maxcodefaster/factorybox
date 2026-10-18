package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"runtime-fork-experiment/pageview"
)

func TestRetirementMismatchDistinguishesExportFromSnapshotData(t *testing.T) {
	store, file := rawBaseFixture(t, false)
	target, err := os.CreateTemp(filepath.Dir(store.output), "bad-raw-backing-")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	data := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	if _, err := target.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := target.WriteAt([]byte{201}, 6000); err != nil {
		t.Fatal(err)
	}
	if err := target.Sync(); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := pageview.RetireAt(socket, file, target); err == nil {
		t.Fatal("wrong complete backing was accepted")
	}
	proof, err := service.stop()
	if err == nil || len(proof["failures"].([]string)) == 0 {
		t.Fatal("retirement failure was hidden")
	}
	paths, err := filepath.Glob(filepath.Join(store.output, "retirement-mismatch-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("actual mismatch evidence absent", paths, err)
	}
	bytes, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(bytes, &report); err != nil {
		t.Fatal(err)
	}
	if report["page_offset"] != float64(4096) || report["logical_still_matches_capture"] != true ||
		report["backing_matches_current_logical"] != false {
		t.Fatal("actual corrupt export was misclassified", report)
	}
	if store.images[0].retired {
		t.Fatal("invalid backing released the snapshot")
	}
}

func TestUnprovedPrivateSpanUsesOriginalWriterBytes(t *testing.T) {
	t.Setenv("RUNTIME_FORK_PIPE_GUARD_AUDIT", "1")
	for _, flag := range []uint64{8, 16} {
		t.Run(fmt.Sprint(flag), func(t *testing.T) {
			store, connection, done, file, pipe, source := captureFixtureBackend(t, false, true)
			for _, message := range []captureMessage{
				{op: 1, a: 7, b: uint64(source.Process.Pid), c: 1, fds: []int{int(file.Fd())}},
				{op: 2, b: 8192, c: 2, fds: []int{pipe[0]}, spans: []captureSpan{
					{0, 4096, 0, 1 | flag}, {4096, 4096, 4096, 1},
				}},
				{op: 3, a: 8192},
			} {
				if err := sendFixture(connection, message); err != nil {
					t.Fatal(err)
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.discard(store.images[0]) })
			changed := make([]byte, 8192)
			if n, err := unix.Read(pipe[0], changed); err != nil || n != len(changed) {
				t.Fatal("fixture original-pipe drain", n, err)
			}
			if n, err := unix.Write(pipe[1], bytes.Repeat([]byte{99}, 8192)); err != nil || n != 8192 {
				t.Fatal("fixture original-pipe overwrite", n, err)
			}
			_ = source.Process.Kill()
			_ = source.Wait()
			proof, err := store.finish()
			if err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(filepath.Join(store.output, "capture-000-pages-7.img"))
			expected := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
			if err != nil || !bytes.Equal(saved, expected) {
				t.Fatal("unproved page aliased later source writes", err)
			}
			record := proof["images"].([]map[string]any)[0]
			name := "cow_unshared_eager_bytes"
			if flag == 16 {
				name = "cow_unknown_eager_bytes"
			}
			if record[name] != int64(4096) || record["private_pipe_bytes"] != int64(0) {
				t.Fatal("guard fallback accounting differs", record)
			}
			if record["guard_audit_changed_pages"] != int64(1) ||
				record["guard_audit_checked_bytes"] != int64(4096) {
				t.Fatal("safe copy passed but the unsafe original-page audit was absent", record)
			}
			if proof["remaining_guard_audit_pipe_bytes"] != int64(0) ||
				proof["remaining_guard_audit_fds"] != 0 {
				t.Fatal("guard audit retained descriptor or pipe credits")
			}
		})
	}
}

func TestObserverRetirementRejectsChangedPrivateBytesWithCorrectBacking(t *testing.T) {
	store, connection, done, file, pipe, source := captureFixture(t)
	for _, message := range []captureMessage{
		{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}},
		{op: 2, b: 8192, c: 2, fds: []int{pipe[0]}, spans: []captureSpan{
			{0, 4096, 0, 0}, {4096, 4096, 4096, 1},
		}},
		{op: 3, a: 8192},
	} {
		if err := sendFixture(connection, message); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.discard(store.images[0]) })
	target, err := os.CreateTemp(filepath.Dir(store.output), "observer-correct-backing-")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	expected := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	if _, err := target.Write(expected); err != nil {
		t.Fatal(err)
	}
	if err := target.Sync(); err != nil {
		t.Fatal(err)
	}
	// Inject changed bytes through the fixture's actual FIFO while preserving
	// its identity, length and every original raw-file/backing byte. This
	// tests the observer's detection, not a proposed cause of the live bug.
	changed := make([]byte, len(expected))
	if n, err := unix.Read(pipe[0], changed); err != nil || n != len(changed) {
		t.Fatal("fixture drain", n, err)
	}
	changed[143] ^= 1
	if n, err := unix.Write(pipe[1], changed); err != nil || n != len(changed) {
		t.Fatal("fixture replacement", n, err)
	}
	socket := filepath.Join(t.TempDir(), "r.sock")
	service, err := startReadService(store, socket, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.stop() })
	if err := pageview.RetireAt(socket, file, target); err == nil {
		t.Fatal("observer accepted changed retained bytes because complete backing was correct")
	}
	paths, err := filepath.Glob(filepath.Join(store.output, "retirement-mismatch-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("changed private-byte diagnostic absent", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	offsets, _ := report["changed_byte_offsets_in_page"].([]any)
	if report["page_offset"] != float64(0) || report["logical_still_matches_capture"] != false ||
		report["backing_still_matches_capture"] != true || report["raw_still_matches_capture"] != true ||
		len(offsets) != 1 || offsets[0] != float64(143) || store.images[0].retired {
		t.Fatal("changed private bytes were misclassified or released", report)
	}
}

func TestDeferredMismatchUsesBoundedCapturePreimage(t *testing.T) {
	t.Setenv("RUNTIME_FORK_PIPE_PREIMAGES", "1")
	store, file := deferredFixture(t)
	image := store.images[0]
	lease := image.reader.spans[0].pipe
	writer, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", lease.fd), os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	changed := make([]byte, lease.available)
	if n, err := unix.Read(lease.fd, changed); err != nil || n != len(changed) {
		t.Fatal("fixture private-pipe drain", n, err)
	}
	changed[143] ^= 1
	if n, err := writer.Write(changed); err != nil || n != len(changed) {
		t.Fatal("fixture private-pipe replacement", n, err)
	}
	target, err := os.CreateTemp(filepath.Dir(store.output), "changed-deferred-backing-")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if _, err := target.Write(changed); err != nil {
		t.Fatal(err)
	}
	if err := target.Sync(); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "r.sock")
	service, err := startReadService(store, socket, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.stop() })
	if err := pageview.RetireAt(socket, file, target); err == nil {
		t.Fatal("changed deferred image was accepted")
	}
	paths, err := filepath.Glob(filepath.Join(store.output, "retirement-mismatch-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("deferred mismatch diagnostic absent", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	offsets, _ := report["changed_byte_offsets_in_page"].([]any)
	if report["capture_preimage_available"] != true || report["raw_still_matches_capture"] != false ||
		report["backing_matches_current_logical"] != true ||
		len(offsets) != 1 || offsets[0] != float64(143) || image.retired {
		t.Fatal("deferred private-byte change lost its original preimage", report)
	}
	captured, err := os.ReadFile(strings.TrimSuffix(paths[0], ".json") + "-capture.bin")
	if err != nil || !bytes.Equal(captured, bytes.Repeat([]byte{17}, 4096)) {
		t.Fatal("actual original captured page was not preserved", err)
	}
}

// Real descriptor capture/commit with the optional original-image backend.
// Private bytes stay in the exact FIFO spans retained by capture.
func rawBaseFixture(t *testing.T, ownTmpfs bool) (*captureStore, *os.File) {
	t.Helper()
	return deferredFixtureBackend(t, ownTmpfs, true)
}

func TestActualCReaderAcceptsExactReadonlyRawBaseAndRetiresAnchor(t *testing.T) {
	t.Setenv("RUNTIME_FORK_PIPE_PREIMAGES", "1")
	store, file := rawBaseFixture(t, true)
	path := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := unix.Mount("", store.imageRoot, "tmpfs", unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV, ""); err != nil {
		t.Fatal(err)
	}
	if writer, err := os.OpenFile(path, os.O_WRONLY, 0); !errors.Is(err, unix.EROFS) {
		if writer != nil {
			_ = writer.Close()
		}
		t.Fatal("actual kernel raw-image mount did not reject writes", err)
	}
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.stop() })
	nativeReadCommand(t, socket, file.Name(), "normal")
	view, err := pageview.OpenAt(socket, file)
	if err != nil || view == nil {
		t.Fatalf("readonly raw logical view: %v", err)
	}
	data := make([]byte, 8192)
	if n, err := view.ReadAt(data, 0); err != nil || n != len(data) ||
		!bytes.Equal(data[:4096], bytes.Repeat([]byte{17}, 4096)) ||
		!bytes.Equal(data[4096:], bytes.Repeat([]byte{29}, 4096)) {
		t.Fatal("raw holes escaped logical image", n, err)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	target, err := os.CreateTemp(filepath.Dir(store.output), "raw-complete-backing-")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if _, err := target.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := target.Sync(); err != nil {
		t.Fatal(err)
	}
	baseFD := store.images[0].reader.baseFD
	if err := pageview.RetireAt(socket, file, target); err != nil {
		t.Fatal(err)
	}
	if _, _, err := identity(baseFD); !errors.Is(err, unix.EBADF) {
		t.Fatal("raw snapshot anchor remained after retirement ACK", err)
	}
	if store.preimageBytes != 0 || store.peakPreimageBytes != 4096 || store.images[0].preimages != nil {
		t.Fatal("diagnostic capture preimages remained after retirement ACK")
	}
	proof, err := service.stop()
	if err != nil {
		t.Fatalf("raw reader evidence: %v %+v", err, proof)
	}
}

func TestCRIURejectsWritableRawBase(t *testing.T) {
	store, file := rawBaseFixture(t, false)
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, false)
	if err != nil {
		t.Fatal(err)
	}
	nativeReadCommand(t, socket, file.Name(), "reject")
	proof, err := service.stop()
	if err == nil || len(proof["failures"].([]string)) == 0 {
		t.Fatal("broker did not reject writable raw image before CRIU lease", proof)
	}
}

func TestTrustedGoConstructionReadsRawBeforeReadonlyAndRejectsMutation(t *testing.T) {
	store, file := rawBaseFixture(t, false)
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.stop() })
	view, err := pageview.OpenAt(socket, file)
	if err != nil || view == nil {
		t.Fatalf("trusted raw construction read: %v", err)
	}
	data := make([]byte, 8192)
	if n, err := view.ReadAt(data, 0); err != nil || n != len(data) ||
		!bytes.Equal(data[:4096], bytes.Repeat([]byte{17}, 4096)) ||
		!bytes.Equal(data[4096:], bytes.Repeat([]byte{29}, 4096)) {
		t.Fatal("trusted construction bytes differ", n, err)
	}
	time.Sleep(time.Millisecond)
	if _, err := file.WriteAt([]byte{211}, 6000); err != nil {
		t.Fatal(err)
	}
	if n, err := view.ReadAt(data[4096:], 4096); err == nil || n != 0 {
		t.Fatal("changed raw backend was read as immutable", n, err)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	proof, err := service.stop()
	if err == nil || len(proof["failures"].([]string)) == 0 {
		t.Fatal("raw mutation was not accounted as reader failure", proof)
	}
}
