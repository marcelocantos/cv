// Copyright 2026 The mk Authors
// SPDX-License-Identifier: Apache-2.0

package mk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseDepfile parses a depfile in the given format and returns the list of
// prerequisite paths it declares.
//
// Supported formats:
//
//   - "gcc", "makefile": Makefile-style depfile as emitted by gcc/clang
//     -MMD/-MD/-M. The file looks like:
//
//       target1 target2: prereq1 prereq2 \
//         prereq3 prereq4
//
//     Multiple target groups (gcc -MP) are tolerated; their phony empty rules
//     contribute nothing. Backslash-newline continues a line. A backslash
//     before a space escapes the space (paths with spaces). Other backslash
//     escapes are passed through.
//
// The returned paths are cleaned with filepath.Clean but otherwise preserved
// as the depfile emitted them (relative or absolute as-is).
func ParseDepfile(path, format string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch format {
	case "gcc", "makefile", "":
		return parseMakefileDepfile(string(data))
	default:
		return nil, fmt.Errorf("unknown depfile format %q", format)
	}
}

// parseMakefileDepfile parses a Makefile-syntax depfile. The targets are
// ignored; only the prerequisite list (across all rules in the file) is
// returned, deduplicated in first-seen order.
func parseMakefileDepfile(src string) ([]string, error) {
	// Stitch line continuations first: "\\\n" → " ".
	src = stitchContinuations(src)

	var prereqs []string
	seen := map[string]bool{}

	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Find the rule-separating colon. The first colon at depth 0 that
		// isn't escaped wins. Depfiles never quote with brackets, so a plain
		// scan is sufficient — but we still need to skip "C:" on Windows
		// paths in the target list. We do that by requiring the colon to be
		// followed by whitespace, end-of-line, or another path char that
		// could not appear in a drive letter (rare in practice for depfiles
		// but free to handle).
		colon := findDepfileColon(line)
		if colon < 0 {
			continue
		}
		rhs := strings.TrimSpace(line[colon+1:])
		if rhs == "" {
			// gcc -MP emits "header.h:" lines with no prereqs — skip.
			continue
		}
		for _, p := range splitDepfileWords(rhs) {
			p = filepath.Clean(p)
			if seen[p] {
				continue
			}
			seen[p] = true
			prereqs = append(prereqs, p)
		}
	}
	return prereqs, nil
}

// stitchContinuations replaces "\\\n" (and "\\\r\n") with " ", joining
// continuation lines into one logical line.
func stitchContinuations(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] == '\n' {
				b.WriteByte(' ')
				i++ // skip the newline
				continue
			}
			if s[i+1] == '\r' && i+2 < len(s) && s[i+2] == '\n' {
				b.WriteByte(' ')
				i += 2 // skip CRLF
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// findDepfileColon returns the index of the rule-separating colon, or -1.
// It skips colons that are part of a Windows drive letter (e.g., "C:/").
func findDepfileColon(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ':' {
			continue
		}
		// Skip drive-letter colon: a single ASCII letter immediately to the
		// left at the start of a path token, followed by a path separator.
		if i == 1 && isASCIILetter(line[0]) && i+1 < len(line) && (line[i+1] == '/' || line[i+1] == '\\') {
			continue
		}
		if i >= 2 && isASCIILetter(line[i-1]) && (line[i-2] == ' ' || line[i-2] == '\t') && i+1 < len(line) && (line[i+1] == '/' || line[i+1] == '\\') {
			continue
		}
		return i
	}
	return -1
}

func isASCIILetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// splitDepfileWords splits a depfile RHS into path words, honouring "\ " as
// an escaped space within a path.
func splitDepfileWords(s string) []string {
	var words []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			if i+1 < len(s) && (s[i+1] == ' ' || s[i+1] == '\t') {
				cur.WriteByte(s[i+1])
				i++
				continue
			}
			cur.WriteByte(c)
		case ' ', '\t':
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}
