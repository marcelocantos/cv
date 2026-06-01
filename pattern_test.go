// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"testing"
)

func TestParsePattern(t *testing.T) {
	tests := []struct {
		input     string
		isPattern bool
		captures  []string
	}{
		{"foo.o", false, nil},
		{"build/{name}.o", true, []string{"name"}},
		{"build/{config}/{name}.o", true, []string{"config", "name"}},
		{"{a}/{b}/{c}", true, []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		p, ok, err := ParsePattern(tt.input)
		if err != nil {
			t.Errorf("ParsePattern(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if ok != tt.isPattern {
			t.Errorf("ParsePattern(%q): isPattern = %v, want %v", tt.input, ok, tt.isPattern)
			continue
		}
		if ok {
			if len(p.Captures) != len(tt.captures) {
				t.Errorf("ParsePattern(%q): captures = %v, want %v", tt.input, p.Captures, tt.captures)
			}
			for i := range p.Captures {
				if i < len(tt.captures) && p.Captures[i] != tt.captures[i] {
					t.Errorf("ParsePattern(%q): capture[%d] = %q, want %q", tt.input, i, p.Captures[i], tt.captures[i])
				}
			}
		}
	}
}

func TestPatternMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		input    string
		match    bool
		captures map[string]string
	}{
		{"foo.o", "foo.o", true, nil},
		{"foo.o", "bar.o", false, nil},
		{"build/{name}.o", "build/foo.o", true, map[string]string{"name": "foo"}},
		{"build/{name}.o", "build/bar.o", true, map[string]string{"name": "bar"}},
		{"build/{name}.o", "build/.o", true, map[string]string{"name": ""}},
		{"build/{name}.o", "src/foo.o", false, nil},
		{"build/{config}/{name}.o", "build/debug/foo.o", true, map[string]string{"config": "debug", "name": "foo"}},
		{"build/{config}/{name}.o", "build/release/bar.o", true, map[string]string{"config": "release", "name": "bar"}},
		// Captures must not contain /
		{"build/{name}.o", "build/a/b.o", false, nil},
	}

	for _, tt := range tests {
		p, _, _ := ParsePattern(tt.pattern)
		caps, ok := p.Match(tt.input)
		if ok != tt.match {
			t.Errorf("Pattern(%q).Match(%q): match = %v, want %v", tt.pattern, tt.input, ok, tt.match)
			continue
		}
		if ok && tt.captures != nil {
			for k, v := range tt.captures {
				if caps[k] != v {
					t.Errorf("Pattern(%q).Match(%q): capture[%q] = %q, want %q", tt.pattern, tt.input, k, caps[k], v)
				}
			}
		}
	}
}

func TestPatternExpand(t *testing.T) {
	tests := []struct {
		pattern  string
		captures map[string]string
		want     string
	}{
		{"build/{name}.o", map[string]string{"name": "foo"}, "build/foo.o"},
		{"build/{config}/{name}.o", map[string]string{"config": "debug", "name": "bar"}, "build/debug/bar.o"},
	}

	for _, tt := range tests {
		p, _, _ := ParsePattern(tt.pattern)
		got := p.Expand(tt.captures)
		if got != tt.want {
			t.Errorf("Pattern(%q).Expand(%v) = %q, want %q", tt.pattern, tt.captures, got, tt.want)
		}
	}
}

func TestSameNamedCaptures(t *testing.T) {
	// Same capture name on both sides of a rule means values must match
	target, _, _ := ParsePattern("build/{name}.o")
	prereq, _, _ := ParsePattern("src/{name}.c")

	caps, ok := target.Match("build/foo.o")
	if !ok {
		t.Fatal("target should match")
	}

	expanded := prereq.Expand(caps)
	if expanded != "src/foo.c" {
		t.Errorf("prereq.Expand = %q, want %q", expanded, "src/foo.c")
	}
}

func TestParsePatternGlob(t *testing.T) {
	p, ok, err := ParsePattern("build/{name:test_*}.o")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected pattern")
	}
	if p.Captures[0] != "name" {
		t.Errorf("capture name = %q, want %q", p.Captures[0], "name")
	}
	if p.Constraints[0] == nil || p.Constraints[0].Glob != "test_*" {
		t.Errorf("expected glob constraint 'test_*'")
	}
}

