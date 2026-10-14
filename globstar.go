// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// globStar expands pattern like filepath.Glob, but a path segment that is
// exactly "**" matches zero or more directory levels (bash globstar
// semantics). Like bash, "**" does not descend into hidden directories
// (names starting with ".") and does not follow symlinked directories.
// Patterns without a "**" segment are delegated to filepath.Glob unchanged.
func globStar(pattern string) ([]string, error) {
	if !hasGlobStar(pattern) {
		return filepath.Glob(pattern)
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	segs := strings.Split(filepath.ToSlash(pattern), "/")
	root := ""
	if strings.HasPrefix(pattern, "/") {
		root = "/"
		segs = segs[1:]
	}
	globSegs(root, segs, add)
	sort.Strings(out)
	return out, nil
}

func hasGlobStar(pattern string) bool {
	for _, s := range strings.Split(filepath.ToSlash(pattern), "/") {
		if s == "**" {
			return true
		}
	}
	return false
}

func joinGlob(dir, name string) string {
	switch dir {
	case "":
		return name
	case "/":
		return "/" + name
	}
	return dir + "/" + name
}

func readGlobDir(dir string) []os.DirEntry {
	if dir == "" {
		dir = "."
	}
	entries, _ := os.ReadDir(filepath.FromSlash(dir)) // like filepath.Glob, ignore I/O errors
	return entries
}

func globSegs(dir string, segs []string, add func(string)) {
	if len(segs) == 0 {
		if dir != "" {
			add(filepath.FromSlash(dir))
		}
		return
	}
	seg, rest := segs[0], segs[1:]
	if seg == "" { // tolerate "a//b"
		globSegs(dir, rest, add)
		return
	}
	if seg == "**" {
		for len(rest) > 0 && rest[0] == "**" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			// Trailing "**": everything below dir.
			walkStar(dir, func(p string, _ bool) { add(filepath.FromSlash(p)) })
			return
		}
		globSegs(dir, rest, add)                   // zero levels
		walkStar(dir, func(p string, isDir bool) { // one or more levels
			if isDir {
				globSegs(p, rest, add)
			}
		})
		return
	}
	if !strings.ContainsAny(seg, `*?[\`) {
		p := joinGlob(dir, seg)
		if _, err := os.Lstat(filepath.FromSlash(p)); err == nil {
			globSegs(p, rest, add)
		}
		return
	}
	for _, e := range readGlobDir(dir) {
		if ok, _ := filepath.Match(seg, e.Name()); !ok {
			continue
		}
		p := joinGlob(dir, e.Name())
		if len(rest) > 0 {
			if fi, err := os.Stat(filepath.FromSlash(p)); err != nil || !fi.IsDir() {
				continue
			}
		}
		globSegs(p, rest, add)
	}
}

// walkStar visits every entry strictly below dir, skipping hidden entries
// and not descending into symlinked directories.
func walkStar(dir string, visit func(p string, isDir bool)) {
	for _, e := range readGlobDir(dir) {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := joinGlob(dir, e.Name())
		isDir := e.IsDir() // symlinks report as non-dir: not followed
		visit(p, isDir)
		if isDir {
			walkStar(p, visit)
		}
	}
}

// matchStar reports whether path matches pattern, where a "**" segment
// matches zero or more path segments and other segments use filepath.Match.
func matchStar(pattern, path string) bool {
	return matchSegs(strings.Split(filepath.ToSlash(pattern), "/"), strings.Split(filepath.ToSlash(path), "/"))
}

func matchSegs(pat, p []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(p); i++ {
				if matchSegs(pat[1:], p[i:]) {
					return true
				}
			}
			return false
		}
		if len(p) == 0 {
			return false
		}
		if ok, _ := filepath.Match(pat[0], p[0]); !ok {
			return false
		}
		pat, p = pat[1:], p[1:]
	}
	return len(p) == 0
}
