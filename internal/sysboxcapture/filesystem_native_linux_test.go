//go:build linux

package sysboxcapture

import (
	"strings"
	"testing"
)

func nativeManifestFixture() *FilesystemSnapshot {
	source := subvolumeIdentity{FilesystemUUID: strings.Repeat("a", 32), ID: 257,
		UUID: strings.Repeat("b", 32), Generation: 7, Changed: 5, Created: 3, Device: 11, MountID: 12}
	saved := source
	saved.ID, saved.UUID, saved.ParentUUID = 258, strings.Repeat("c", 32), source.UUID
	saved.Flags = btrfsReadonlyRoot
	return &FilesystemSnapshot{Version: 2, Roots: []FilesystemTree{
		{Name: "lower", Saved: "/generation/filesystem/lower", Immutable: &ImmutableTree{}},
		{Name: "upper", Source: "/bundle/mutable/upper", Saved: "/generation/filesystem/mutable/upper",
			Native: &NativeFilesystemTree{Member: "upper", Source: source, Saved: saved}},
	}}
}

func TestNativeFilesystemManifestRequiresExplicitStorageProof(t *testing.T) {
	if err := validateFilesystemSnapshotPaths("/generation", nativeManifestFixture()); err != nil {
		t.Fatal("valid native shape", err)
	}
	cases := map[string]func(*FilesystemSnapshot){
		"legacy-version":      func(s *FilesystemSnapshot) { s.Version = 1 },
		"unknown-version":     func(s *FilesystemSnapshot) { s.Version = 3 },
		"unsealed-saved-root": func(s *FilesystemSnapshot) { s.Roots[1].Native.Saved.Flags = 0 },
		"reused-source":       func(s *FilesystemSnapshot) { s.Roots[1].Native.Saved = s.Roots[1].Native.Source },
		"wrong-lineage":       func(s *FilesystemSnapshot) { s.Roots[1].Native.Saved.ParentUUID = strings.Repeat("d", 32) },
		"escaped-generation":  func(s *FilesystemSnapshot) { s.Roots[1].Saved = "/foreign/mutable/upper" },
		"flat-upper":          func(s *FilesystemSnapshot) { s.Roots[1].Source = "/bundle/upper" },
		"wrong-member":        func(s *FilesystemSnapshot) { s.Roots[1].Native.Member = "work" },
		"false-content-hash":  func(s *FilesystemSnapshot) { s.Roots[1].SHA256 = strings.Repeat("a", 64) },
		"false-entry-count":   func(s *FilesystemSnapshot) { s.Roots[1].Entries = 1 },
		"false-hashed-bytes":  func(s *FilesystemSnapshot) { s.Roots[1].HashedBytes = 1 },
		"missing-native":      func(s *FilesystemSnapshot) { s.Roots[1].Native = nil },
		"missing-base":        func(s *FilesystemSnapshot) { s.Roots[0].Immutable = nil },
		"duplicate-root":      func(s *FilesystemSnapshot) { s.Roots = append(s.Roots, s.Roots[1]) },
		"working-checkpoint": func(s *FilesystemSnapshot) {
			s.Writable = true
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot := nativeManifestFixture()
			change(snapshot)
			if err := validateFilesystemSnapshotPaths("/generation", snapshot); err == nil {
				t.Fatal("malformed native manifest accepted")
			}
		})
	}
	native := nativeManifestFixture().Roots[1]
	if sameFilesystemTree(native, native) || sameFilesystemTree(native, FilesystemTree{}) {
		t.Fatal("native metadata acquired a legacy empty-digest equality")
	}
}

func TestNativeFilesystemIdentityAllowsOnlyBookkeepingAdvance(t *testing.T) {
	original := nativeManifestFixture().Roots[1].Native.Saved
	advanced := original
	advanced.Generation++
	if !sameNativeRoot(advanced, original) {
		t.Fatal("descendant bookkeeping invalidated unchanged native content")
	}
	for name, change := range map[string]func(*subvolumeIdentity){
		"rollback": func(s *subvolumeIdentity) { s.Generation-- },
		"content":  func(s *subvolumeIdentity) { s.Changed++ },
		"uuid":     func(s *subvolumeIdentity) { s.UUID = strings.Repeat("d", 32) },
		"id":       func(s *subvolumeIdentity) { s.ID++ },
		"mount":    func(s *subvolumeIdentity) { s.MountID++ },
		"device":   func(s *subvolumeIdentity) { s.Device++ },
		"writable": func(s *subvolumeIdentity) { s.Flags = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			current := original
			change(&current)
			if sameNativeRoot(current, original) {
				t.Fatal("changed native root retained its original authority")
			}
		})
	}
}
