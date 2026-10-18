package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"runtime-fork-experiment/pageview"
)

func retireFixtureImage(t *testing.T, socket string, file *os.File, directory string) {
	t.Helper()
	backing, err := os.CreateTemp(directory, "complete-retirement-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(backing.Name())
	defer backing.Close()
	view, err := pageview.OpenAt(socket, file)
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	var copyErr, closeErr error
	if view == nil {
		n, copyErr = io.Copy(backing, io.NewSectionReader(file, 0, 8192))
	} else {
		n, copyErr = io.Copy(backing, view)
		closeErr = view.Close()
	}
	if copyErr != nil || closeErr != nil || n != 8192 {
		t.Fatal("actual logical reader export failed", n, copyErr, closeErr)
	}
	if err := backing.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := pageview.RetireAt(socket, file, backing); err != nil {
		t.Fatal(err)
	}
	if err := pageview.RetireAt(socket, file, backing); err != nil {
		t.Fatal("exact duplicate retirement must be idempotent", err)
	}
}

func TestCompletedReaderHistoryIsBoundedWithoutLosingTotals(t *testing.T) {
	store, file := deferredFixture(t)
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.stop() })
	const cycles = 530
	var page [4096]byte
	for index := 0; index < cycles; index++ {
		view, err := pageview.OpenAt(socket, file)
		if err != nil || view == nil {
			t.Fatal("actual logical view absent", err)
		}
		n, readErr := view.ReadAt(page[:], 0)
		closeErr := view.Close()
		if n != len(page) || readErr != nil || closeErr != nil || !bytes.Equal(page[:], bytes.Repeat([]byte{17}, 4096)) {
			t.Fatal("actual private reader changed", index, n, readErr, closeErr)
		}
	}
	proof, err := service.stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(service.leases) > 512 {
		t.Fatalf("completed reader history is unbounded: records=%d", len(service.leases))
	}
	if proof["calls"] != uint64(cycles) || proof["bytes"] != uint64(cycles*4096) ||
		proof["active_client_leases"] != 0 || proof["active_client_descriptors"] != 0 {
		t.Fatal("bounded history lost real reader accounting")
	}
}

func TestRetirementReturnsLiveCaptureCredits(t *testing.T) {
	store, file := deferredFixture(t)
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.stop() })
	retireFixtureImage(t, socket, file, filepath.Dir(store.output))
	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.heldBytes != 0 || store.heldFDs != 0 {
		t.Fatalf("retired live budget leaked: bytes=%d descriptors=%d", store.heldBytes, store.heldFDs)
	}
}

func TestRetirementReleasesLargeCaptureMetadata(t *testing.T) {
	store, file := deferredFixture(t)
	image := store.images[0]
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.stop() })
	retireFixtureImage(t, socket, file, filepath.Dir(store.output))
	if len(image.pageHashes) != 0 || len(image.specs) != 0 || len(image.privateVMAStarts) != 0 ||
		image.maps != "" || image.comm != "" || len(image.reader.pipes) != 0 || len(image.reader.spans) != 0 ||
		len(image.reader.scratches) != 0 {
		t.Fatal("closed image still owns per-page/range/process metadata")
	}
}

func TestActiveCaptureAdmissionRemainsBoundedAndRollbackReturnsSlot(t *testing.T) {
	store, _, _, _, _, source := captureFixture(t)
	var images []*capturedImage
	for index := 0; index < 129; index++ {
		path := filepath.Join(store.imageRoot, "images-again", fmt.Sprintf("pages-%d.img", index+1000))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		image, err := store.begin(captureMessage{
			op: 1, a: uint64(index + 1000), b: uint64(source.Process.Pid), fds: []int{int(file.Fd())},
		})
		if index < 128 {
			if err != nil || image == nil {
				t.Fatal("active reservation failed before its bound", index, err)
			}
			images = append(images, image)
		} else {
			if err == nil || image != nil || store.liveImages != 128 {
				t.Fatal("active capture bound was weakened")
			}
			store.discard(images[0])
			store.discard(images[0])
			if store.liveImages != 127 {
				t.Fatal("rollback did not return exactly one active slot")
			}
			image, err = store.begin(captureMessage{
				op: 1, a: uint64(index + 1000), b: uint64(source.Process.Pid), fds: []int{int(file.Fd())},
			})
			if err != nil || image == nil || store.liveImages != 128 {
				t.Fatal("returned capture slot could not be reused", err)
			}
			images = append(images, image)
		}
		_ = file.Close()
	}
	for _, image := range images {
		store.discard(image)
	}
	if store.liveImages != 0 || len(store.pending) != 0 || store.heldBytes != 0 || store.heldFDs != 0 {
		t.Fatal("active admission rollback leaked state")
	}
}

