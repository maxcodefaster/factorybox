package main

import "testing"

func TestCopyPipelineRequiresOwnedFrozenSourceMode(t *testing.T) {
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_RAM_CAPTURE", "CRIU_SYSBOX_COPY_PIPELINE"} {
		t.Setenv(name, "1")
	}
	t.Setenv("CRIU_SYSBOX_PROFILE", "0")
	t.Setenv("CRIU_SYSBOX_COPY_PIPELINE_ABORT", "0")
	if err := criuCopyPipelineGuard("/experiment", "source-sysbox"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_RAM_CAPTURE"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "0")
			if err := criuCopyPipelineGuard("/experiment", "source-sysbox"); err == nil {
				t.Fatalf("pipeline accepted without %s", name)
			}
		})
	}
	for _, pair := range [][2]string{{"/other", "source-sysbox"}, {"/experiment", "copy-sysbox"}} {
		if err := criuCopyPipelineGuard(pair[0], pair[1]); err == nil {
			t.Fatalf("pipeline accepted unrelated owner %v", pair)
		}
	}
	t.Setenv("CRIU_SYSBOX_PROFILE", "1")
	if err := criuCopyPipelineGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("pipeline accepted misleading command-descendant profile")
	}
	t.Setenv("CRIU_SYSBOX_COPY_PIPELINE", "0")
	t.Setenv("CRIU_SYSBOX_COPY_PIPELINE_ABORT", "1")
	if err := criuCopyPipelineGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("abort proof accepted without pipeline")
	}
	t.Setenv("CRIU_SYSBOX_COPY_PIPELINE_ABORT", "0")
	if err := criuCopyPipelineGuard("/other", "unrelated"); err != nil {
		t.Fatalf("disabled pipeline changed control: %v", err)
	}
	t.Setenv("CRIU_SYSBOX_COPY_PIPELINE", "true")
	if err := criuCopyPipelineGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("invalid pipeline switch accepted")
	}
}
