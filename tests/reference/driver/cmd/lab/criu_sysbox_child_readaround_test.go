package main

import (
	"strings"
	"testing"
)

func TestChildDemandProofRejectsUnboundedOrResetProgress(t *testing.T) {
	header := "Adaptive demand limit=262144 budget=67108864\n"
	first := "Adaptive demand request=131072 extra=65536 used=65536 budget=67108864 expanded=1\n"
	later := "Adaptive demand request=262144 extra=196608 used=4259840 budget=67108864 expanded=23\n"
	proof, err := criuParseDemandProof(header + first + later)
	if err != nil || proof.ReportedExtraBytes != 4259840 || proof.ObservedMaxRequest != 262144 {
		t.Fatalf("valid bounded progress rejected: %+v %v", proof, err)
	}
	for _, bad := range []string{
		first,
		header,
		header + header + first,
		header + strings.Replace(first, "expanded=1", "expanded=2", 1),
		header + strings.Replace(first, "used=65536", "used=134217728", 1),
		header + strings.Replace(first, "request=131072", "request=524288", 1),
		header + first + first,
		header + strings.Replace(first, "extra=65536", "extra=65537", 1),
	} {
		if _, err := criuParseDemandProof(bad); err == nil {
			t.Fatalf("unsafe/unproved policy accepted: %q", bad)
		}
	}
}

func TestChildDemandRequiresOwnedDemandOnlyPipeline(t *testing.T) {
	t.Setenv("CRIU_SYSBOX_CHILD_READAROUND", "1")
	for _, name := range []string{"CRIU_SYSBOX_LAZY_INHERIT", "CRIU_SYSBOX_RETAIN_SOURCE",
		"CRIU_SYSBOX_LAZY", "CRIU_SYSBOX_PROBE", "CRIU_SYSBOX_COPY_PIPELINE"} {
		t.Setenv(name, "1")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_PREFETCH", "0")
	if err := criuChildDemandGuard("/experiment", "source-sysbox"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CRIU_SYSBOX_LAZY_PREFETCH", "1")
	if err := criuChildDemandGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("bounded policy accepted unbounded background mode")
	}
	t.Setenv("CRIU_SYSBOX_LAZY_PREFETCH", "0")
	t.Setenv("CRIU_SYSBOX_COPY_PIPELINE", "0")
	if err := criuChildDemandGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("policy accepted outside owned pipeline")
	}
	t.Setenv("CRIU_SYSBOX_CHILD_READAROUND", "0")
	if err := criuChildDemandGuard("/other", "unrelated"); err != nil {
		t.Fatalf("disabled policy changed control: %v", err)
	}
	t.Setenv("CRIU_SYSBOX_CHILD_READAROUND", "true")
	if err := criuChildDemandGuard("/experiment", "source-sysbox"); err == nil {
		t.Fatal("invalid child policy flag accepted")
	}
}