func TestParsePatternRegex(t *testing.T) {
	p, ok, err := ParsePattern("build/{name/[a-z]+}.o")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected pattern")
	}
	if p.Captures[0] != "name" {
		t.Errorf("capture name = %q, want %q", p.Captures[0], "name")
	}
	if p.Constraints[0] == nil || p.Constraints[0].Regex == nil {
		t.Fatal("expected regex constraint")
	}
}

func TestParsePatternRegexWithBraces(t *testing.T) {
	// Regex quantifiers {n,m} must not confuse the parser
	p, ok, err := ParsePattern("v{ver/\\d{1,3}}.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected pattern")
	}
	if p.Captures[0] != "ver" {
		t.Errorf("capture name = %q, want %q", p.Captures[0], "ver")
	}
	if p.Constraints[0] == nil || p.Constraints[0].Regex == nil {
		t.Fatal("expected regex constraint")
	}
	// Should match version-like strings
	caps, ok := p.Match("v42.txt")
	if !ok || caps["ver"] != "42" {
		t.Errorf("Match(v42.txt) = %v, %v; want ver=42", caps, ok)
	}
	caps, ok = p.Match("v123.txt")
	if !ok || caps["ver"] != "123" {
		t.Errorf("Match(v123.txt) = %v, %v; want ver=123", caps, ok)
	}
	// 4 digits should not match \d{1,3}
	_, ok = p.Match("v1234.txt")
	if ok {
		t.Error("Match(v1234.txt) should fail (too many digits)")
	}
}

func TestGlobConstraintMatch(t *testing.T) {
	p, _, _ := ParsePattern("build/{name:test_*}.o")

	caps, ok := p.Match("build/test_foo.o")
	if !ok || caps["name"] != "test_foo" {
		t.Errorf("expected match for test_foo, got %v, %v", caps, ok)
	}

	_, ok = p.Match("build/bench_foo.o")
	if ok {
		t.Error("expected no match for bench_foo")
	}
}

func TestGlobAlternation(t *testing.T) {
	p, _, _ := ParsePattern("src/{name}.{ext:c,cc,cpp}")

	tests := []struct {
		input string
		match bool
		ext   string
	}{
		{"src/foo.c", true, "c"},
		{"src/foo.cc", true, "cc"},
		{"src/foo.cpp", true, "cpp"},
		{"src/foo.h", false, ""},
		{"src/foo.go", false, ""},
	}

	for _, tt := range tests {
		caps, ok := p.Match(tt.input)
		if ok != tt.match {
			t.Errorf("Match(%q) = %v, want %v", tt.input, ok, tt.match)
		}
		if ok && caps["ext"] != tt.ext {
			t.Errorf("Match(%q) ext = %q, want %q", tt.input, caps["ext"], tt.ext)
		}
	}
}

func TestRegexConstraintMatch(t *testing.T) {
	p, _, _ := ParsePattern("build/{name/[a-z]+}.o")

	caps, ok := p.Match("build/foo.o")
	if !ok || caps["name"] != "foo" {
		t.Errorf("expected match for foo, got %v, %v", caps, ok)
	}

	_, ok = p.Match("build/Foo.o")
	if ok {
		t.Error("expected no match for Foo (uppercase)")
	}

	_, ok = p.Match("build/foo123.o")
	if ok {
		t.Error("expected no match for foo123 (contains digits)")
	}
}

func TestConstraintExpand(t *testing.T) {
	// Expand should work normally with constrained patterns
	p, _, _ := ParsePattern("build/{name:test_*}.o")
	got := p.Expand(map[string]string{"name": "test_foo"})
	if got != "build/test_foo.o" {
		t.Errorf("Expand = %q, want %q", got, "build/test_foo.o")
	}
}

func TestParsePatternRegexError(t *testing.T) {
	// *+ is invalid regex (nothing to repeat)
	_, _, err := ParsePattern("build/{name/*+}.o")
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}
