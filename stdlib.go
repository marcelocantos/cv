// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import "embed"

//go:embed std/*.cv
var stdlibFS embed.FS

//go:embed agents-guide.md
var AgentsGuide string
