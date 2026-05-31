// Copyright 2026 The mk Authors
// SPDX-License-Identifier: Apache-2.0

package mk

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// --- annotation parsing -----------------------------------------------------

func TestParseDepsAnnotation(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantFormat string
		wantTarget string
		wantKeep   bool
	}{
		{
			name:       "gcc",
			input:      `build/foo.o [deps: gcc]: src/foo.c`,
			wantFormat: "gcc",
			wantTarget: "build/foo.o",
		},
		{
			name:       "makefile",
			input:      `build/foo.o [deps: makefile]: src/foo.c`,
			wantFormat: "makefile",
			wantTarget: "build/foo.o",
		},
		{
			name:       "with keep",
			input:      `build/foo.o [deps: gcc] [keep]: src/foo.c`,
			wantFormat: "gcc",
			wantTarget: "build/foo.o",
			wantKeep:   true,
		},
		{
			name:       "no annotation",
			input:      `build/foo.o: src/foo.c`,
			wantFormat: "",
			wantTarget: "build/foo.o",
		},
		{
			name:       "pattern",
			input:      `{name}.o [deps: gcc]: {name}.c`,
			wantFormat: "gcc",
			wantTarget: "{name}.o",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Parse(strings.NewReader(tt.input + "\n\trecipe\n"))
			if err != nil {
				t.Fatal(err)
			}
			r := f.Stmts[0].(Rule)
			if r.DepsFormat != tt.wantFormat {
				t.Errorf("DepsFormat = %q, want %q", r.DepsFormat, tt.wantFormat)
			}
			if r.Targets[0] != tt.wantTarget {
				t.Errorf("Targets[0] = %q, want %q", r.Targets[0], tt.wantTarget)
			}
			if r.Keep != tt.wantKeep {
				t.Errorf("Keep = %v, want %v", r.Keep, tt.wantKeep)
			}
		})
	}
}

