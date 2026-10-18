//go:build linux

package sysboxcapture

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPrivateConfigViewPreservesSysboxState(t *testing.T) {
	original := []byte(`{
		"id":"fbx-example","init_process_pid":123,"cgroup_paths":{"":"/sys/fs/cgroup/fbx-example"},
		"config":{"rootfs":"/owned/rootfs","rootfs_uid_shift_type":2,"rootfs_cloned":false,
		  "labels":["bundle=/owned/bundle","io.factorybox.owner=example","org.criu.config=/old/conf"],
		  "unknown_vendor_field":{"retain":true}},
		"sysbox":{"manager":{"registration":"original"},"fs":{"virtual_values":{"panic":"17"}}}
	}`)
	shadow, err := stateWithConfig(original, "/owned/capture/criu.conf")
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if json.Unmarshal(original, &before) != nil || json.Unmarshal(shadow, &after) != nil {
		t.Fatal("state did not remain valid JSON")
	}
	config := after["config"].(map[string]any)
	if !reflect.DeepEqual(config["labels"], []any{
		"bundle=/owned/bundle", "io.factorybox.owner=example", "org.criu.config=/owned/capture/criu.conf",
	}) {
		t.Fatalf("private config labels differ: %v", config["labels"])
	}
	// Ignore only the intentional annotation replacement. A typed
	// serialization through modern runc would lose these Sysbox-only fields.
	before["config"].(map[string]any)["labels"] = nil
	after["config"].(map[string]any)["labels"] = nil
	if !reflect.DeepEqual(before, after) {
		t.Fatal("private state view discarded or changed source/vendor metadata")
	}
}
