//go:build linux

package sysboxcapture

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"factorybox/internal/sandboxnetwork"
)

// IRMapScanPathsAnnotation supplies namespace-local fallback search roots for
// watches whose filesystem handles cannot be reopened. The caller chooses the
// application data paths; the runtime does not depend on an IDE or agent.
const IRMapScanPathsAnnotation = "io.factorybox.checkpoint.irmap-scan-paths"

// CheckpointConfiguration is shared by admission and the private capture view.
// Its input is a JSON array in one OCI annotation, never arbitrary CRIU options.
func CheckpointConfiguration(annotations map[string]string) ([]byte, error) {
	configuration := "nested-ns\nsysbox-oom-floor\n"
	requested, err := sandboxnetwork.Requested(annotations)
	if err != nil {
		return nil, err
	}
	if requested {
		network, err := sandboxnetwork.Plan(annotations["io.factorybox.owner"])
		if err != nil {
			return nil, err
		}
		extra, err := network.CRIUConfiguration()
		if err != nil {
			return nil, err
		}
		configuration += extra
	}
	encoded, present := annotations[IRMapScanPathsAnnotation]
	if !present {
		return []byte(configuration), nil
	}
	var paths []string
	if len(encoded) > 16<<10 || !utf8.ValidString(encoded) || json.Unmarshal([]byte(encoded), &paths) != nil || paths == nil || len(paths) > 32 {
		return nil, fmt.Errorf("%s requires a JSON array of at most 32 search roots within 16 KiB", IRMapScanPathsAnnotation)
	}
	seen := map[string]bool{}
	for _, root := range paths {
		if root == "/" || !path.IsAbs(root) || path.Clean(root) != root || len(root) >= 4096 ||
			strings.ContainsAny(root, "\\\"'#") || strings.ContainsFunc(root, func(c rune) bool { return unicode.IsControl(c) || unicode.IsSpace(c) }) {
			return nil, fmt.Errorf("invalid checkpoint watch search root %q", root)
		}
		for _, pseudo := range []string{"/proc", "/sys", "/dev"} {
			if root == pseudo || strings.HasPrefix(root, pseudo+"/") {
				return nil, fmt.Errorf("checkpoint watch search root enters a pseudo filesystem: %q", root)
			}
		}
		if seen[root] {
			return nil, fmt.Errorf("duplicate checkpoint watch search root %q", root)
		}
		seen[root] = true
		configuration += "irmap-scan-path " + root + "\n"
	}
	return []byte(configuration), nil
}

func checkpointConfigurationFromLabels(labels []string) ([]byte, error) {
	annotations := map[string]string{}
	for _, label := range labels {
		key, value, found := strings.Cut(label, "=")
		if !found || (key != IRMapScanPathsAnnotation && key != sandboxnetwork.Annotation && key != "io.factorybox.owner") {
			continue
		}
		if _, present := annotations[key]; present {
			return nil, fmt.Errorf("duplicate checkpoint watch search annotation")
		}
		annotations[key] = value
	}
	return CheckpointConfiguration(annotations)
}
