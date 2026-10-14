// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGlobStarRecursive(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"main.go", "a/a.go", "a/b/b.go", "a/b/c/c.go", "cmd/x/main.go",
		"a/b/notes.txt", ".git/hooks/h.go", "a/.hidden/h.go",
	} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	cases := map[string][]string{
		"**/*.go":     {"a/a.go", "a/b/b.go", "a/b/c/c.go", "cmd/x/main.go", "main.go"},
		"a/**/*.go":   {"a/a.go", "a/b/b.go", "a/b/c/c.go"},
		"a/**/c/*.go": {"a/b/c/c.go"},
		"**/**/c.go":  {"a/b/c/c.go"},
		"*.go":        {"main.go"},
		"a/b/**":      {"a/b/b.go", "a/b/c", "a/b/c/c.go", "a/b/notes.txt"},
	}
	for pat, want := range cases {
		got, err := wildcardGlob(pat)
		if err != nil {
			t.Fatalf("%s: %v", pat, err)
		}
		for i := range want {
			want[i] = filepath.FromSlash(want[i])
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("wildcardGlob(%q) = %v, want %v", pat, got, want)
		}
	}
}

func TestMatchStar(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "a/b/c/x.go", true},
		{"src/**/*.h", "src/x.h", true},
		{"src/**/*.h", "src/a/b/x.h", true},
		{"src/**/*.h", "other/x.h", false},
		{"src/**", "src/a/b", true},
		{"a/*/c", "a/b/x/c", false},
	}
	for _, c := range cases {
		if got := matchStar(c.pat, c.path); got != c.want {
			t.Errorf("matchStar(%q, %q) = %v, want %v", c.pat, c.path, got, c.want)
		}
	}
}

// TestCvfileWildcardGlobStar checks the vellum shape end to end: a rule
// whose prereqs come from $[wildcard **/*.go] must see files two or more
// directories deep.
func TestCvfileWildcardGlobStar(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"main.go", "cmd/app/main.go", "internal/x/y/y.go"} {
		p := filepath.Join(dir, f)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	src := "gosrc = $[wildcard **/*.go]\n\napp: $gosrc\n    touch $target\n"
	f, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildGraph(f, NewVars(), &BuildState{Targets: map[string]*TargetState{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := g.Resolve("app")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cmd/app/main.go", "internal/x/y/y.go", "main.go"}
	if !reflect.DeepEqual(rule.prereqs, want) {
		t.Errorf("prereqs = %v, want %v", rule.prereqs, want)
	}
}