func TestEmptyCaptureDoesNotConsumeALiveSlot(t *testing.T) {
	store, connection, done, file, _, source := captureFixture(t)
	if err := file.Truncate(0); err != nil {
		t.Fatal(err)
	}
	for _, message := range []captureMessage{
		{op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(file.Fd())}},
		{op: 3},
	} {
		if err := sendFixture(connection, message); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(store.images) != 1 || !store.images[0].retired || store.liveImages != 0 ||
		store.heldBytes != 0 || store.heldFDs != 0 || len(store.pending) != 0 {
		t.Fatal("empty complete capture consumed retained RAM credits")
	}
}

func TestRepeatedCaptureRetirementOutlivesHistoricalImageBound(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		name := "original-writer"
		if deferred {
			name = "deferred"
		}
		t.Run(name, func(t *testing.T) { runRepeatedCaptureRetirements(t, deferred) })
	}
}

func runRepeatedCaptureRetirements(t *testing.T, deferred bool) {
	store, first, firstDone, firstFile, firstPipe, source := captureFixtureBackend(t, false, true)
	socket := filepath.Join(t.TempDir(), "read.sock")
	service, err := startReadService(store, socket, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = service.stop() })
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{
		Name: filepath.Join(store.imageRoot, "repeat.sock"), Net: "unixpacket",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		for {
			connection, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			serverDone <- store.connection(connection)
		}
	}()
	data := append(bytes.Repeat([]byte{17}, 4096), bytes.Repeat([]byte{29}, 4096)...)
	const cycles = 257
	for index := 0; index < cycles; index++ {
		file, connection, pipe, done := firstFile, first, firstPipe, firstDone
		if index > 0 {
			path := filepath.Join(store.imageRoot, "images-again", fmt.Sprintf("pages-%d.img", index+7))
			file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
				t.Fatal(err)
			}
			if _, err := unix.FcntlInt(uintptr(pipe[1]), unix.F_SETPIPE_SZ, 8192); err != nil {
				t.Fatal(err)
			}
			if n, err := unix.Write(pipe[1], data); err != nil || n != len(data) {
				t.Fatal("actual retained pipe fixture failed", n, err)
			}
			connection, err = net.DialUnix("unixpacket", nil, listener.Addr().(*net.UnixAddr))
			if err != nil {
				t.Fatal(err)
			}
			done = serverDone
		}
		if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var captureMode, privateLane uint64
		if deferred {
			captureMode, privateLane = 1, 2
			if _, err := file.WriteAt(make([]byte, 4096), 0); err != nil {
				t.Fatal(err)
			}
			if err := unix.Fsetxattr(int(file.Fd()), deferredImageAttribute, []byte{1}, 0); err != nil {
				t.Fatal(err)
			}
		}
		if index == 129 {
			oldest := store.images[0]
			collision := filepath.Join(store.output, "retired-images", retiredArchiveName(oldest.name))
			if err := os.WriteFile(collision, []byte("owned fault-injection sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := sendFixture(connection, captureMessage{
				op: 1, a: uint64(index + 7), b: uint64(source.Process.Pid), c: captureMode,
				fds: []int{int(file.Fd())},
			}); err == nil {
				t.Fatal("capture continued after retirement archive collision")
			}
			if err := <-done; err == nil || !strings.Contains(err.Error(), "retirement archive") {
				t.Fatal("archive write failure was not propagated", err)
			}
			if len(store.images) != 128 || store.images[0] != oldest ||
				store.liveImages != 0 || store.heldBytes != 0 || store.heldFDs != 0 {
				t.Fatal("failed archive pruned an identity or reserved live credits")
			}
			if err := os.Remove(collision); err != nil {
				t.Fatal(err)
			}
			_ = connection.Close()
			connection, err = net.DialUnix("unixpacket", nil, listener.Addr().(*net.UnixAddr))
			if err != nil {
				t.Fatal(err)
			}
			if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
		}
		if index == 130 {
			store.mu.Lock()
			_, persistErr := store.persistRetiredImageLocked(store.images[0])
			store.mu.Unlock()
			if persistErr != nil {
				t.Fatal("actual persisted-before-pruned transition failed", persistErr)
			}
		}
		for _, message := range []captureMessage{
			{op: 1, a: uint64(index + 7), b: uint64(source.Process.Pid), c: captureMode, fds: []int{int(file.Fd())}},
			{op: 2, b: 8192, c: 2, fds: []int{pipe[0]}, spans: []captureSpan{
				{0, 4096, 0, privateLane}, {4096, 4096, 4096, 1},
			}},
			{op: 3, a: 8192},
		} {
			if err := sendFixture(connection, message); err != nil {
				t.Fatalf("capture %d of%d failed: %v", index+1, cycles, err)
			}
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		retireFixtureImage(t, socket, file, filepath.Dir(store.output))
		_ = connection.Close()
		if index > 0 {
			_ = file.Close()
			_ = unix.Close(pipe[0])
			_ = unix.Close(pipe[1])
		}
		if len(store.images) > 256 {
			t.Fatal("successful retirement produced an unbounded in-memory catalog", len(store.images))
		}
	}
	if store.heldBytes != 0 || store.heldFDs != 0 {
		t.Fatal("repeated captures leaked live credits", store.heldBytes, store.heldFDs)
	}
	if len(store.images) != 128 || store.archivedImages != 129 || store.liveImages != 0 {
		t.Fatal("bounded catalog or archived identities differ")
	}
	replay, err := net.DialUnix("unixpacket", nil, listener.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	if err := sendFixture(replay, captureMessage{
		op: 1, a: 7, b: uint64(source.Process.Pid), fds: []int{int(firstFile.Fd())},
	}); err == nil {
		t.Fatal("catalog pruning allowed immutable identity reuse")
	}
	if err := <-serverDone; err == nil || !strings.Contains(err.Error(), "previously retired") {
		t.Fatal("pruned identity replay was not rejected", err)
	}
	_ = replay.Close()
	if _, err := store.verifyArchivedRetirements(); err == nil || !strings.Contains(err.Error(), "source still alive") {
		t.Fatal("archiving hid a still-running original source", err)
	}
	if err := source.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = source.Wait()
	if _, err := store.verifyArchivedRetirements(); err != nil {
		t.Fatal(err)
	}
	archived := filepath.Join(store.output, "retired-images", retiredArchiveName(firstFile.Name()))
	saved, err := os.ReadFile(archived)
	if err != nil {
		t.Fatal(err)
	}
	var record archivedRetirement
	if err := json.Unmarshal(saved, &record); err != nil {
		t.Fatal(err)
	}
	record.SHA256 = strings.Repeat("0", 64)
	changed, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archived, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.verifyArchivedRetirements(); err == nil || !strings.Contains(err.Error(), "archive changed") {
		t.Fatal("changed durable retirement evidence was accepted", err)
	}
	if err := os.WriteFile(archived, saved, 0600); err != nil {
		t.Fatal(err)
	}
	store.readService = service
	proof, err := store.finish()
	if err != nil {
		t.Fatal(err)
	}
	if proof["registered_pipe_bytes"] != int64(cycles*8192) {
		t.Fatal("cumulative capture evidence lost retired generations")
	}
	encoded, err := json.Marshal(proof)
	if err != nil || len(encoded) > 1<<20 {
		t.Fatal("final bounded proof failed", len(encoded), err)
	}
	t.Logf("real SCM_RIGHTS captures/verified backing retirements=%d live_bytes=%d live_fds=%d catalog=%d proof_bytes=%d",
		cycles, store.heldBytes, store.heldFDs, len(store.images), len(encoded))
}
