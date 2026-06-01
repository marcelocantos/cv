// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package cv

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// traceSupported reports whether the platform can observe a recipe's
// file accesses. On Linux this is gated on `strace` being installed.
func traceSupported() (bool, string) {
	if _, err := exec.LookPath("strace"); err != nil {
		return false, "strace not found in PATH; install it to use [deps: trace] / [writes: trace] on Linux"
	}
	return true, ""
}

// runTraced runs the given shell script under strace and returns the
// list of paths the script read and wrote. The trace covers child
// processes (-f) and only file-related syscalls (-e trace=%file).
//
// The returned paths are normalized via filepath.Clean and deduplicated
// in first-seen order. Paths are recorded relative to cwd if possible,
// otherwise absolute as strace reports them.
func runTraced(script string, stdout, stderr io.Writer, env []string) (reads, writes []string, err error) {
	tmp, err := os.CreateTemp("", "cv-trace-*.log")
	if err != nil {
		return nil, nil, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	// strace -f follows forks; -e trace=%file selects file syscalls.
	// -y decodes file descriptors to paths where possible; we ignore it
	// and parse the path argument directly. -o writes to a file so
	// stdout/stderr remain untouched.
	args := []string{
		"-f",
		"-qq",
		"-e", "trace=openat,open,creat,unlinkat,unlink,renameat,renameat2,rename,mkdir,mkdirat",
		"-o", tmpPath,
		"--", "sh", "-c", "set -e\n" + script,
	}
	cmd := exec.Command("strace", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = env

	runErr := cmd.Run()

	data, readErr := os.ReadFile(tmpPath)
	if readErr != nil && runErr == nil {
		return nil, nil, fmt.Errorf("reading strace log: %w", readErr)
	}

	reads, writes = parseStraceLog(string(data))
	return reads, writes, runErr
}

// openatRE matches lines like:
//
//	[pid 12345] openat(AT_FDCWD, "/path/to/file", O_RDONLY) = 3
//	openat(AT_FDCWD, "path", O_WRONLY|O_CREAT|O_TRUNC, 0644) = 4
var (
	openatRE = regexp.MustCompile(`open(?:at)?\(([^)]*)"([^"]+)"(?:,\s*([A-Z_|0-9 ]+))?[^)]*\)\s*=\s*(-?\d+)`)
	creatRE  = regexp.MustCompile(`creat\("([^"]+)"`)
)

func parseStraceLog(log string) (reads, writes []string) {
	rSeen := map[string]bool{}
	wSeen := map[string]bool{}
	addRead := func(p string) {
		p = filepath.Clean(p)
		if rSeen[p] {
			return
		}
		rSeen[p] = true
		reads = append(reads, p)
	}
	addWrite := func(p string) {
		p = filepath.Clean(p)
		if wSeen[p] {
			return
		}
		wSeen[p] = true
		writes = append(writes, p)
	}

	for _, line := range strings.Split(log, "\n") {
		if line == "" || strings.Contains(line, "ENOENT") {
			// Skip failed opens; they didn't read anything observable.
			continue
		}
		if m := openatRE.FindStringSubmatch(line); m != nil {
			path := m[2]
			flags := m[3]
			result := m[4]
			if strings.HasPrefix(result, "-") {
				continue
			}
			if isWriteFlag(flags) {
				addWrite(path)
			} else {
				addRead(path)
			}
			continue
		}
		if m := creatRE.FindStringSubmatch(line); m != nil {
			addWrite(m[1])
			continue
		}
		// unlinkat, renameat, mkdirat — treat targets as writes
		if strings.Contains(line, "unlink") || strings.Contains(line, "rename") || strings.Contains(line, "mkdir") {
			if i := strings.Index(line, `"`); i >= 0 {
				rest := line[i+1:]
				if j := strings.Index(rest, `"`); j > 0 {
					addWrite(rest[:j])
				}
			}
		}
	}
	return reads, writes
}

func isWriteFlag(flags string) bool {
	return strings.Contains(flags, "O_WRONLY") || strings.Contains(flags, "O_RDWR") ||
		strings.Contains(flags, "O_CREAT") || strings.Contains(flags, "O_TRUNC") ||
		strings.Contains(flags, "O_APPEND")
}
