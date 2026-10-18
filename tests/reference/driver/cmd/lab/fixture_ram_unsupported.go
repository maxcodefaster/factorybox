//go:build !linux

package main

import "fmt"

type fixtureRAMToken struct{}

func newFixtureRAMToken() (*fixtureRAMToken, error) {
	return nil, fmt.Errorf("native RAM fixture requires Linux")
}
func (*fixtureRAMToken) set(string) error { return fmt.Errorf("native RAM fixture requires Linux") }
func (*fixtureRAMToken) get() string      { return "" }
func (*fixtureRAMToken) close()           {}
