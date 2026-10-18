package main

import "os"

func descendantInheritProofEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_INHERIT_PROOF") == "1"
}

func descendantInheritedTargetEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_INHERITED_TARGET") == "1"
}

func descendantDirectRangeEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_DIRECT_RANGE") == "1"
}

func descendantPageInputLeaseOnlyEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_PAGE_INPUT_LEASE_ONLY") == "1"
}

func descendantStreamOriginalEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_STREAM_ORACLE") == "1"
}

func descendantSingleCaptureEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_SINGLE_CAPTURE") == "1"
}

func descendantFastSourceEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_FAST_SOURCE") == "1"
}

func descendantCompleteRAMEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_COMPLETE_RAM") == "1"
}

func descendantTargetCertificateEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_TARGET_CERTIFICATE") == "1"
}

func descendantSourceCertificateEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_SOURCE_CERTIFICATE") == "1"
}

func descendantSourceSyncEnabled() bool {
	return os.Getenv("RUNTIME_FORK_DESCENDANT_SOURCE_SYNC") == "1"
}
