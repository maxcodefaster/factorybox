//go:build linux

package worker

import "testing"

func TestBoundedStoreDoesNotReinterpretExistingStore(t *testing.T) {
	config := Config{Store: "/existing/store", BoundedRoot: "/private/fixed", BoundedBytes: 512 << 20}
	if err := boundedConfig(&config); err == nil {
		t.Fatal("bounded mode silently reinterpreted an existing store")
	}
	for _, invalid := range []Config{
		{BoundedRoot: "/private/fixed"}, {BoundedBytes: 512 << 20},
		{BoundedRoot: "/private/fixed", BoundedBytes: 512<<20 + 1},
		{BoundedRoot: "/private/fixed", BoundedBytes: 256 << 20},
		{BoundedRoot: "/private/../fixed", BoundedBytes: 512 << 20},
	} {
		if err := boundedConfig(&invalid); err == nil {
			t.Fatal("incomplete or unsafe store request admitted", invalid)
		}
	}
	config = Config{BoundedRoot: "/private/fixed", BoundedBytes: 512 << 20}
	if err := boundedConfig(&config); err != nil || config.Store != "/private/fixed/data/store" {
		t.Fatal("new bounded store did not resolve its own internal data path", config, err)
	}
	config = Config{}
	if err := boundedConfig(&config); err != nil || config.Store != "/var/lib/sysbox/store" {
		t.Fatal("legacy default changed", config, err)
	}
}
