//go:build linux

package lifecycle

import "testing"

func TestRetainedBackingRequiresExactOriginalStoreTranslation(t *testing.T) {
	saved := KernelObject{Device: 71, Inode: 10, MountID: 20}
	current := KernelObject{Device: 72, Inode: 10, MountID: 21}
	previous := KernelObject{Device: 71, Inode: 2, MountID: 20}
	active := KernelObject{Device: 72, Inode: 2, MountID: 21}
	if !retainedBackingObject(saved, current, previous, active) {
		t.Fatal("exact same-store inode with validated attachment translation rejected")
	}
	for _, field := range []string{"inode", "original-device", "original-mount", "current-device", "current-mount", "missing"} {
		t.Run(field, func(t *testing.T) {
			original, observed := saved, current
			switch field {
			case "inode":
				observed.Inode++
			case "original-device":
				original.Device++
			case "original-mount":
				original.MountID++
			case "current-device":
				observed.Device++
			case "current-mount":
				observed.MountID++
			case "missing":
				original.Inode, observed.Inode = 0, 0
			}
			if retainedBackingObject(original, observed, previous, active) {
				t.Fatal("retained cleanup adopted a foreign or unproved object")
			}
		})
	}
}
