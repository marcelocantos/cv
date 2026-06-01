// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package cv

import (
	"fmt"
	"io"
	"runtime"
)

// traceSupported reports whether the platform can observe a recipe's
// file accesses. Currently only Linux is supported (via strace); other
// platforms return a clear "not yet implemented" message.
func traceSupported() (bool, string) {
	return false, fmt.Sprintf("[deps: trace] / [writes: trace] are not yet implemented on %s — use [deps: gcc] (depfile adapter) or [writes: manifest …] (producer-emitted manifest) instead", runtime.GOOS)
}

func runTraced(script string, stdout, stderr io.Writer, env []string) (reads, writes []string, err error) {
	return nil, nil, fmt.Errorf("trace mode not supported on %s", runtime.GOOS)
}
