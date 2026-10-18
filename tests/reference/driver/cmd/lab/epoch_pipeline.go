package main

import (
	"fmt"
	"os"
)

var criuEpochPipelineCancelled bool

// The first child remains the common setup. Enable the already tested
// pipeline only while preparing/capturing/restoring the actual second child.
// The helper must inherit its explicit original protocol authorization.
func criuEpochEnableSecondPipeline() (func() error, error) {
	if os.Getenv("RUNTIME_FORK_EPOCH_PIPELINE") != "1" ||
		os.Getenv("RUNTIME_FORK_EPOCH_BACKED_RESUME") == "1" ||
		criuRoot != "/experiment" || criuCopyPipelineEnabled() {
		return nil, fmt.Errorf("second-copy pipeline outside its exact frozen fork")
	}
	old, present := os.LookupEnv("CRIU_SYSBOX_COPY_PIPELINE")
	if old != "" && old != "0" {
		return nil, fmt.Errorf("first-copy pipeline was already enabled")
	}
	if err := os.Setenv("CRIU_SYSBOX_COPY_PIPELINE", "1"); err != nil {
		return nil, err
	}
	return func() error {
		if present {
			return os.Setenv("CRIU_SYSBOX_COPY_PIPELINE", old)
		}
		return os.Unsetenv("CRIU_SYSBOX_COPY_PIPELINE")
	}, nil
}
