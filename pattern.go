// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Pattern represents a target or prerequisite pattern with named captures.
// e.g. "build/{config}/{name}.o" has captures ["config", "name"]
// and parts ["build/", "/", ".o"].
type Pattern struct {
	Parts       []string             // literal parts between captures
	Captures    []string             // capture names
	Constraints []*CaptureConstraint // parallel to Captures; nil entry = unconstrained
	Raw         string               // original pattern string
}

// CaptureConstraint restricts what a named capture can match.
type CaptureConstraint struct {
	Glob  string         // comma-separated alternatives, matched with filepath.Match
	Regex *regexp.Regexp // compiled regex, anchored with ^...$
}

// Matches returns true if the candidate string satisfies the constraint.
func (c *CaptureConstraint) Matches(s string) bool {
	if c.Regex != nil {
		return c.Regex.MatchString(s)
	}
	for _, alt := range strings.Split(c.Glob, ",") {
		if matched, _ := filepath.Match(alt, s); matched {
			return true
		}
	}
	return false
}

// ParsePattern parses a pattern string into a Pattern.
// Patterns use {name} for named captures, {name:glob} for glob-constrained
// captures, and {name/regex} for regex-constrained captures.
func ParsePattern(s string) (Pattern, bool, error) {
	var parts []string
	var captures []string
	var constraints []*CaptureConstraint

	rest := s
	var current string
	hasCapture := false

	for len(rest) > 0 {
		idx := strings.IndexByte(rest, '{')
		if idx < 0 {
			current += rest
			break
		}

		// Classify capture content by scanning for :, /, or }
		inner := rest[idx+1:]
		name, constraint, end, err := parseCapture(inner)
		if err != nil {
			return Pattern{}, false, fmt.Errorf("pattern %q: %w", s, err)
		}
		if end < 0 {
			// No closing } found
			current += rest
			break
		}

		hasCapture = true
		current += rest[:idx]
		parts = append(parts, current)
		current = ""
		captures = append(captures, name)
		constraints = append(constraints, constraint)
		rest = inner[end+1:] // skip past the closing }
	}
	parts = append(parts, current)

	if !hasCapture {
		return Pattern{Raw: s}, false, nil
	}

	return Pattern{
		Parts:       parts,
		Captures:    captures,
		Constraints: constraints,
		Raw:         s,
	}, true, nil
}

// parseCapture parses the content after '{' and returns the capture name,
// an optional constraint, and the index of the closing '}' within inner.
// Returns end=-1 if no closing } is found.
func parseCapture(inner string) (name string, constraint *CaptureConstraint, end int, err error) {
	// Scan for the first ':', '/', or '}' to classify
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '}':
			// Simple unconstrained capture: {name}
			return inner[:i], nil, i, nil

		case ':':
			// Glob capture: {name:glob}
			closeBrace := strings.IndexByte(inner[i+1:], '}')
			if closeBrace < 0 {
				return "", nil, -1, nil
			}
			closeBrace += i + 1
			glob := inner[i+1 : closeBrace]
			return inner[:i], &CaptureConstraint{Glob: glob}, closeBrace, nil

		case '/':
			// Regex capture: {name/regex}
			// Walk regex syntax to find the real closing }
			reStart := i + 1
			reEnd := findRegexEnd(inner, reStart)
			if reEnd < 0 {
				return "", nil, -1, nil
			}
			reStr := inner[reStart:reEnd]
			compiled, err := regexp.Compile("^(?:" + reStr + ")$")
			if err != nil {
				return "", nil, -1, fmt.Errorf("invalid regex in capture %q: %w", inner[:i], err)
			}
			return inner[:i], &CaptureConstraint{Regex: compiled}, reEnd, nil
		}
	}
	return "", nil, -1, nil
}

// findRegexEnd walks regex syntax starting at pos within s, tracking
// escapes (\x), character classes ([...]), and quantifiers ({n,m}) to
// find the } that closes the capture (not one that's part of the regex).
// Returns the index of that }, or -1 if not found.
func findRegexEnd(s string, pos int) int {
	inCharClass := false
	escaped := false
	for i := pos; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		c := s[i]
		switch {
		case c == '\\':
			escaped = true
		case inCharClass:
			if c == ']' {
				inCharClass = false
			}
		case c == '[':
			inCharClass = true
		case c == '{':
			// Regex quantifier like {2,4} — find matching }
			j := strings.IndexByte(s[i+1:], '}')
			if j >= 0 {
				i += j + 1 // skip past the quantifier's }
			}
		case c == '}':
			return i
		}
	}
	return -1
}

// Match attempts to match a concrete string against this pattern.
// Returns the captured values and true if it matches, nil and false otherwise.
func (p Pattern) Match(s string) (map[string]string, bool) {
	if len(p.Captures) == 0 {
		return nil, s == p.Raw
	}

	captures := make(map[string]string)
	return p.match(s, 0, captures)
}

func (p Pattern) match(s string, idx int, captures map[string]string) (map[string]string, bool) {
	// Must start with Parts[idx]
	prefix := p.Parts[idx]
	if !strings.HasPrefix(s, prefix) {
		return nil, false
	}
	s = s[len(prefix):]

	// If this is the last part, s must be empty
	if idx >= len(p.Captures) {
		if s == "" {
			return captures, true
		}
		return nil, false
	}

	// Try to match the capture: find the next literal part
	suffix := p.Parts[idx+1]
	captureName := p.Captures[idx]

	if idx+1 >= len(p.Parts)-1 && suffix == "" && idx+1 >= len(p.Captures) {
		// Last capture, no suffix after it — capture the rest
		if strings.Contains(s, "/") {
			return nil, false
		}
		if !p.constraintMatches(idx, s) {
			return nil, false
		}
		if existing, ok := captures[captureName]; ok {
			if existing != s {
				return nil, false
			}
		} else {
			captures[captureName] = s
		}
		return captures, true
	}

	// Try each possible split point
	for i := 0; i <= len(s); i++ {
		candidate := s[:i]
		// Don't allow captures to contain /
		if strings.Contains(candidate, "/") {
			continue
		}

		// Check constraint
		if !p.constraintMatches(idx, candidate) {
			continue
		}

		if existing, ok := captures[captureName]; ok {
			if existing != candidate {
				continue
			}
		}

		capturesCopy := make(map[string]string)
		for k, v := range captures {
			capturesCopy[k] = v
		}
		capturesCopy[captureName] = candidate

		if result, ok := p.match(s[i:], idx+1, capturesCopy); ok {
			return result, true
		}
	}

	return nil, false
}

// constraintMatches checks if the candidate satisfies the constraint for
// capture at the given index. Returns true if unconstrained.
func (p Pattern) constraintMatches(idx int, candidate string) bool {
	if idx >= len(p.Constraints) || p.Constraints[idx] == nil {
		return true
	}
	return p.Constraints[idx].Matches(candidate)
}

// Expand substitutes capture values into a pattern to produce a concrete string.
func (p Pattern) Expand(captures map[string]string) string {
	if len(p.Captures) == 0 {
		return p.Raw
	}

	var b strings.Builder
	for i, part := range p.Parts {
		b.WriteString(part)
		if i < len(p.Captures) {
			b.WriteString(captures[p.Captures[i]])
		}
	}
	return b.String()
}

// IsPattern returns true if this has any captures.
func (p Pattern) IsPattern() bool {
	return len(p.Captures) > 0
}