func TestDepsFormatPropagatesToRule(t *testing.T) {
	mkfile := `
build/foo.o [deps: gcc]: src/foo.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(mkfile))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildGraph(f, NewVars(), &BuildState{Targets: map[string]*TargetState{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := g.Resolve("build/foo.o")
	if err != nil {
		t.Fatal(err)
	}
	if rule.depsFormat != "gcc" {
		t.Errorf("rule.depsFormat = %q, want %q", rule.depsFormat, "gcc")
	}
}

func TestDepsFormatPropagatesToPatternRule(t *testing.T) {
	mkfile := `
build/{name}.o [deps: gcc]: src/{name}.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(mkfile))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildGraph(f, NewVars(), &BuildState{Targets: map[string]*TargetState{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := g.Resolve("build/foo.o")
	if err != nil {
		t.Fatal(err)
	}
	if rule.depsFormat != "gcc" {
		t.Errorf("rule.depsFormat = %q, want %q", rule.depsFormat, "gcc")
	}
}

// --- depfile parser ---------------------------------------------------------

func TestParseDepfileSimple(t *testing.T) {
	src := `build/foo.o: src/foo.c include/foo.h`
	got, err := parseMakefileDepfile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/foo.c", "include/foo.h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDepfileLineContinuation(t *testing.T) {
	src := `build/foo.o: src/foo.c \
  include/foo.h \
  include/bar.h
`
	got, err := parseMakefileDepfile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/foo.c", "include/foo.h", "include/bar.h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDepfileEscapedSpace(t *testing.T) {
	src := `build/foo.o: src/foo.c include/has\ space.h`
	got, err := parseMakefileDepfile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/foo.c", "include/has space.h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDepfilePhonyRules(t *testing.T) {
	// gcc -MP emits empty phony rules per header. They must not appear as
	// prereqs themselves but shouldn't break parsing either.
	src := `build/foo.o: src/foo.c include/foo.h

include/foo.h:
`
	got, err := parseMakefileDepfile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/foo.c", "include/foo.h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDepfileDedup(t *testing.T) {
	// A path appearing twice should be returned once, in first-seen order.
	src := `build/foo.o: src/foo.c include/foo.h
build/bar.o: include/foo.h src/bar.c
`
	got, err := parseMakefileDepfile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/foo.c", "include/foo.h", "src/bar.c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDepfileMultipleTargets(t *testing.T) {
	src := `build/a.o build/b.o: src/shared.c include/shared.h`
	got, err := parseMakefileDepfile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/shared.c", "include/shared.h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDepfileUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.d")
	os.WriteFile(path, []byte(`foo: bar`), 0o644)
	_, err := ParseDepfile(path, "nope")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

// --- staleness with discovered deps ----------------------------------------

func TestIsStaleDiscoveredVanished(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile("a.c", []byte("a"), 0o644)
	os.WriteFile("a.h", []byte("h"), 0o644)
	os.WriteFile("a.o", []byte("o"), 0o644)

	cache := NewHashCache()
	hC, _ := cache.Hash("a.c")
	hH, _ := cache.Hash("a.h")

	state := &BuildState{Targets: map[string]*TargetState{
		"a.o": {
			RecipeHash:            hashString("recipe"),
			InputHashes:           map[string]string{"a.c": hC},
			Prereqs:               []string{"a.c"},
			DiscoveredPrereqs:     []string{"a.h"},
			DiscoveredInputHashes: map[string]string{"a.h": hH},
		},
	}}

	if state.IsStale([]string{"a.o"}, []string{"a.c"}, "recipe", "", cache) {
		t.Fatal("should not be stale when all inputs unchanged")
	}

	// Delete the discovered header. The model says: vanished discovered dep
	// = changed = stale. Self-heals on rebuild (new run records a fresh set
	// without the deleted file).
	os.Remove("a.h")
	cache = NewHashCache() // invalidate stat cache
	if !state.IsStale([]string{"a.o"}, []string{"a.c"}, "recipe", "", cache) {
		t.Fatal("should be stale when a discovered prereq has vanished")
	}
}

func TestIsStaleDiscoveredChanged(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile("a.c", []byte("a"), 0o644)
	os.WriteFile("a.h", []byte("h-old"), 0o644)
	os.WriteFile("a.o", []byte("o"), 0o644)

	cache := NewHashCache()
	hC, _ := cache.Hash("a.c")
	hH, _ := cache.Hash("a.h")

	state := &BuildState{Targets: map[string]*TargetState{
		"a.o": {
			RecipeHash:            hashString("recipe"),
			InputHashes:           map[string]string{"a.c": hC},
			Prereqs:               []string{"a.c"},
			DiscoveredPrereqs:     []string{"a.h"},
			DiscoveredInputHashes: map[string]string{"a.h": hH},
		},
	}}

	// Change the header content.
	os.WriteFile("a.h", []byte("h-new"), 0o644)
	cache = NewHashCache()
	if !state.IsStale([]string{"a.o"}, []string{"a.c"}, "recipe", "", cache) {
		t.Fatal("should be stale when a discovered prereq's content changed")
	}
}

func TestRecordWholesaleReplacesDiscovered(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile("a.c", []byte("a"), 0o644)
	os.WriteFile("h1.h", []byte("1"), 0o644)
	os.WriteFile("h2.h", []byte("2"), 0o644)
	os.WriteFile("a.o", []byte("o"), 0o644)

	state := &BuildState{Targets: map[string]*TargetState{}}
	cache := NewHashCache()

	// First run discovers h1 and h2.
	state.Record([]string{"a.o"}, []string{"a.c"}, "recipe", "", []string{"h1.h", "h2.h"}, cache)
	ts := state.GetTarget("a.o")
	if got := sortedKeys(ts.DiscoveredInputHashes); !reflect.DeepEqual(got, []string{"h1.h", "h2.h"}) {
		t.Fatalf("first record: discovered = %v, want [h1.h h2.h]", got)
	}

	// Second run only reads h1 — h2 must be dropped (wholesale replace, not
	// union). Otherwise stale edges would slowly reintroduce Make's deleted-
	// header bug. See DESIGN.md §11.
	state.Record([]string{"a.o"}, []string{"a.c"}, "recipe", "", []string{"h1.h"}, cache)
	ts = state.GetTarget("a.o")
	if got := sortedKeys(ts.DiscoveredInputHashes); !reflect.DeepEqual(got, []string{"h1.h"}) {
		t.Fatalf("second record: discovered = %v, want [h1.h] (wholesale replace)", got)
	}
}

// --- end-to-end: real C compile with [deps: gcc] ---------------------------

func TestEndToEndDiscoveredHeaderDeps(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc not available")
	}
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile("foo.h", []byte("#define VALUE 41\n"), 0o644)
	os.WriteFile("main.c", []byte(`#include "foo.h"
int main(){return VALUE;}
`), 0o644)
	os.WriteFile("mkfile", []byte(`
cc = cc

main.o [deps: gcc]: main.c
    $cc -MMD -MF $depfile -c $input -o $target
`), 0o644)

	// First build records main.o with discovered prereq foo.h.
	mustBuild(t, "main.o")

	state := LoadState("")
	ts := state.GetTarget("main.o")
	if ts == nil {
		t.Fatal("no state recorded for main.o")
	}
	if _, ok := ts.DiscoveredInputHashes["foo.h"]; !ok {
		t.Fatalf("foo.h not recorded as discovered dep; got %v", sortedKeys(ts.DiscoveredInputHashes))
	}

	// Depfile under .mk/deps/ should be cleaned up after fold.
	if _, err := os.Stat(filepath.Join(".mk", "deps", "main.o.d")); !os.IsNotExist(err) {
		t.Errorf("depfile was not removed after fold: %v", err)
	}

	// Modify the header. Next build should detect staleness via the
	// discovered edge and rebuild.
	os.WriteFile("foo.h", []byte("#define VALUE 42\n"), 0o644)
	g, st, vars := loadGraphAndState(t, "mkfile", "")
	rule, err := g.Resolve("main.o")
	if err != nil {
		t.Fatal(err)
	}
	ex := NewExecutorWithConfig(g, st, vars, false, false, false, 1, "")
	recipeText := ex.expandRecipe(rule)
	if !st.IsStale(rule.targets, rule.prereqs, recipeText, "", NewHashCache()) {
		t.Fatal("expected main.o to be stale after header change (via discovered edge)")
	}

	// Delete the header AND the #include. main.o must rebuild successfully
	// — no -MP empty-rule crash, the discovered edge self-heals (DESIGN.md §11).
	os.Remove("foo.h")
	os.WriteFile("main.c", []byte(`int main(){return 0;}
`), 0o644)
	mustBuild(t, "main.o")

	state = LoadState("")
	ts = state.GetTarget("main.o")
	if ts == nil {
		t.Fatal("no state recorded for main.o after self-heal rebuild")
	}
	if _, ok := ts.DiscoveredInputHashes["foo.h"]; ok {
		t.Errorf("foo.h still recorded as discovered dep after self-heal; got %v", sortedKeys(ts.DiscoveredInputHashes))
	}
}

func TestEndToEndStdCMkAnnotation(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc not available")
	}
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile("foo.h", []byte("#define V 1\n"), 0o644)
	os.WriteFile("hi.c", []byte(`#include "foo.h"
int main(){return V;}
`), 0o644)
	os.WriteFile("mkfile", []byte(`
include std/c.mk
`), 0o644)

	mustBuild(t, "hi.o")

	state := LoadState("")
	ts := state.GetTarget("hi.o")
	if ts == nil {
		t.Fatal("no state for hi.o")
	}
	if _, ok := ts.DiscoveredInputHashes["foo.h"]; !ok {
		t.Errorf("foo.h not picked up via std/c.mk; got %v", sortedKeys(ts.DiscoveredInputHashes))
	}
}

// --- helpers ----------------------------------------------------------------

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mustBuild(t *testing.T, target string) {
	t.Helper()
	g, state, vars := loadGraphAndState(t, "mkfile", "")
	ex := NewExecutorWithConfig(g, state, vars, false, false, false, 1, "")
	if err := ex.Build(target); err != nil {
		t.Fatalf("build %q: %v", target, err)
	}
	if err := state.Save(""); err != nil {
		t.Fatalf("save state: %v", err)
	}
}

func loadGraphAndState(t *testing.T, mkfile, configSuffix string) (*Graph, *BuildState, *Vars) {
	t.Helper()
	f, err := os.Open(mkfile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ast, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	state := LoadState(configSuffix)
	vars := NewVars()
	g, err := BuildGraph(ast, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g, state, vars
}
