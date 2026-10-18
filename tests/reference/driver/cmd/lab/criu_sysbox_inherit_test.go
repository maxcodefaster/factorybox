package main

import "testing"

func TestInheritGuardRequiresOwnedFreshSource(t *testing.T) {
	for _, name := range []string{"CRIU_SYSBOX_WARM_PREPARE", "CRIU_SYSBOX_PRE_DUMP",
		"CRIU_SYSBOX_ASYNC_TRACK", "CRIU_SYSBOX_DELTA", "CRIU_SYSBOX_RAM_BASE", "CRIU_SYSBOX_REPEAT_DELTA"} {
		t.Setenv(name, "0")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_INHERIT", "1")
	for _, name := range []string{"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_RETAIN_SOURCE", "CRIU_SYSBOX_PROBE"} {
		t.Setenv(name, "1")
	}
	if err := criuInheritGuard("/experiment", "source-sysbox"); err != nil {
		t.Fatal(err)
	}
	for _, item := range [][2]string{{"/other", "source-sysbox"}, {"/experiment", "copy-sysbox"}} {
		if err := criuInheritGuard(item[0], item[1]); err == nil {
			t.Fatalf("accepted ownership outside source: %v", item)
		}
	}
	for _, name := range []string{"CRIU_SYSBOX_WARM_PREPARE", "CRIU_SYSBOX_PRE_DUMP",
		"CRIU_SYSBOX_ASYNC_TRACK", "CRIU_SYSBOX_DELTA", "CRIU_SYSBOX_RAM_BASE", "CRIU_SYSBOX_REPEAT_DELTA"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "1")
			if err := criuInheritGuard("/experiment", "source-sysbox"); err == nil {
				t.Fatalf("accepted competing snapshot mode %s", name)
			}
		})
	}
	t.Setenv("CRIU_SYSBOX_LAZY_INHERIT", "2")
	if err := criuInheritGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("accepted invalid inheritance switch")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_INHERIT", "0")
	if err := criuInheritGuard("/other", "unrelated"); err != nil {
		t.Fatalf("disabled candidate changed existing control: %v", err)
	}
}

func TestInheritManifestRetainsImagesAndPidAliases(t *testing.T) {
	for _, name := range []string{"inventory.img", "pages-1.img", "pagemap-12.img", "lazy-parent-12.bin"} {
		if !criuInheritParentEntry(name) {
			t.Fatalf("omitted immutable parent payload %q", name)
		}
	}
	for _, name := range []string{"lazy-pages.socket", "lazy-pages.log", "restore.log", "stats-restore",
		"lazy-parent-0.bin", "lazy-parent-x.bin"} {
		if criuInheritParentEntry(name) {
			t.Fatalf("included mutable/invalid parent metadata %q", name)
		}
	}
}

func TestLazyCopiedPageTrackingRequiresOwnedInheritance(t *testing.T) {
	for _, name := range []string{"CRIU_SYSBOX_WARM_PREPARE", "CRIU_SYSBOX_PRE_DUMP",
		"CRIU_SYSBOX_ASYNC_TRACK", "CRIU_SYSBOX_DELTA", "CRIU_SYSBOX_RAM_BASE",
		"CRIU_SYSBOX_REPEAT_DELTA", "CRIU_SYSBOX_PARENT_MANIFEST_REUSE"} {
		t.Setenv(name, "0")
	}
	for _, name := range []string{"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_RETAIN_SOURCE", "CRIU_SYSBOX_PROBE"} {
		t.Setenv(name, "1")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_INHERIT", "1")
	t.Setenv("CRIU_SYSBOX_LAZY_WP", "1")
	if err := criuInheritGuard("/experiment", "source-sysbox"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "0"} {
		t.Run("without-inheritance-"+value, func(t *testing.T) {
			t.Setenv("CRIU_SYSBOX_LAZY_INHERIT", value)
			if err := criuInheritGuard("/experiment", "source-sysbox"); err == nil {
				t.Fatal("accepted copied-page tracking without inherited page provenance")
			}
		})
	}
	t.Setenv("CRIU_SYSBOX_LAZY_WP", "true")
	if err := criuInheritGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("accepted invalid copied-page tracking switch")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_WP", "0")
	t.Setenv("CRIU_SYSBOX_LAZY_INHERIT", "0")
	if err := criuInheritGuard("/other", "unrelated"); err != nil {
		t.Fatalf("disabled copied-page tracking changed the control: %v", err)
	}
}
