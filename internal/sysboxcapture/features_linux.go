//go:build linux

package sysboxcapture

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"time"
)

type Features struct {
	Schema                  int  `json:"schema"`
	RootOwnedTimeNamespaces bool `json:"root_owned_time_namespaces"`
	SharedFileDescriptions  bool `json:"shared_file_descriptions,omitempty"`
	SharedSourceDirectories bool `json:"shared_source_directories,omitempty"`
}

type featureOutput struct {
	bytes.Buffer
	overflow bool
}

func (output *featureOutput) Write(data []byte) (int, error) {
	size := len(data)
	remaining := 4096 - output.Len()
	if size > remaining {
		output.overflow = true
		data = data[:remaining]
	}
	output.Buffer.Write(data)
	return size, nil
}

// The caller supplies an already hash-verified executable descriptor. Older
// CRIU builds reject this read-only command; unknown/invalid output admits no
// additional kernel resources.
func InspectFeatures(ctx context.Context, binary *os.File) Features {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, "/proc/self/fd/3", "factorybox-features")
	command.ExtraFiles = []*os.File{binary}
	var output featureOutput
	command.Stdout, command.Stderr = &output, io.Discard
	if command.Run() != nil || output.overflow {
		return Features{}
	}
	var features Features
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&features) != nil || decoder.Decode(new(any)) != io.EOF || features.Schema != 1 {
		return Features{}
	}
	return features
}
