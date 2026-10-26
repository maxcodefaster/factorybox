// Copyright 2026 Factorybox contributors.
// SPDX-License-Identifier: Apache-2.0

package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var text string

// String identifies the source version embedded in this executable.
func String() string { return strings.TrimSpace(text) }
