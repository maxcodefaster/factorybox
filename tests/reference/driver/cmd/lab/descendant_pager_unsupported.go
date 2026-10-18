//go:build !linux

package main

import "fmt"

type descendantPagerBinding struct{}

func descendantPreparePager(_ *criuLazyDaemon, ownerID, _ string) error {
	if descendantInheritProofEnabled() && ownerID != "" {
		return fmt.Errorf("descendant pager inheritance requires the tested Linux worker")
	}
	return nil
}

func descendantBindRestoredPager(_ *criuLazyDaemon) error { return nil }

func descendantPagerEndpointOwned(_ *criuLazyDaemon, _ string) bool { return false }
