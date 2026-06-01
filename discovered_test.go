// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

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
	cvfile := `
build/foo.o [deps: gcc]: src/foo.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(cvfile))
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
	cvfile := `
build/{name}.o [deps: gcc]: src/{name}.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(cvfile))
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

func TestParseMSVCDepfile(t *testing.T) {
	src := `main.c
Note: including file: C:\Program Files\foo\include\foo.h
Note: including file:  C:\Program Files\foo\include\bar.h
Some other compiler chatter
Note: including file: C:\Program Files\foo\include\foo.h
`
	got := parseMSVCDepfile(src)
	want := []string{
		filepath.Clean(`C:\Program Files\foo\include\foo.h`),
		filepath.Clean(`C:\Program Files\foo\include\bar.h`),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseJSONDepfile(t *testing.T) {
	data := []byte(`["src/foo.c", "include/foo.h", "include/foo.h", "  "]`)
	got, err := parseJSONDepfile(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/foo.c", "include/foo.h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseJSONDepfileMalformed(t *testing.T) {
	_, err := parseJSONDepfile([]byte(`{"not": "an array"}`))
	if err == nil {
		t.Fatal("expected error for non-array JSON depfile")
	}
}

func TestParseLinesDepfileNewline(t *testing.T) {
	src := "src/foo.c\ninclude/foo.h\n\ninclude/foo.h\nsrc/bar.c\n"
	got := parseLinesDepfile(src)
	want := []string{"src/foo.c", "include/foo.h", "src/bar.c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseLinesDepfileNUL(t *testing.T) {
	src := "src/foo.c\x00include/foo.h\x00src/bar.c\x00"
	got := parseLinesDepfile(src)
	want := []string{"src/foo.c", "include/foo.h", "src/bar.c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseDepfileDispatch(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		format  string
		content string
		want    []string
	}{
		{"gcc", "a.o: a.c b.h", []string{"a.c", "b.h"}},
		{"makefile", "a.o: a.c b.h", []string{"a.c", "b.h"}},
		{"json", `["a.c","b.h"]`, []string{"a.c", "b.h"}},
		{"lines", "a.c\nb.h\n", []string{"a.c", "b.h"}},
		{"msvc", "Note: including file: a.c\nNote: including file: b.h\n", []string{"a.c", "b.h"}},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			p := filepath.Join(dir, c.format+".d")
			os.WriteFile(p, []byte(c.content), 0o644)
			got, err := ParseDepfile(p, c.format)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
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
	state.Record([]string{"a.o"}, []string{"a.c"}, "recipe", "", []string{"h1.h", "h2.h"}, nil, cache)
	ts := state.GetTarget("a.o")
	if got := sortedKeys(ts.DiscoveredInputHashes); !reflect.DeepEqual(got, []string{"h1.h", "h2.h"}) {
		t.Fatalf("first record: discovered = %v, want [h1.h h2.h]", got)
	}

	// Second run only reads h1 — h2 must be dropped (wholesale replace, not
	// union). Otherwise stale edges would slowly reintroduce Make's deleted-
	// header bug. See DESIGN.md §11.
	state.Record([]string{"a.o"}, []string{"a.c"}, "recipe", "", []string{"h1.h"}, nil, cache)
	ts = state.GetTarget("a.o")
	if got := sortedKeys(ts.DiscoveredInputHashes); !reflect.DeepEqual(got, []string{"h1.h"}) {
		t.Fatalf("second record: discovered = %v, want [h1.h] (wholesale replace)", got)
	}
}

// --- T1.6: trace mode & envelopes ------------------------------------------

func TestParseReadsAnnotation(t *testing.T) {
	input := `build/foo.o [reads: include/** src/**]: src/foo.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Stmts[0].(Rule)
	if r.Reads != "include/** src/**" {
		t.Errorf("Reads = %q", r.Reads)
	}
}

func TestEnvelopeViolations(t *testing.T) {
	got := envelopeViolations("include/** src/**", []string{
		"include/foo.h", "src/foo.c", "/etc/passwd", "lib/oops.h",
	})
	want := []string{"/etc/passwd", "lib/oops.h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEnvelopeAllInside(t *testing.T) {
	got := envelopeViolations("include/**", []string{"include/foo.h", "include/sub/bar.h"})
	if len(got) != 0 {
		t.Errorf("expected no violations, got %v", got)
	}
}

func TestTraceUnsupportedReturnsClearError(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc not available")
	}
	if ok, _ := traceSupported(); ok {
		t.Skip("trace is supported on this platform — this test asserts the unsupported path")
	}
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile("main.c", []byte("int main(){return 0;}\n"), 0o644)
	os.WriteFile("cvfile", []byte(`
main.o [deps: trace]: main.c
    cc -c $input -o $target
`), 0o644)

	g, state, vars := loadGraphAndState(t, "cvfile", "")
	ex := NewExecutor(g, state, vars, &ExecutorArgs{Jobs: 1})
	err := ex.Build("main.o")
	if err == nil {
		t.Fatal("expected error from [deps: trace] on unsupported platform")
	}
	if !strings.Contains(err.Error(), "trace") {
		t.Errorf("error message should mention trace: %v", err)
	}
}

// --- T1.5: dynamic outputs -------------------------------------------------

func TestParseWritesAnnotation(t *testing.T) {
	input := `gen/ [writes: manifest gen/.manifest]: schema.idl
    echo > $target
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Stmts[0].(Rule)
	if r.Writes != "manifest gen/.manifest" {
		t.Errorf("Writes = %q", r.Writes)
	}
}

func TestWritesManifestRecordsOutputs(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	os.WriteFile("schema.idl", []byte("a\nb\nc\n"), 0o644)
	os.WriteFile("cvfile", []byte(`
gen.stamp [writes: manifest gen.manifest]: schema.idl
    mkdir -p gen
    while read n; do echo "$$n" > gen/"$$n".gen; done < $input
    ls gen/ | sed 's|^|gen/|' > gen.manifest
    touch $target
`), 0o644)

	mustBuild(t, "gen.stamp")

	state := LoadState("")
	ts := state.GetTarget("gen.stamp")
	if ts == nil {
		t.Fatal("no state for gen.stamp")
	}
	got := sortedKeys(ts.DiscoveredOutputHashes)
	want := []string{"gen/a.gen", "gen/b.gen", "gen/c.gen"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("discovered outputs = %v, want %v", got, want)
	}

	// Deleting a discovered output should force a rebuild.
	os.Remove("gen/b.gen")
	cache := NewHashCache()
	if !state.IsStale([]string{"gen.stamp"}, []string{"schema.idl"}, "", "", cache) {
		// recipe text would normally be needed for the recipe-hash check;
		// pass empty since this is a focused check on discoveredOutputsStale.
		// Actually empty recipe text != the recorded hash, so it'd be stale
		// for the wrong reason. Use the recorded recipe text.
	}
	// Use the recorded recipe hash to isolate the dynamic-output check.
	ts2 := state.GetTarget("gen.stamp")
	if !discoveredOutputsStale(ts2, cache) {
		t.Error("expected discoveredOutputsStale to be true after deleting an output")
	}
}

// --- T1.4: scan nodes ------------------------------------------------------

func TestParseScanAnnotation(t *testing.T) {
	input := `build/foo.o [scan: cc -M $cflags $input] [scan-format: gcc]: src/foo.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Stmts[0].(Rule)
	if r.Scan != "cc -M $cflags $input" {
		t.Errorf("Scan = %q", r.Scan)
	}
	if r.ScanFormat != "gcc" {
		t.Errorf("ScanFormat = %q", r.ScanFormat)
	}
}

func TestScanPropagatesToPatternRule(t *testing.T) {
	input := `
build/{name}.o [scan: cc -M $input]: src/{name}.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(input))
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
	// Capture-bound expansion: "src/foo.c" should appear in the scan cmd
	// because the rule context expanded $input via captures? No — captures
	// substitute `{name}` placeholders, but `$input` is a recipe variable
	// expanded at execute time, not at resolve. So the scan command is
	// stored verbatim with capture-substituted placeholders only.
	if !strings.Contains(rule.scan, "$input") {
		t.Errorf("rule.scan = %q (expected to retain $input for recipe-time expansion)", rule.scan)
	}
}

func TestScanBuildsInGraphDiscoveredDep(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc not available")
	}
	dir := t.TempDir()
	chdir(t, dir)

	// gen/config.h is in-graph (rule below). main.c includes it.
	// The scan command discovers it; cv must build gen/config.h before the
	// heavy compile runs.
	os.MkdirAll("gen", 0o755)
	os.WriteFile("main.c", []byte(`#include "gen/config.h"
int main(){return V;}
`), 0o644)
	os.WriteFile("cvfile", []byte(`
cc = cc

gen/config.h:
    echo "#define V 7" > $target

# Scan discovers gen/config.h; the heavy recipe needs it built first.
# -MG treats missing headers as generated (suppresses "not found" error),
# which is exactly the scan-through-generated case.
main.o [scan: $cc -MM -MG -I. $input]: main.c
    $cc -I. -c $input -o $target
`), 0o644)

	mustBuild(t, "main.o")

	// gen/config.h must have been built (by the scan-discovered edge).
	if _, err := os.Stat("gen/config.h"); err != nil {
		t.Errorf("gen/config.h not built: %v", err)
	}

	// And it must show up in the discovered set for main.o.
	state := LoadState("")
	ts := state.GetTarget("main.o")
	if ts == nil {
		t.Fatal("no state for main.o")
	}
	if _, ok := ts.DiscoveredInputHashes["gen/config.h"]; !ok {
		t.Errorf("gen/config.h not recorded as discovered dep; got %v", sortedKeys(ts.DiscoveredInputHashes))
	}
}

// --- T1.3: --verify and undeclared in-graph reads --------------------------

func TestHasRuleForExplicit(t *testing.T) {
	cvfile := `
build/foo.o: src/foo.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildGraph(f, NewVars(), &BuildState{Targets: map[string]*TargetState{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !g.HasRuleFor("build/foo.o") {
		t.Error("HasRuleFor(build/foo.o) = false, want true")
	}
	if g.HasRuleFor("src/foo.c") {
		t.Error("HasRuleFor(src/foo.c) = true, want false (it's only a prereq)")
	}
}

func TestHasRuleForPattern(t *testing.T) {
	cvfile := `
build/{name}.o: src/{name}.c
    cc -c $input -o $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildGraph(f, NewVars(), &BuildState{Targets: map[string]*TargetState{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !g.HasRuleFor("build/foo.o") {
		t.Error("HasRuleFor(build/foo.o) = false, want true (pattern match)")
	}
	if g.HasRuleFor("build/foo.x") {
		t.Error("HasRuleFor(build/foo.x) = true, want false")
	}
}

func TestVerifyDetectsUndeclaredInGraphRead(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc not available")
	}
	dir := t.TempDir()
	chdir(t, dir)

	// gen/config.h is an in-graph target. main.c includes it without
	// declaring the dependency. With --verify, the recipe should error.
	os.MkdirAll("gen", 0o755)
	os.WriteFile("gen/config.h", []byte("#define V 1\n"), 0o644)
	os.WriteFile("main.c", []byte(`#include "gen/config.h"
int main(){return V;}
`), 0o644)
	os.WriteFile("cvfile", []byte(`
cc = cc

gen/config.h:
    @true

main.o [deps: gcc]: main.c
    $cc -MMD -MF $depfile -c $input -o $target
`), 0o644)

	// First: build with --verify. main.o reads gen/config.h (in-graph)
	// without declaring it — the run must fail.
	g, state, vars := loadGraphAndState(t, "cvfile", "")
	ex := NewExecutor(g, state, vars, &ExecutorArgs{Jobs: 1, Verify: true})
	err := ex.Build("main.o")
	if err == nil {
		t.Fatal("expected --verify to error on undeclared in-graph read")
	}
	if !strings.Contains(err.Error(), "gen/config.h") {
		t.Errorf("error message missing offending path: %v", err)
	}

	// Same setup without --verify: build should succeed (warning only).
	os.RemoveAll(".cv")
	os.Remove("main.o")
	g2, state2, vars2 := loadGraphAndState(t, "cvfile", "")
	ex2 := NewExecutor(g2, state2, vars2, &ExecutorArgs{Jobs: 1})
	if err := ex2.Build("main.o"); err != nil {
		t.Fatalf("expected build to succeed without --verify, got: %v", err)
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
	os.WriteFile("cvfile", []byte(`
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

	// Depfile under .cv/deps/ should be cleaned up after fold.
	if _, err := os.Stat(filepath.Join(".cv", "deps", "main.o.d")); !os.IsNotExist(err) {
		t.Errorf("depfile was not removed after fold: %v", err)
	}

	// Modify the header. Next build should detect staleness via the
	// discovered edge and rebuild.
	os.WriteFile("foo.h", []byte("#define VALUE 42\n"), 0o644)
	g, st, vars := loadGraphAndState(t, "cvfile", "")
	rule, err := g.Resolve("main.o")
	if err != nil {
		t.Fatal(err)
	}
	ex := NewExecutor(g, st, vars, &ExecutorArgs{Jobs: 1})
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
	os.WriteFile("cvfile", []byte(`
include std/c.cv
`), 0o644)

	mustBuild(t, "hi.o")

	state := LoadState("")
	ts := state.GetTarget("hi.o")
	if ts == nil {
		t.Fatal("no state for hi.o")
	}
	if _, ok := ts.DiscoveredInputHashes["foo.h"]; !ok {
		t.Errorf("foo.h not picked up via std/c.cv; got %v", sortedKeys(ts.DiscoveredInputHashes))
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
	g, state, vars := loadGraphAndState(t, "cvfile", "")
	ex := NewExecutor(g, state, vars, &ExecutorArgs{Jobs: 1})
	if err := ex.Build(target); err != nil {
		t.Fatalf("build %q: %v", target, err)
	}
	if err := state.Save(""); err != nil {
		t.Fatalf("save state: %v", err)
	}
}

func loadGraphAndState(t *testing.T, cvfile, configSuffix string) (*Graph, *BuildState, *Vars) {
	t.Helper()
	f, err := os.Open(cvfile)
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
