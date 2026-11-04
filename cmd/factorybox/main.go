package main

import (
	"encoding/json"
	"fmt"
	"os"

	"factorybox/internal/platform"
	"factorybox/internal/version"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "version" && os.Args[1] != "doctor") {
		if err := runLifecycle(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if os.Args[1] == "version" {
		fmt.Println("factorybox v" + version.String())
		return
	}
	report, err := platform.Inspect()
	if err == nil {
		err = report.Err()
	}
	output := struct {
		Platform platform.Info `json:"platform"`
		// MatchesReference keeps its historical name. It means that the
		// platform passed the capability gate, not that it equals a pinned kernel.
		MatchesReference    bool                  `json:"matches_reference"`
		Qualification       string                `json:"qualification"`
		MinimumKernel       string                `json:"minimum_kernel"`
		BootID              string                `json:"boot_id,omitempty"`
		KernelConfig        string                `json:"kernel_config"`
		Capabilities        []platform.Capability `json:"capabilities"`
		ProductionQualified bool                  `json:"production_qualified"`
		Scope               string                `json:"scope"`
		Error               string                `json:"error,omitempty"`
	}{
		Platform: report.Platform, MatchesReference: err == nil, Qualification: report.Qualification,
		MinimumKernel: report.MinimumKernel, BootID: report.BootID, KernelConfig: report.KernelConfig,
		Capabilities: report.Capabilities,
		Scope: "Kernel capability inspection. An unverified qualification means the project has not run its native suites on this exact kernel; " +
			"it does not refuse execution. Component identities, filesystem, ownership, " +
			"resource boundaries and lifecycle qualification require separate evidence.",
	}
	if err != nil {
		output.Error = err.Error()
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(output); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(1)
	}
	if err != nil {
		os.Exit(1)
	}
}
