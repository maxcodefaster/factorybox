//go:build linux

package sysboxcapture

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Exercise the capture/materialization entry points, not just snapshot ioctls.
// The ordinary complete byte/metadata oracle stays outside the capture path.
func TestFilesystemSnapshotManifestNative(t *testing.T) {
	if os.Getenv("FACTORYBOX_NATIVE_COW_MANIFEST") != "1" {
		t.Skip("requires the separately owned native manifest fixture")
	}
	ctx := context.Background()
	root, err := os.MkdirTemp("/var/lib/sysbox/fixed-store/data/store", "native-manifest-")
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []string
	var mounted []string
	command := func(arguments ...string) {
		t.Helper()
		if output, err := exec.Command("btrfs", arguments...).CombinedOutput(); err != nil {
			t.Fatal(string(output), err)
		}
	}
	t.Cleanup(func() {
		for i := len(mounted) - 1; i >= 0; i-- {
			if err := unix.Unmount(mounted[i], 0); err != nil {
				t.Error(err)
				return
			}
		}
		for i := len(snapshots) - 1; i >= 0; i-- {
			if _, err := os.Lstat(snapshots[i]); errors.Is(err, os.ErrNotExist) {
				continue
			}
			if output, err := exec.Command("btrfs", "subvolume", "delete", "--", snapshots[i]).CombinedOutput(); err != nil {
				t.Error(string(output), err)
				return
			}
		}
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	for _, path := range []string{"import", "bundle", "generation"} {
		if err := os.Mkdir(root+"/"+path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(root+"/import/base-marker", []byte("imported base"), 0640); err != nil {
		t.Fatal(err)
	}
	base, err := PrepareFilesystemBase(ctx, root+"/import", root+"/base")
	if err != nil {
		t.Fatal(err)
	}
	mounted = append(mounted, root+"/base")
	lower, err := bindImmutableTree(base.Saved, root+"/bundle/lower", base)
	if err != nil {
		t.Fatal(err)
	}
	mounted = append(mounted, root+"/bundle/lower")
	for _, path := range []string{root + "/bundle/mutable", root + "/docker"} {
		command("subvolume", "create", "--", path)
		snapshots = append(snapshots, path)
	}
	for _, name := range []string{"upper", "work"} {
		if err := os.Mkdir(root+"/bundle/mutable/"+name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(root+"/docker/covered", 0700); err != nil {
		t.Fatal(err)
	}
	upper := root + "/bundle/mutable/upper"
	for _, path := range []string{upper + "/marker", root + "/docker/marker"} {
		if err := os.WriteFile(path, []byte("captured private data"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(upper+"/marker", upper+"/hardlink"); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(upper+"/marker", "user.factorybox", []byte("metadata"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root+"/bundle/rootfs", 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("overlay", root+"/bundle/rootfs", "overlay", 0,
		"index=off,lowerdir="+root+"/bundle/lower,upperdir="+upper+",workdir="+root+"/bundle/mutable/work"); err != nil {
		t.Fatal(err)
	}
	mounted = append(mounted, root+"/bundle/rootfs")
	if err := CheckOverlayLayout(root + "/bundle"); err != nil {
		t.Fatal("real native common upper/work overlay admission", err)
	}
	if err := os.WriteFile(root+"/bundle/rootfs/base-marker", []byte("native copy-up"), 0640); err != nil {
		t.Fatal("native source cannot copy up an existing lower file", err)
	}
	roots := []filesystemRoot{
		{Name: "lower", Source: lower.Saved, Expected: &lower},
		{Name: "upper", Source: upper},
		{Name: "volumes/docker", Source: root + "/docker"},
	}
	// If capture opens payload data, this watch records it. Snapshot metadata
	// admission must not secretly fall back to the full-content copier.
	watch, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(watch)
	for _, path := range []string{upper + "/marker", root + "/docker/marker"} {
		if _, err := unix.InotifyAddWatch(watch, path, unix.IN_OPEN); err != nil {
			t.Fatal(err)
		}
	}
	captured, err := copyFilesystem(ctx, roots, root+"/generation/filesystem")
	if err != nil {
		t.Fatal(err)
	}
	mounted = append(mounted, root+"/generation/filesystem/lower")
	if captured.Version != 2 {
		t.Fatal("snapshot-capable capture still used the complete-file copy manifest", captured.Version)
	}
	snapshots = append(snapshots, root+"/generation/filesystem/mutable", root+"/generation/filesystem/volumes/docker")
	buffer := make([]byte, 4096)
	if count, err := unix.Read(watch, buffer); count > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("native capture opened private payload bytes", count, err)
	}
	if err := VerifyFilesystemSnapshot(ctx, root+"/generation", captured); err != nil {
		t.Fatal("native checkpoint verification", err)
	}
	for _, tree := range captured.Roots[1:] {
		if err := VerifyRetainedFilesystemTree(ctx, tree, nil); err != nil {
			t.Fatal("unchanged native retained root", err)
		}
		refused := errors.New("original store does not authorize translation")
		if err := VerifyRetainedFilesystemTree(ctx, tree, func(FilesystemTree, uint64) error {
			return refused
		}); !errors.Is(err, refused) {
			t.Fatal("unchanged namespace-local numbers bypassed rejected store authority", err)
		}
		changed := tree
		native := *tree.Native
		native.Saved.MountID++
		changed.Native = &native
		if err := VerifyRetainedFilesystemTree(ctx, changed, nil); err == nil {
			t.Fatal("retained root translated a mount without store authority")
		}
		if err := VerifyRetainedFilesystemTree(ctx, changed, func(FilesystemTree, uint64) error {
			return refused
		}); !errors.Is(err, refused) {
			t.Fatal("retained root ignored rejected original-store translation", err)
		}
		native.Saved.Changed++
		if err := VerifyRetainedFilesystemTree(ctx, changed, func(FilesystemTree, uint64) error {
			return nil
		}); err == nil {
			t.Fatal("device translation also authorized changed snapshot content")
		}
	}
	for index, original := range roots[1:] {
		saved := captured.Roots[index+1]
		if saved.SHA256 != "" || saved.HashedBytes != 0 || saved.SourceReadUS != 0 || saved.TargetReadUS != 0 {
			t.Fatal("native identity was misrepresented as a content hash", saved)
		}
		source, err := openDirectory(original.Source)
		if err != nil {
			t.Fatal(err)
		}
		target, err := openDirectory(saved.Saved)
		if err != nil {
			source.Close()
			t.Fatal(err)
		}
		before, firstErr := fingerprintTree(ctx, source, nil, false)
		after, secondErr := fingerprintTree(ctx, target, source, false)
		closeErr := errors.Join(source.Close(), target.Close())
		if err := errors.Join(firstErr, secondErr, closeErr); err != nil || !sameFilesystemTree(before, after) {
			t.Fatal("native capture differs from the independent complete-content oracle", err)
		}
		if err := os.WriteFile(saved.Saved+"/marker", []byte("forbidden"), 0600); !errors.Is(err, unix.EROFS) {
			t.Fatal("captured root is not globally read-only", err)
		}
	}
	if err := VerifyLiveFilesystemSnapshot(ctx, captured); err != nil {
		t.Fatal("original stopped private backing", err)
	}
	savedWatch, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(savedWatch)
	for _, tree := range captured.Roots[1:] {
		if _, err := unix.InotifyAddWatch(savedWatch, tree.Saved+"/marker", unix.IN_OPEN); err != nil {
			t.Fatal(err)
		}
	}
	left, err := MaterializeFilesystemSnapshot(ctx, root+"/generation", root+"/left", captured)
	if err != nil {
		t.Fatal("native materialization", err)
	}
	mounted = append(mounted, root+"/left/filesystem/lower")
	snapshots = append(snapshots, root+"/left/filesystem/mutable", root+"/left/filesystem/volumes/docker")
	if err := VerifyFilesystemSnapshot(ctx, root+"/left", left); err != nil {
		t.Fatal("new native writable descendant", err)
	}
	// Saved parent generation may advance during descendant creation. Its
	// original data identity and content-change transaction must not change.
	if err := VerifyFilesystemSnapshot(ctx, root+"/generation", captured); err != nil {
		t.Fatal("materialization invalidated the sealed source", err)
	}
	if count, err := unix.Read(savedWatch, buffer); count > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatal("native materialization or verification opened saved payload bytes", count, err)
	}
	if err := os.Mkdir(root+"/left/filesystem/rootfs", 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount("overlay", root+"/left/filesystem/rootfs", "overlay", 0,
		"index=off,lowerdir="+root+"/left/filesystem/lower,upperdir="+left.Roots[1].Saved+
			",workdir="+root+"/left/filesystem/mutable/work"); err != nil {
		t.Fatal("native descendant cannot mount its captured common upper/work root", err)
	}
	mounted = append(mounted, root+"/left/filesystem/rootfs")
	if err := CheckOverlayLayout(root + "/left/filesystem"); err != nil {
		t.Fatal("materialized overlay layout admission", err)
	}
	if err := os.WriteFile(root+"/left/filesystem/rootfs/base-marker", []byte("independent child copy-up"), 0640); err != nil {
		t.Fatal("native descendant cannot write its captured upper", err)
	}
	if data, err := os.ReadFile(root + "/bundle/rootfs/base-marker"); err != nil || string(data) != "native copy-up" {
		t.Fatal("native descendant changed the original overlay", string(data), err)
	}
	if err := os.WriteFile(left.Roots[2].Saved+"/marker", []byte("independent writable descendant"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFilesystemSnapshot(ctx, root+"/left", left); err == nil {
		t.Fatal("modified writable materialization still matched its preparation")
	}
	if err := VerifyFilesystemSnapshot(ctx, root+"/generation", captured); err != nil {
		t.Fatal("independent write changed the saved checkpoint", err)
	}
	t.Run("cold-native-retirement", func(t *testing.T) {
		working := left.Roots[2]
		if _, err := RebindNativeFilesystemRetirement(ctx, working, nil); err == nil {
			t.Fatal("accepted retirement silently adopted later working writes")
		}
		observed, err := ObserveRetainedNativeFilesystemRetirement(ctx, working, nil)
		if err != nil || observed.Native.Saved.Flags != 0 {
			t.Fatal("observe owned cold working root without changing its flags", err)
		}
		refused := errors.New("original store does not authorize retirement")
		if _, err := RebindNativeFilesystemRetirement(ctx, observed, func(FilesystemTree, uint64) error {
			return refused
		}); !errors.Is(err, refused) {
			t.Fatal("reused local numbers bypassed retirement store authority", err)
		}
		if err := CheckNativeFilesystemRetirement(ctx, observed); err == nil {
			t.Fatal("cold working proof broadened the sealed-checkpoint API")
		}
		if err := os.WriteFile(observed.Saved+"/marker", []byte("changed after observation"), 0640); err != nil {
			t.Fatal(err)
		}
		if err := CheckColdNativeFilesystemRetirement(ctx, observed); err == nil {
			t.Fatal("cold retirement adopted writes after its exact observation")
		}
		observed, err = ObserveRetainedNativeFilesystemRetirement(ctx, working, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := RetireColdNativeFilesystemTree(ctx, observed); err != nil {
			t.Fatal("withdraw exact cold working subvolume", err)
		}
		if err := RetireColdNativeFilesystemTree(ctx, observed); err != nil {
			t.Fatal("replay caller-authorized absent cold root", err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if err := WaitColdNativeFilesystemRetirement(cancelled, observed); !errors.Is(err, context.Canceled) {
			t.Fatal("cold cleaning observation ignored cancellation", err)
		}
		if err := VerifyFilesystemSnapshot(ctx, root+"/generation", captured); err != nil {
			t.Fatal("cold working retirement changed its sealed ancestor", err)
		}
	})
	if err := os.WriteFile(root+"/docker/marker", []byte("source advanced"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := VerifyLiveFilesystemSnapshot(ctx, captured); err == nil {
		t.Fatal("changed original working volume was accepted")
	}
	// The version and storage proof are explicit. Empty legacy digests must
	// never acquire native verification merely because both compare equal.
	data, _ := json.Marshal(captured)
	var legacy FilesystemSnapshot
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.Version = 1
	if err := VerifyFilesystemSnapshot(ctx, root+"/generation", &legacy); err == nil {
		t.Fatal("native storage proof was silently accepted as a legacy hash")
	}
	covered := captured.Roots[2].Saved
	if err := unix.Mount(covered, covered, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	rejected := VerifyFilesystemSnapshot(ctx, root+"/generation", captured)
	if err := unix.Unmount(covered, 0); err != nil {
		t.Fatal(err)
	}
	if rejected == nil {
		t.Fatal("same-data covering mount acquired native snapshot authority")
	}
	t.Run("capture-admission", func(t *testing.T) {
		if err := unix.Mount(root+"/import", root+"/docker/covered", "", unix.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		_, rejected := copyFilesystem(ctx, roots, root+"/foreign-mount")
		if err := unix.Unmount(root+"/docker/covered", 0); err != nil {
			t.Fatal(err)
		}
		if rejected == nil || !strings.Contains(rejected.Error(), "descendant mount") {
			t.Fatal("capture did not refuse a foreign descendant before creating backing", rejected)
		}
		if _, err := os.Lstat(root + "/foreign-mount"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rejected capture created partial backing", err)
		}
		command("subvolume", "create", "--", root+"/docker/nested")
		snapshots = append(snapshots, root+"/docker/nested")
		_, rejected = copyFilesystem(ctx, roots, root+"/nested-subvolume")
		if !errors.Is(rejected, errNestedSubvolume) {
			t.Fatal("capture would silently omit a nested subvolume", rejected)
		}
		command("subvolume", "delete", "--", root+"/docker/nested")
		if err := os.Rename(root+"/docker", root+"/original-docker"); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, root+"/original-docker")
		if err := os.Mkdir(root+"/docker", 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := copyFilesystem(ctx, roots, root+"/mixed-layout"); err == nil {
			t.Fatal("native capture silently fell back to copying an ordinary private root")
		}
		if err := os.Remove(root + "/docker"); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(root+"/original-docker", root+"/docker"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("same-name-replacement", func(t *testing.T) {
		original := root + "/retained-original-docker"
		source, err := openDirectory(covered)
		if err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, original)
		_, cloneErr := cloneSubvolume(ctx, source, original, true)
		identity, inspectErr := inspectSubvolume(source)
		if err := errors.Join(cloneErr, inspectErr, source.Close()); err != nil {
			t.Fatal(err)
		}
		// Read-only subvolumes are not made writable merely to manufacture
		// a replacement. Retire the exact original ID, then create another
		// sealed root with the same bytes at its old path.
		if err := retireSubvolume(ctx, covered, identity); err != nil {
			t.Fatal(err)
		}
		source, err = openDirectory(original)
		if err != nil {
			t.Fatal(err)
		}
		_, cloneErr = cloneSubvolume(ctx, source, covered, true)
		if err := errors.Join(cloneErr, source.Close()); err != nil {
			t.Fatal(err)
		}
		if err := CheckNativeFilesystemRetirement(ctx, captured.Roots[2]); err == nil {
			t.Fatal("retirement admission accepted a same-name native replacement")
		}
		if err := RetireNativeFilesystemTree(ctx, captured.Roots[2]); err == nil {
			t.Fatal("native retirement deleted a same-name replacement")
		}
		if err := VerifyFilesystemSnapshot(ctx, root+"/generation", captured); err == nil {
			t.Fatal("same-name same-data root replacement retained snapshot authority")
		}
	})
	for _, tree := range captured.Roots {
		if !strings.HasPrefix(tree.Saved, filepath.Join(root, "generation", "filesystem")+"/") {
			t.Fatal("native manifest escaped its generation", tree.Saved)
		}
		if tree.Name != "lower" {
			t.Logf("%s native capture=%dus, no source/target payload reads", tree.Name, tree.CaptureUS)
		}
	}
}
