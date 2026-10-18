package main

import (
	"encoding/json"
	"fmt"
	"os"

	"factorybox/internal/platform"
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
		fmt.Println("factorybox development; runtime extraction in progress")
		return
	}
	info, err := platform.Current()
	if err == nil {
		err = info.CheckReference()
	}
	report := struct {
		Platform            platform.Info `json:"platform"`
		MatchesReference    bool          `json:"matches_reference"`
		ProductionQualified bool          `json:"production_qualified"`
		Scope               string        `json:"scope"`
		Error               string        `json:"error,omitempty"`
	}{
		Platform: info, MatchesReference: err == nil,
		Scope: "Platform inspection only. Runtime extraction and native lifecycle qualification " +
			"are incomplete. Filesystem, ownership, lease and userfaultfd checks are separately required.",
	}
	if err != nil {
		report.Error = err.Error()
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(report); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(1)
	}
	if err != nil {
		os.Exit(1)
	}
}
