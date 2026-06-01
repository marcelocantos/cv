// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func wildcardGlob(pattern string) ([]string, error) {
	// Support space-separated patterns
	patterns := strings.Fields(pattern)
	var all []string
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, err
		}
		all = append(all, matches...)
	}
	return all, nil
}

func runShellCapture(cmd string) (string, error) {
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
