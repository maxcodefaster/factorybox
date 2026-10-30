//go:build linux

package worker

import (
	"testing"

	"factorybox/internal/storefs"
)

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

func TestAutomaticBoundedCapacityIsSelectedBeforeAdmissionAndNeverReinterpreted(t *testing.T) {
	config := Config{BoundedRoot: "/private/fixed", BoundedBytes: 32 << 30, MinimumBoundedBytes: 8 << 30}
	if err := boundedConfig(&config); err != nil {
		t.Fatal("valid automatic bounds refused", err)
	}
	identity := boundedIdentity{Image: storefs.Image{Bytes: 17 << 30}, Capacity: boundedRange(config)}
	if !retainedBoundedCapacity(config, identity) {
		t.Fatal("a retained selection within its original bounds was refused")
	}
	for _, changed := range []Config{
		{BoundedBytes: 32 << 30},
		{BoundedBytes: 32 << 30, MinimumBoundedBytes: 9 << 30},
		{BoundedBytes: 24 << 30, MinimumBoundedBytes: 8 << 30},
		{BoundedBytes: 17 << 30},
	} {
		if retainedBoundedCapacity(changed, identity) {
			t.Fatal("retained storage accepted changed admission bounds", changed)
		}
	}
	for _, capacity := range []int64{0, 7 << 30, 17<<30 + 1, 33 << 30, -1} {
		changed := identity
		changed.Image.Bytes = capacity
		if retainedBoundedCapacity(config, changed) {
			t.Fatal("retained storage accepted an unadmitted capacity", capacity)
		}
	}
	fixed := boundedIdentity{Image: storefs.Image{Bytes: 32 << 30}}
	if retainedBoundedCapacity(config, fixed) {
		t.Fatal("automatic bounds reinterpreted an existing fixed image")
	}
	if !retainedBoundedCapacity(Config{BoundedBytes: 32 << 30}, fixed) {
		t.Fatal("unchanged original fixed admission was refused")
	}
	for _, invalid := range []Config{
		{MinimumBoundedBytes: 8 << 30},
		{BoundedRoot: "/private/fixed", BoundedBytes: 8 << 30, MinimumBoundedBytes: 9 << 30},
		{BoundedRoot: "/private/fixed", BoundedBytes: 32 << 30, MinimumBoundedBytes: -1},
		{BoundedRoot: "/private/fixed", BoundedBytes: 32 << 30, MinimumBoundedBytes: 8<<30 + 1},
		{BoundedRoot: "/private/fixed", BoundedBytes: 32<<30 + 4096, MinimumBoundedBytes: 8 << 30},
	} {
		if err := boundedConfig(&invalid); err == nil {
			t.Fatal("invalid automatic bounds admitted", invalid)
		}
	}
}
