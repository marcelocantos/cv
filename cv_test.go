// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVariables(t *testing.T) {
	input := `
cc = gcc
cflags = -Wall -O2
cflags += -Werror
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.Stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(f.Stmts))
	}

	v1 := f.Stmts[0].(VarAssign)
	if v1.Name != "cc" || v1.Value != "gcc" || v1.Op != OpSet {
		t.Errorf("unexpected var: %+v", v1)
	}

	v2 := f.Stmts[1].(VarAssign)
	if v2.Name != "cflags" || v2.Value != "-Wall -O2" || v2.Op != OpSet {
		t.Errorf("unexpected var: %+v", v2)
	}

	v3 := f.Stmts[2].(VarAssign)
	if v3.Name != "cflags" || v3.Value != "-Werror" || v3.Op != OpAppend {
		t.Errorf("unexpected var: %+v", v3)
	}
}

func TestParseLazy(t *testing.T) {
	input := `lazy version = $[shell echo hello]`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	v := f.Stmts[0].(VarAssign)
	if !v.Lazy {
		t.Error("expected lazy")
	}
}

func TestParseCondAssign(t *testing.T) {
	input := `cc ?= gcc`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	v := f.Stmts[0].(VarAssign)
	if v.Name != "cc" || v.Value != "gcc" || v.Op != OpCondSet {
		t.Errorf("unexpected var: %+v", v)
	}
}

func TestCondAssignSemantics(t *testing.T) {
	input := `
cc = clang
cc ?= gcc
opt ?= O2
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// cc was already set, ?= should not overwrite
	if got := vars.Get("cc"); got != "clang" {
		t.Errorf("cc = %q, want %q", got, "clang")
	}
	// opt was not set, ?= should set it
	if got := vars.Get("opt"); got != "O2" {
		t.Errorf("opt = %q, want %q", got, "O2")
	}
}

func TestParseRule(t *testing.T) {
	input := `
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if len(r.Targets) != 1 || r.Targets[0] != "build/{name}.o" {
		t.Errorf("unexpected targets: %v", r.Targets)
	}
	if len(r.Prereqs) != 1 || r.Prereqs[0] != "src/{name}.c" {
		t.Errorf("unexpected prereqs: %v", r.Prereqs)
	}
	if len(r.Recipe) != 1 {
		t.Errorf("expected 1 recipe line, got %d", len(r.Recipe))
	}
	if r.IsTask {
		t.Error("should not be a task")
	}
}

func TestInlineComments(t *testing.T) {
	input := `
cc = gcc # the compiler

build/app: main.o lib.o # link step
    $cc -o $target $inputs
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	// Variable assignment: comment should be stripped
	v := f.Stmts[0].(VarAssign)
	if v.Value != "gcc" {
		t.Errorf("variable value = %q, want %q", v.Value, "gcc")
	}

	// Rule: comment should be stripped from prereqs
	r := f.Stmts[1].(Rule)
	if len(r.Prereqs) != 2 || r.Prereqs[0] != "main.o" || r.Prereqs[1] != "lib.o" {
		t.Errorf("prereqs = %v, want [main.o lib.o]", r.Prereqs)
	}

	// Recipe should still contain the full command (no stripping)
	if len(r.Recipe) != 1 || r.Recipe[0] != "$cc -o $target $inputs" {
		t.Errorf("recipe = %v, want [\"$cc -o $target $inputs\"]", r.Recipe)
	}
}

func TestParseTask(t *testing.T) {
	input := `
!clean:
    rm -rf build/
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if !r.IsTask {
		t.Error("should be a task")
	}
	if r.Targets[0] != "clean" {
		t.Errorf("expected target 'clean', got %q", r.Targets[0])
	}
}

func TestParseConditional(t *testing.T) {
	input := `
if $cc == gcc
    cflags += -Wextra
end
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	c := f.Stmts[0].(Conditional)
	if len(c.Branches) != 1 {
		t.Fatalf("expected 1 branch, got %d", len(c.Branches))
	}
	if c.Branches[0].Left != "$cc" {
		t.Errorf("expected left '$cc', got %q", c.Branches[0].Left)
	}
}

func TestVarExpansion(t *testing.T) {
	v := NewVars()
	v.Set("name", "world")
	v.Set("greeting", "hello")

	tests := []struct {
		input string
		want  string
	}{
		{"$name", "world"},
		{"${name}", "world"},
		{"$greeting $name", "hello world"},
		{"${greeting}_${name}", "hello_world"},
		{"no vars here", "no vars here"},
		{"$$literal", "$literal"},
	}

	for _, tt := range tests {
		got := v.Expand(tt.input)
		if got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSubstitutionRef(t *testing.T) {
	v := NewVars()
	v.Set("src", "foo.c bar.c baz.c")

	got := v.Expand("$src:.c=.o")
	if got != "foo.o bar.o baz.o" {
		t.Errorf("substitution ref = %q, want %q", got, "foo.o bar.o baz.o")
	}
}

func TestFuncSyntax(t *testing.T) {
	v := NewVars()
	v.Set("files", "foo.c bar.h baz.c")

	// $[...] should invoke cv functions
	got := v.Expand("$[filter %.c,$files]")
	if got != "foo.c baz.c" {
		t.Errorf("$[filter] = %q, want %q", got, "foo.c baz.c")
	}

	// $(...) should pass through for shell use
	got = v.Expand("echo $(date)")
	if got != "echo $(date)" {
		t.Errorf("$(...) should pass through, got %q", got)
	}
}

func TestBuiltinFunctions(t *testing.T) {
	v := NewVars()
	v.Set("src", "foo.c bar.c baz.c")
	v.Set("objs", "foo.o bar.o baz.o")
	v.Set("files", "main.c lib.c main.h lib.h")

	tests := []struct {
		input string
		want  string
	}{
		// subst
		{"$[subst .c,.o,$src]", "foo.o bar.o baz.o"},
		// filter
		{"$[filter %.c,$files]", "main.c lib.c"},
		// filter-out
		{"$[filter-out %.h,$files]", "main.c lib.c"},
		// dir
		{"$[dir src/foo.c]", "src/"},
		{"$[dir foo.c]", "./"},
		// notdir
		{"$[notdir src/foo.c]", "foo.c"},
		// basename
		{"$[basename src/foo.c]", "src/foo"},
		// suffix
		{"$[suffix foo.c bar.h]", ".c .h"},
		// addprefix
		{"$[addprefix src/,$src]", "src/foo.c src/bar.c src/baz.c"},
		// addsuffix
		{"$[addsuffix .bak,$objs]", "foo.o.bak bar.o.bak baz.o.bak"},
		// sort (also deduplicates)
		{"$[sort c b a b]", "a b c"},
		// word
		{"$[word 2,$src]", "bar.c"},
		// words
		{"$[words $src]", "3"},
		// strip
		{"$[strip  foo   bar  ]", "foo bar"},
		// findstring
		{"$[findstring bar,$src]", "bar"},
		{"$[findstring xyz,$src]", ""},
		// if
		{"$[if yes,true,false]", "true"},
		{"$[if ,true,false]", "false"},
		{"$[if yes,true]", "true"},
		{"$[if ,true]", ""},
		// patsubst
		{"$[patsubst %.c,%.o,$src]", "foo.o bar.o baz.o"},
	}

	for _, tt := range tests {
		got := v.Expand(tt.input)
		if got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestVarProperties(t *testing.T) {
	v := NewVars()
	v.Set("src", "src/main.c")

	tests := []struct {
		input string
		want  string
	}{
		{"$src.dir", "src"},
		{"$src.file", "main.c"},
	}

	for _, tt := range tests {
		got := v.Expand(tt.input)
		if got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLineContinuation(t *testing.T) {
	input := "cflags = -Wall \\\n-O2 \\\n-Werror\n"
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(f.Stmts))
	}
	v := f.Stmts[0].(VarAssign)
	if v.Value != "-Wall -O2 -Werror" {
		t.Errorf("line continuation: got %q, want %q", v.Value, "-Wall -O2 -Werror")
	}
}

func TestEndToEnd(t *testing.T) {
	// Set up a temporary directory
	dir := t.TempDir()

	// Create source files
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "main.c"), []byte(`int main() { return 0; }`), 0o644)

	// Create cvfile
	cvfile := `
cc = cc
src = src/main.c
obj = $src:.c=.o

build/{name}.o: src/{name}.c
    $cc -c $input -o $target

build/app: $obj
    $cc -o $target $inputs

!clean:
    rm -rf build/ .cv/
`
	// Parse
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	// Build graph
	vars := NewVars()
	// Override working directory context
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Write cvfile to disk for $[wildcard] etc.
	os.WriteFile(filepath.Join(dir, "cvfile"), []byte(cvfile), 0o644)

	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verify default target
	def := graph.DefaultTarget()
	if def != "build/app" {
		t.Errorf("default target = %q, want %q", def, "build/app")
	}

	// Verify pattern resolution
	rule, err := graph.Resolve("build/main.o")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.prereqs) != 1 || rule.prereqs[0] != "src/main.c" {
		t.Errorf("prereqs = %v, want [src/main.c]", rule.prereqs)
	}
	if rule.stem != "main" {
		t.Errorf("stem = %q, want %q", rule.stem, "main")
	}
}

func TestParseKeep(t *testing.T) {
	input := `
build/data.db [keep]: schema.sql
    sqlite3 $target < $input
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if !r.Keep {
		t.Error("expected [keep]")
	}
	if r.Targets[0] != "build/data.db" {
		t.Errorf("target = %q, want %q", r.Targets[0], "build/data.db")
	}
}

func TestParseKeepPattern(t *testing.T) {
	input := `
build/{name}.db [keep]: src/{name}.sql
    sqlite3 $target < $input
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if !r.Keep {
		t.Error("expected [keep]")
	}
}

func TestKeepPropagation(t *testing.T) {
	input := `
build/data.db [keep]: schema.sql
    sqlite3 $target < $input
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	rule, err := graph.Resolve("build/data.db")
	if err != nil {
		t.Fatal(err)
	}
	if !rule.keep {
		t.Error("resolved rule should have keep=true")
	}
}

func TestKeepPatternPropagation(t *testing.T) {
	input := `
build/{name}.db [keep]: src/{name}.sql
    sqlite3 $target < $input
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "foo.sql"), []byte("CREATE TABLE foo;"), 0o644)

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	rule, err := graph.Resolve("build/foo.db")
	if err != nil {
		t.Fatal(err)
	}
	if !rule.keep {
		t.Error("resolved pattern rule should have keep=true")
	}
}

func TestChangedVariable(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create source files
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb"), 0o644)

	cvfile := `
out.txt: a.txt b.txt
    echo $changed > $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// First build: all prereqs are changed (no previous state)
	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := exec.Build("out.txt"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "out.txt"))
	if s := strings.TrimSpace(string(got)); s != "a.txt b.txt" {
		t.Errorf("first build $changed = %q, want %q", s, "a.txt b.txt")
	}

	// Save and reload state
	state.Save("")
	state = LoadState("")

	// Modify only b.txt
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb-modified"), 0o644)

	graph, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec = NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := exec.Build("out.txt"); err != nil {
		t.Fatal(err)
	}

	got, _ = os.ReadFile(filepath.Join(dir, "out.txt"))
	if s := strings.TrimSpace(string(got)); s != "b.txt" {
		t.Errorf("second build $changed = %q, want %q", s, "b.txt")
	}
}

func TestParseMultiOutput(t *testing.T) {
	input := `
gen/{name}.pb.h gen/{name}.pb.cc: proto/{name}.proto
    protoc --cpp_out=gen/ $input
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if len(r.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(r.Targets))
	}
	if r.Targets[0] != "gen/{name}.pb.h" || r.Targets[1] != "gen/{name}.pb.cc" {
		t.Errorf("unexpected targets: %v", r.Targets)
	}
}

func TestMultiOutputResolve(t *testing.T) {
	input := `
gen/{name}.pb.h gen/{name}.pb.cc: proto/{name}.proto
    protoc --cpp_out=gen/ $input
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.MkdirAll(filepath.Join(dir, "proto"), 0o755)
	os.WriteFile(filepath.Join(dir, "proto", "foo.proto"), []byte("syntax = \"proto3\";"), 0o644)

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Resolving either target should return the same multi-output rule
	rule1, err := graph.Resolve("gen/foo.pb.h")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule1.targets) != 2 {
		t.Fatalf("expected 2 targets, got %d: %v", len(rule1.targets), rule1.targets)
	}
	if rule1.target != "gen/foo.pb.h" {
		t.Errorf("primary target = %q, want %q", rule1.target, "gen/foo.pb.h")
	}
	if rule1.targets[1] != "gen/foo.pb.cc" {
		t.Errorf("second target = %q, want %q", rule1.targets[1], "gen/foo.pb.cc")
	}

	rule2, err := graph.Resolve("gen/foo.pb.cc")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule2.targets) != 2 {
		t.Fatalf("expected 2 targets from second resolve, got %d", len(rule2.targets))
	}
	// Primary target is always the first listed, regardless of which output was requested
	if rule2.target != "gen/foo.pb.h" {
		t.Errorf("primary target from second resolve = %q, want %q", rule2.target, "gen/foo.pb.h")
	}
}

func TestMultiOutputExplicitResolve(t *testing.T) {
	input := `
gen/foo.h gen/foo.cc: proto/foo.proto
    protoc --cpp_out=gen/ $input
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Both targets should resolve to the same rule
	rule1, err := graph.Resolve("gen/foo.h")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule1.targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(rule1.targets))
	}

	rule2, err := graph.Resolve("gen/foo.cc")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule2.targets) != 2 {
		t.Fatalf("expected 2 targets from second resolve, got %d", len(rule2.targets))
	}
}

func TestMultiOutputExecution(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("hello"), 0o644)

	// Recipe creates both outputs
	cvfile := `
out1.txt out2.txt: input.txt
    cp $input out1.txt
    cp $input out2.txt
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})

	// Build first output
	if err := exec.Build("out1.txt"); err != nil {
		t.Fatal(err)
	}

	// Both should exist
	if _, err := os.Stat(filepath.Join(dir, "out1.txt")); err != nil {
		t.Error("out1.txt should exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "out2.txt")); err != nil {
		t.Error("out2.txt should exist")
	}

	// Building second output should be a no-op (already built)
	if err := exec.Build("out2.txt"); err != nil {
		t.Fatal(err)
	}

	// State should have entries for both
	if state.Targets["out1.txt"] == nil {
		t.Error("state should have out1.txt")
	}
	if state.Targets["out2.txt"] == nil {
		t.Error("state should have out2.txt")
	}
}

func TestParseOrderOnly(t *testing.T) {
	input := `
build/foo.o: src/foo.c | build/
    gcc -c $input -o $target
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if len(r.Prereqs) != 1 || r.Prereqs[0] != "src/foo.c" {
		t.Errorf("prereqs = %v, want [src/foo.c]", r.Prereqs)
	}
	if len(r.OrderOnlyPrereqs) != 1 || r.OrderOnlyPrereqs[0] != "build/" {
		t.Errorf("order-only = %v, want [build/]", r.OrderOnlyPrereqs)
	}
}

func TestOrderOnlyNoRebuild(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "src.txt"), []byte("source"), 0o644)
	os.WriteFile(filepath.Join(dir, "order.txt"), []byte("order1"), 0o644)

	cvfile := `
out.txt: src.txt | order.txt
    cat $input > $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// First build
	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := exec.Build("out.txt"); err != nil {
		t.Fatal(err)
	}
	state.Save("")

	// Overwrite out.txt with a sentinel so we can detect if recipe re-runs
	os.WriteFile(filepath.Join(dir, "out.txt"), []byte("sentinel"), 0o644)

	// Modify the order-only prereq
	os.WriteFile(filepath.Join(dir, "order.txt"), []byte("order2-changed"), 0o644)

	// Reload state and rebuild — recipe should NOT run
	state = LoadState("")
	graph, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec = NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := exec.Build("out.txt"); err != nil {
		t.Fatal(err)
	}

	// Sentinel should still be there — recipe didn't re-run
	got, _ := os.ReadFile(filepath.Join(dir, "out.txt"))
	if string(got) != "sentinel" {
		t.Errorf("recipe should NOT have re-run, but out.txt = %q", string(got))
	}
}

func TestOrderOnlyInputsExclusion(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)

	// order-only prereq should NOT appear in $inputs or $input
	cvfile := `
out.txt: a.txt | b.txt
    echo "$inputs" > $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := exec.Build("out.txt"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "out.txt"))
	if s := strings.TrimSpace(string(got)); s != "a.txt" {
		t.Errorf("$inputs = %q, want %q (order-only should be excluded)", s, "a.txt")
	}
}

func TestUnscopedInclude(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "common.cv"), []byte("cc = clang\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "src.c"), []byte("int main() { return 0; }"), 0o644)

	cvfile := `
include common.cv

build/app: src.c
    $cc -o $target $input
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Variable from included file should be visible
	if got := vars.Get("cc"); got != "clang" {
		t.Errorf("cc = %q, want %q", got, "clang")
	}

	// Rule from root cvfile should work
	rule, err := graph.Resolve("build/app")
	if err != nil {
		t.Fatal(err)
	}
	if rule.prereqs[0] != "src.c" {
		t.Errorf("prereqs = %v, want [src.c]", rule.prereqs)
	}
}

func TestScopedInclude(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "cvfile"), []byte(`
src = foo.c bar.c

build/libfoo.a: build/foo.o build/bar.o
    ar rcs $target $inputs
`), 0o644)
	os.WriteFile(filepath.Join(dir, "lib", "foo.c"), []byte("void foo() {}"), 0o644)
	os.WriteFile(filepath.Join(dir, "lib", "bar.c"), []byte("void bar() {}"), 0o644)

	cvfile := `
cc = gcc
include lib/cvfile as lib
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Scoped variable should be accessible as lib.src
	if got := vars.Get("lib.src"); got != "foo.c bar.c" {
		t.Errorf("lib.src = %q, want %q", got, "foo.c bar.c")
	}

	// Targets should be rebased under lib/
	rule, err := graph.Resolve("lib/build/libfoo.a")
	if err != nil {
		t.Fatal(err)
	}
	if rule.target != "lib/build/libfoo.a" {
		t.Errorf("target = %q, want %q", rule.target, "lib/build/libfoo.a")
	}
	// Prereqs should also be rebased
	expected := []string{"lib/build/foo.o", "lib/build/bar.o"}
	if len(rule.prereqs) != 2 || rule.prereqs[0] != expected[0] || rule.prereqs[1] != expected[1] {
		t.Errorf("prereqs = %v, want %v", rule.prereqs, expected)
	}
}

func TestScopedIncludeInheritance(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	// Child cvfile uses $cc from parent
	os.WriteFile(filepath.Join(dir, "lib", "cvfile"), []byte(`
compiler = $cc
`), 0o644)

	cvfile := `
cc = clang
include lib/cvfile as lib
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Child should have inherited cc from parent and used it
	if got := vars.Get("lib.compiler"); got != "clang" {
		t.Errorf("lib.compiler = %q, want %q", got, "clang")
	}

	// Parent's cc should not be affected
	if got := vars.Get("cc"); got != "clang" {
		t.Errorf("cc = %q, want %q", got, "clang")
	}
}

func TestPatternDiscovery(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create two subdirectories with mkfiles
	for _, sub := range []string{"lib", "app"} {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
		os.WriteFile(filepath.Join(dir, sub, "cvfile"), []byte(fmt.Sprintf(`
name = %s
`, sub)), 0o644)
	}

	cvfile := `
include {path}/cvfile as {path}
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Each subdirectory's variables should be scoped
	if got := vars.Get("app.name"); got != "app" {
		t.Errorf("app.name = %q, want %q", got, "app")
	}
	if got := vars.Get("lib.name"); got != "lib" {
		t.Errorf("lib.name = %q, want %q", got, "lib")
	}
}

func TestScopedIncludePatternRule(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "cvfile"), []byte(`
build/{name}.o: {name}.c
    gcc -c $input -o $target
`), 0o644)
	os.WriteFile(filepath.Join(dir, "lib", "foo.c"), []byte("void foo() {}"), 0o644)

	cvfile := `
include lib/cvfile as lib
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Pattern rule targets should be rebased: lib/build/{name}.o
	rule, err := graph.Resolve("lib/build/foo.o")
	if err != nil {
		t.Fatal(err)
	}
	if rule.target != "lib/build/foo.o" {
		t.Errorf("target = %q, want %q", rule.target, "lib/build/foo.o")
	}
	if len(rule.prereqs) != 1 || rule.prereqs[0] != "lib/foo.c" {
		t.Errorf("prereqs = %v, want [lib/foo.c]", rule.prereqs)
	}
}

func TestScopedVariableExpansion(t *testing.T) {
	v := NewVars()
	v.Set("lib.src", "foo.c bar.c")
	v.Set("target", "build/main.o")

	tests := []struct {
		input string
		want  string
	}{
		// Scoped variable lookup
		{"$lib.src", "foo.c bar.c"},
		// Property still works
		{"$target.dir", "build"},
		{"$target.file", "main.o"},
		// Scoped + property
		{"$lib.src.dir", "."},
	}

	for _, tt := range tests {
		got := v.Expand(tt.input)
		if got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSiblingCrossReference(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create lib/ with a library target
	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "cvfile"), []byte(`
build/libfoo.a: foo.o
    ar rcs $target $input
`), 0o644)
	os.WriteFile(filepath.Join(dir, "lib", "foo.o"), []byte{}, 0o644)

	// Create app/ that references ../lib/build/libfoo.a
	os.MkdirAll(filepath.Join(dir, "app"), 0o755)
	os.WriteFile(filepath.Join(dir, "app", "cvfile"), []byte(`
build/app: main.o ../lib/build/libfoo.a
    gcc -o $target $inputs
`), 0o644)
	os.WriteFile(filepath.Join(dir, "app", "main.o"), []byte{}, 0o644)

	cvfile := `
include lib/cvfile as lib
include app/cvfile as app
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// app/build/app should depend on lib/build/libfoo.a via ../lib/ resolution
	rule, err := graph.Resolve("app/build/app")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"app/main.o", "lib/build/libfoo.a"}
	if len(rule.prereqs) != 2 || rule.prereqs[0] != expected[0] || rule.prereqs[1] != expected[1] {
		t.Errorf("prereqs = %v, want %v", rule.prereqs, expected)
	}

	// lib/build/libfoo.a should also be resolvable in the same graph
	libRule, err := graph.Resolve("lib/build/libfoo.a")
	if err != nil {
		t.Fatal(err)
	}
	if libRule.target != "lib/build/libfoo.a" {
		t.Errorf("target = %q, want %q", libRule.target, "lib/build/libfoo.a")
	}
}

func TestNestedScopedInclude(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create nested structure: lib/core/cvfile included by lib/cvfile
	os.MkdirAll(filepath.Join(dir, "lib", "core"), 0o755)
	os.WriteFile(filepath.Join(dir, "lib", "core", "cvfile"), []byte(`
name = core-impl

build/core.a: core.o
    ar rcs $target $input
`), 0o644)
	os.WriteFile(filepath.Join(dir, "lib", "core", "core.o"), []byte{}, 0o644)

	os.WriteFile(filepath.Join(dir, "lib", "cvfile"), []byte(`
include core/cvfile as core

build/libfoo.a: core/build/core.a
    ar rcs $target $input
`), 0o644)

	cvfile := `
include lib/cvfile as lib
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Grandchild variable should be scoped as lib.core.name
	if got := vars.Get("lib.core.name"); got != "core-impl" {
		t.Errorf("lib.core.name = %q, want %q", got, "core-impl")
	}

	// Grandchild target should be double-rebased: lib/core/build/core.a
	rule, err := graph.Resolve("lib/core/build/core.a")
	if err != nil {
		t.Fatal(err)
	}
	if rule.target != "lib/core/build/core.a" {
		t.Errorf("target = %q, want %q", rule.target, "lib/core/build/core.a")
	}

	// lib/build/libfoo.a should depend on lib/core/build/core.a
	libRule, err := graph.Resolve("lib/build/libfoo.a")
	if err != nil {
		t.Fatal(err)
	}
	if len(libRule.prereqs) != 1 || libRule.prereqs[0] != "lib/core/build/core.a" {
		t.Errorf("prereqs = %v, want [lib/core/build/core.a]", libRule.prereqs)
	}
}

func TestNestedPatternDiscovery(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Root discovers lib/ and app/ via pattern
	// lib/cvfile discovers lib/core/ and lib/util/ via pattern
	for _, sub := range []string{"lib/core", "lib/util"} {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
		name := filepath.Base(sub)
		os.WriteFile(filepath.Join(dir, sub, "cvfile"), []byte(fmt.Sprintf(`
name = %s
`, name)), 0o644)
	}

	os.WriteFile(filepath.Join(dir, "lib", "cvfile"), []byte(`
include {path}/cvfile as {path}
`), 0o644)

	os.MkdirAll(filepath.Join(dir, "app"), 0o755)
	os.WriteFile(filepath.Join(dir, "app", "cvfile"), []byte(`
name = app
`), 0o644)

	cvfile := `
include {path}/cvfile as {path}
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Top-level scoped variables
	if got := vars.Get("app.name"); got != "app" {
		t.Errorf("app.name = %q, want %q", got, "app")
	}

	// Nested pattern discovery: lib.core.name and lib.util.name
	if got := vars.Get("lib.core.name"); got != "core" {
		t.Errorf("lib.core.name = %q, want %q", got, "core")
	}
	if got := vars.Get("lib.util.name"); got != "util" {
		t.Errorf("lib.util.name = %q, want %q", got, "util")
	}
}

func TestWhyStale(t *testing.T) {
	state := &BuildState{Targets: make(map[string]*TargetState)}

	// No previous build
	reasons := state.WhyStale([]string{"foo"}, []string{"bar"}, "recipe", "", NewHashCache())
	if len(reasons) != 1 || reasons[0] != "foo: no previous build recorded" {
		t.Errorf("WhyStale = %v, want [foo: no previous build recorded]", reasons)
	}
}

func TestParseFingerprint(t *testing.T) {
	input := `
extracted/config.json [fingerprint: tar xf archive.tar.gz -O config.json]: archive.tar.gz
    tar xf $input -C extracted/
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if r.Fingerprint != "tar xf archive.tar.gz -O config.json" {
		t.Errorf("fingerprint = %q, want %q", r.Fingerprint, "tar xf archive.tar.gz -O config.json")
	}
	if r.Targets[0] != "extracted/config.json" {
		t.Errorf("target = %q, want %q", r.Targets[0], "extracted/config.json")
	}
}

func TestParseFingerprintAndKeep(t *testing.T) {
	input := `
app.img [keep] [fingerprint: docker inspect myapp]: Dockerfile
    docker build -t myapp .
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if !r.Keep {
		t.Error("expected [keep]")
	}
	if r.Fingerprint != "docker inspect myapp" {
		t.Errorf("fingerprint = %q, want %q", r.Fingerprint, "docker inspect myapp")
	}
	if r.Targets[0] != "app.img" {
		t.Errorf("target = %q, want %q", r.Targets[0], "app.img")
	}
}

func TestFingerprintStaleness(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create two files to put in the tarball
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"version": 1}`), 0o644)
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other"), 0o644)

	// Create the initial tarball
	createTarball(t, dir, "archive.tar.gz", []string{"config.json", "other.txt"})

	// cvfile: extract config.json from tarball, using fingerprint to track
	// only config.json's content within the archive
	cvfile := `
extracted/config.json [fingerprint: tar xf archive.tar.gz -O config.json]: archive.tar.gz
    mkdir -p extracted
    tar xf $input -C extracted/ config.json
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// First build
	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := exec.Build("extracted/config.json"); err != nil {
		t.Fatal(err)
	}
	state.Save("")

	// Verify extracted content
	got, _ := os.ReadFile(filepath.Join(dir, "extracted", "config.json"))
	if string(got) != `{"version": 1}` {
		t.Fatalf("extracted config = %q, want %q", string(got), `{"version": 1}`)
	}

	// --- Modify other.txt (not config.json) and recreate tarball ---
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other-modified"), 0o644)
	createTarball(t, dir, "archive.tar.gz", []string{"config.json", "other.txt"})

	// Write a sentinel to detect if recipe re-runs
	os.WriteFile(filepath.Join(dir, "extracted", "config.json"), []byte("sentinel"), 0o644)

	// Reload state and rebuild — should NOT rebuild (fingerprint unchanged)
	state = LoadState("")
	graph, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec = NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := exec.Build("extracted/config.json"); err != nil {
		t.Fatal(err)
	}

	got, _ = os.ReadFile(filepath.Join(dir, "extracted", "config.json"))
	if string(got) != "sentinel" {
		t.Errorf("recipe should NOT have re-run (fingerprint unchanged), but config = %q", string(got))
	}

	// --- Now modify config.json and recreate tarball ---
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"version": 2}`), 0o644)
	createTarball(t, dir, "archive.tar.gz", []string{"config.json", "other.txt"})

	// Reload state and rebuild — SHOULD rebuild (fingerprint changed)
	state = LoadState("")
	graph, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec = NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := exec.Build("extracted/config.json"); err != nil {
		t.Fatal(err)
	}

	got, _ = os.ReadFile(filepath.Join(dir, "extracted", "config.json"))
	if string(got) != `{"version": 2}` {
		t.Errorf("recipe SHOULD have re-run (fingerprint changed), but config = %q", string(got))
	}
}

func TestFingerprintPropagation(t *testing.T) {
	input := `
extracted/config.json [fingerprint: tar xf archive.tar.gz -O config.json]: archive.tar.gz
    tar xf $input -C extracted/
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	rule, err := graph.Resolve("extracted/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if rule.fingerprint != "tar xf archive.tar.gz -O config.json" {
		t.Errorf("fingerprint = %q, want %q", rule.fingerprint, "tar xf archive.tar.gz -O config.json")
	}
}

func TestParallelIndependent(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)

	// Two independent targets
	cvfile := `
out1.txt: a.txt
    cp $input $target

out2.txt: b.txt
    cp $input $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 2})
	if err := exec.Build("out1.txt"); err != nil {
		t.Fatal(err)
	}
	if err := exec.Build("out2.txt"); err != nil {
		t.Fatal(err)
	}

	got1, _ := os.ReadFile(filepath.Join(dir, "out1.txt"))
	got2, _ := os.ReadFile(filepath.Join(dir, "out2.txt"))
	if string(got1) != "a" {
		t.Errorf("out1 = %q, want %q", string(got1), "a")
	}
	if string(got2) != "b" {
		t.Errorf("out2 = %q, want %q", string(got2), "b")
	}
}

func TestParallelDiamond(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "root.txt"), []byte("root"), 0o644)

	// Diamond: top depends on left and right, both depend on root.txt
	// The recipe for each intermediate writes a unique marker.
	cvfile := `
top.txt: left.txt right.txt
    cat $inputs > $target

left.txt: root.txt
    echo left:$(cat $input) > $target

right.txt: root.txt
    echo right:$(cat $input) > $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 4})
	if err := exec.Build("top.txt"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "top.txt"))
	content := string(got)
	if !strings.Contains(content, "left:root") || !strings.Contains(content, "right:root") {
		t.Errorf("top.txt = %q, expected both left:root and right:root", content)
	}
}

func TestParallelMultiOutput(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("data"), 0o644)

	// Multi-output rule: recipe creates both outputs.
	// A counter file tracks how many times the recipe runs.
	cvfile := `
out1.txt out2.txt: input.txt
    cp $input out1.txt
    cp $input out2.txt
    echo x >> counter.txt
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 4})

	// Build both outputs — recipe should only run once
	if err := exec.Build("out1.txt"); err != nil {
		t.Fatal(err)
	}
	if err := exec.Build("out2.txt"); err != nil {
		t.Fatal(err)
	}

	counter, _ := os.ReadFile(filepath.Join(dir, "counter.txt"))
	lines := strings.TrimSpace(string(counter))
	if lines != "x" {
		t.Errorf("recipe ran %d times (counter=%q), want 1", strings.Count(lines, "x"), lines)
	}
}

func TestParallelErrorPropagation(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "good.txt"), []byte("good"), 0o644)

	// "bad" target always fails; "good_out" is independent
	cvfile := `
bad.txt: good.txt
    exit 1

good_out.txt: good.txt
    cp $input $target

top.txt: bad.txt
    echo should not run > $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	exec := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 4})

	// good_out should succeed despite bad existing
	if err := exec.Build("good_out.txt"); err != nil {
		t.Fatalf("good_out.txt should succeed: %v", err)
	}

	// top depends on bad, should fail
	if err := exec.Build("top.txt"); err == nil {
		t.Fatal("top.txt should fail (depends on bad.txt)")
	}

	// good_out should still exist
	if _, err := os.Stat(filepath.Join(dir, "good_out.txt")); err != nil {
		t.Error("good_out.txt should exist")
	}

	// top.txt should not have been created
	if _, err := os.Stat(filepath.Join(dir, "top.txt")); err == nil {
		t.Error("top.txt should NOT exist (prereq failed)")
	}
}

func TestParseFuncDef(t *testing.T) {
	input := `
fn objpath(src):
    return $src:src/%.c=build/%.o
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(f.Stmts))
	}
	fn := f.Stmts[0].(FuncDef)
	if fn.Name != "objpath" {
		t.Errorf("name = %q, want %q", fn.Name, "objpath")
	}
	if len(fn.Params) != 1 || fn.Params[0] != "src" {
		t.Errorf("params = %v, want [src]", fn.Params)
	}
	if fn.Body != "$src:src/%.c=build/%.o" {
		t.Errorf("body = %q, want %q", fn.Body, "$src:src/%.c=build/%.o")
	}
}

func TestUserFuncEval(t *testing.T) {
	input := `
fn objpath(src):
    return $[patsubst src/%.c,build/%.o,$src]

src = src/foo.c src/bar.c
obj = $[objpath $src]
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := vars.Get("obj"); got != "build/foo.o build/bar.o" {
		t.Errorf("obj = %q, want %q", got, "build/foo.o build/bar.o")
	}
}

func TestUserFuncMultiParam(t *testing.T) {
	v := NewVars()
	fn := &FuncDef{Name: "greet", Params: []string{"greeting", "name"}, Body: "$greeting $name!"}
	v.SetFunc(fn)

	got := v.Expand("$[greet hello world]")
	if got != "hello world!" {
		t.Errorf("greet = %q, want %q", got, "hello world!")
	}
}

func TestUserFuncLastParamCollectsRest(t *testing.T) {
	v := NewVars()
	fn := &FuncDef{Name: "wrap", Params: []string{"tag", "content"}, Body: "<$tag>$content</$tag>"}
	v.SetFunc(fn)

	got := v.Expand("$[wrap div hello world foo]")
	if got != "<div>hello world foo</div>" {
		t.Errorf("wrap = %q, want %q", got, "<div>hello world foo</div>")
	}
}

func TestUserFuncInRule(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "input.txt"), []byte("hello"), 0o644)

	cvfile := `
fn upper(file):
    return $file.upper

out.txt: input.txt
    cp $input $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	ex := NewExecutor(graph, state, vars, &ExecutorArgs{Jobs: 1})
	if err := ex.Build("out.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestHashCacheReuse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("content"), 0o644)

	cache := NewHashCache()

	h1, err := cache.Hash(path)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := cache.Hash(path)
	if err != nil {
		t.Fatal(err)
	}

	if h1 != h2 {
		t.Errorf("hash mismatch: %q != %q", h1, h2)
	}

	// Verify cache has an entry
	cache.mu.Lock()
	if _, ok := cache.entries[path]; !ok {
		t.Error("expected cache entry")
	}
	cache.mu.Unlock()
}

func TestHashCacheInvalidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("content1"), 0o644)

	cache := NewHashCache()

	h1, err := cache.Hash(path)
	if err != nil {
		t.Fatal(err)
	}

	// Modify the file (changes mtime and possibly size)
	os.WriteFile(path, []byte("content2-modified"), 0o644)

	h2, err := cache.Hash(path)
	if err != nil {
		t.Fatal(err)
	}

	if h1 == h2 {
		t.Error("hash should differ after file modification")
	}
}

func TestParseConfigDef(t *testing.T) {
	input := `
config debug:
    excludes release
    cxxflags += -O0 -g -DDEBUG
    ldflags += -g

config release:
    excludes debug
    cxxflags += -O2 -DNDEBUG

config asan:
    requires dist
    cxxflags += -fsanitize=address
    ldflags += -fsanitize=address
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.Stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(f.Stmts))
	}

	// debug config
	cfg := f.Stmts[0].(ConfigDef)
	if cfg.Name != "debug" {
		t.Errorf("name = %q, want %q", cfg.Name, "debug")
	}
	if len(cfg.Excludes) != 1 || cfg.Excludes[0] != "release" {
		t.Errorf("excludes = %v, want [release]", cfg.Excludes)
	}
	if len(cfg.Vars) != 2 {
		t.Errorf("expected 2 vars, got %d", len(cfg.Vars))
	}

	// asan config
	cfg3 := f.Stmts[2].(ConfigDef)
	if cfg3.Name != "asan" {
		t.Errorf("name = %q, want %q", cfg3.Name, "asan")
	}
	if len(cfg3.Requires) != 1 || cfg3.Requires[0] != "dist" {
		t.Errorf("requires = %v, want [dist]", cfg3.Requires)
	}
}

func TestConfigVarOverride(t *testing.T) {
	input := `
opt = none

config debug:
    opt = debug_val

out.txt:
    echo $opt > $target
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, []string{"debug"})
	if err != nil {
		t.Fatal(err)
	}

	if got := vars.Get("opt"); got != "debug_val" {
		t.Errorf("opt = %q, want %q", got, "debug_val")
	}
}

func TestConfigVarAppend(t *testing.T) {
	input := `
cxxflags = -Wall

config debug:
    cxxflags += -O0 -g
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, []string{"debug"})
	if err != nil {
		t.Fatal(err)
	}

	if got := vars.Get("cxxflags"); got != "-Wall -O0 -g" {
		t.Errorf("cxxflags = %q, want %q", got, "-Wall -O0 -g")
	}
}

func TestConfigComposition(t *testing.T) {
	input := `
cxxflags = -Wall
ldflags =

config debug:
    cxxflags += -O0
    ldflags += -g

config asan:
    cxxflags += -fsanitize=address
    ldflags += -fsanitize=address
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, []string{"debug", "asan"})
	if err != nil {
		t.Fatal(err)
	}

	// debug applied first, then asan
	if got := vars.Get("cxxflags"); got != "-Wall -O0 -fsanitize=address" {
		t.Errorf("cxxflags = %q, want %q", got, "-Wall -O0 -fsanitize=address")
	}
	if got := vars.Get("ldflags"); got != "-g -fsanitize=address" {
		t.Errorf("ldflags = %q, want %q", got, "-g -fsanitize=address")
	}
}

func TestConfigExcludeError(t *testing.T) {
	input := `
config debug:
    excludes release

config release:
    excludes debug
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, []string{"debug", "release"})
	if err == nil {
		t.Fatal("expected error for mutually exclusive configs")
	}
	if !strings.Contains(err.Error(), "excludes") {
		t.Errorf("error = %q, expected to mention excludes", err.Error())
	}
}

func TestConfigUnknownError(t *testing.T) {
	input := `
config debug:
    cxxflags += -O0
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown config")
	}
	if !strings.Contains(err.Error(), "unknown config") {
		t.Errorf("error = %q, expected to mention unknown config", err.Error())
	}
}

func TestConfigBuildDir(t *testing.T) {
	input := `
builddir = build

config debug:
    cxxflags += -O0

config asan:
    cxxflags += -fsanitize=address
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, []string{"debug", "asan"})
	if err != nil {
		t.Fatal(err)
	}

	if got := vars.Get("builddir"); got != "build-debug-asan" {
		t.Errorf("builddir = %q, want %q", got, "build-debug-asan")
	}
}

func TestConfigPatternRule(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "foo.c"), []byte("int main() {}"), 0o644)

	cvfile := `
builddir = build

config debug:
    cxxflags += -O0

$builddir/{name}.o: src/{name}.c
    gcc -c $input -o $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	// Without config: pattern should resolve under build/
	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := graph.Resolve("build/foo.o")
	if err != nil {
		t.Fatal(err)
	}
	if rule.target != "build/foo.o" {
		t.Errorf("base target = %q, want %q", rule.target, "build/foo.o")
	}

	// With debug config: pattern should resolve under build-debug/
	vars2 := NewVars()
	graph2, err := BuildGraph(f, vars2, state, []string{"debug"})
	if err != nil {
		t.Fatal(err)
	}
	rule2, err := graph2.Resolve("build-debug/foo.o")
	if err != nil {
		t.Fatal(err)
	}
	if rule2.target != "build-debug/foo.o" {
		t.Errorf("config target = %q, want %q", rule2.target, "build-debug/foo.o")
	}

	// The base path should NOT resolve with debug config
	_, err = graph2.Resolve("build/foo.o")
	if err == nil {
		t.Error("build/foo.o should NOT resolve with debug config")
	}
}

func TestConfigRequires(t *testing.T) {
	input := `
config dist:
    requires distpkg
    csp_include = dist
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, []string{"dist"})
	if err != nil {
		t.Fatal(err)
	}

	requires := graph.ConfigRequires()
	if len(requires) != 1 || requires[0] != "distpkg" {
		t.Errorf("requires = %v, want [distpkg]", requires)
	}
}

func TestParseLoop(t *testing.T) {
	input := `
configs = debug release

for config in $configs:
    cflags_$config = -D$config
end
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.Stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(f.Stmts))
	}

	loop := f.Stmts[1].(Loop)
	if loop.Var != "config" {
		t.Errorf("var = %q, want %q", loop.Var, "config")
	}
	if loop.List != "$configs" {
		t.Errorf("list = %q, want %q", loop.List, "$configs")
	}
	if len(loop.Body) != 1 {
		t.Fatalf("expected 1 body statement, got %d", len(loop.Body))
	}
	assign := loop.Body[0].(VarAssign)
	if assign.Name != "cflags_$config" {
		t.Errorf("body var name = %q, want %q", assign.Name, "cflags_$config")
	}
}

func TestLoopVarExpansion(t *testing.T) {
	input := `
configs = debug release

for config in $configs:
    cflags_$config = -D$config
end
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := vars.Get("cflags_debug"); got != "-Ddebug" {
		t.Errorf("cflags_debug = %q, want %q", got, "-Ddebug")
	}
	if got := vars.Get("cflags_release"); got != "-Drelease" {
		t.Errorf("cflags_release = %q, want %q", got, "-Drelease")
	}
}

func TestLoopRuleGeneration(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "src.c"), []byte("int main() {}"), 0o644)

	cvfile := `
archs = x86 arm

for arch in $archs:
    build_$arch: src.c
        echo $arch > $target
end
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Both rules should be resolvable
	rule1, err := graph.Resolve("build_x86")
	if err != nil {
		t.Fatal(err)
	}
	if rule1.target != "build_x86" {
		t.Errorf("target = %q, want %q", rule1.target, "build_x86")
	}

	rule2, err := graph.Resolve("build_arm")
	if err != nil {
		t.Fatal(err)
	}
	if rule2.target != "build_arm" {
		t.Errorf("target = %q, want %q", rule2.target, "build_arm")
	}
}

func TestLoopNested(t *testing.T) {
	input := `
archs = x86 arm
configs = debug release

for arch in $archs:
    for config in $configs:
        flags_${arch}_$config = -march=$arch -D$config
    end
end
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"flags_x86_debug":   "-march=x86 -Ddebug",
		"flags_x86_release": "-march=x86 -Drelease",
		"flags_arm_debug":   "-march=arm -Ddebug",
		"flags_arm_release": "-march=arm -Drelease",
	}
	for name, want := range cases {
		if got := vars.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestLoopConditional(t *testing.T) {
	input := `
configs = debug release

for config in $configs:
    if $config == debug
        opt_$config = -O0
    else
        opt_$config = -O2
    end
end
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := vars.Get("opt_debug"); got != "-O0" {
		t.Errorf("opt_debug = %q, want %q", got, "-O0")
	}
	if got := vars.Get("opt_release"); got != "-O2" {
		t.Errorf("opt_release = %q, want %q", got, "-O2")
	}
}

func TestLoopEmptyList(t *testing.T) {
	input := `
empty =

for x in $empty:
    should_not_exist = true
end
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := vars.Get("should_not_exist"); got != "" {
		t.Errorf("should_not_exist = %q, want empty (loop should not execute)", got)
	}
}

func TestPatternPrereqMerge(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "foo.c"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "foo.h"), []byte(""), 0o644)

	cvfile := `
{name}.o: {name}.c
    cc -c $input -o $target

{name}.o: {name}.h
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	rule, err := graph.Resolve("foo.o")
	if err != nil {
		t.Fatal(err)
	}

	// Should have merged prereqs from both patterns
	if len(rule.prereqs) != 2 {
		t.Fatalf("prereqs = %v, want [foo.c foo.h]", rule.prereqs)
	}
	if rule.prereqs[0] != "foo.c" || rule.prereqs[1] != "foo.h" {
		t.Errorf("prereqs = %v, want [foo.c foo.h]", rule.prereqs)
	}

	// Should have the recipe from the first pattern
	if len(rule.recipe) != 1 {
		t.Errorf("recipe = %v, want 1 line", rule.recipe)
	}
}

func TestPatternAmbiguousRecipeError(t *testing.T) {
	cvfile := `
{name}.o: {name}.c
    cc -c $input -o $target

{name}.o: {name}.s
    as $input -o $target
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "foo.c"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "foo.s"), []byte(""), 0o644)

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = graph.Resolve("foo.o")
	if err == nil {
		t.Fatal("expected error for ambiguous pattern rules")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error = %q, want ambiguous pattern error", err.Error())
	}
}

func TestPatternMergeOrderOnly(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "foo.c"), []byte(""), 0o644)

	cvfile := `
{name}.o: {name}.c
    cc -c $input -o $target

{name}.o: | builddir
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	rule, err := graph.Resolve("foo.o")
	if err != nil {
		t.Fatal(err)
	}

	if len(rule.prereqs) != 1 || rule.prereqs[0] != "foo.c" {
		t.Errorf("prereqs = %v, want [foo.c]", rule.prereqs)
	}
	if len(rule.orderOnlyPrereqs) != 1 || rule.orderOnlyPrereqs[0] != "builddir" {
		t.Errorf("orderOnlyPrereqs = %v, want [builddir]", rule.orderOnlyPrereqs)
	}
}

func TestRecursiveDefinitionError(t *testing.T) {
	tests := []struct {
		input string
		isErr bool
	}{
		{"foo = $foo bar", true},
		{"foo = ${foo} bar", true},
		{"foo = $foobar", false},  // different variable name
		{"foo = $bar $foo", true}, // self-ref not at start
		{"foo += $foo", false},    // append is fine
		{"foo ?= $foo", false},    // conditional is fine
		{"lazy foo = $foo", true}, // lazy self-ref is recursive
	}

	for _, tt := range tests {
		_, err := Parse(strings.NewReader(tt.input))
		if tt.isErr && err == nil {
			t.Errorf("Parse(%q): expected error, got nil", tt.input)
		}
		if !tt.isErr && err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", tt.input, err)
		}
	}
}

func TestStdlibCInclude(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	os.WriteFile(filepath.Join(dir, "hello.c"), []byte("int main() { return 0; }"), 0o644)

	cvfile := `
include std/c.cv

app: hello.o
    $cc $ldflags -o $target $inputs
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// cc should be set by std/c.cv
	if got := vars.Get("cc"); got != "cc" {
		t.Errorf("cc = %q, want %q", got, "cc")
	}

	// Pattern rule from std/c.cv should resolve hello.o
	rule, err := graph.Resolve("hello.o")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.prereqs) != 1 || rule.prereqs[0] != "hello.c" {
		t.Errorf("prereqs = %v, want [hello.c]", rule.prereqs)
	}
}

func TestStdlibCxxInclude(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	cvfile := `include std/cxx.cv`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := vars.Get("cxx"); got != "c++" {
		t.Errorf("cxx = %q, want %q", got, "c++")
	}
}

func TestStdlibGoInclude(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	cvfile := `include std/go.cv`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// !build task should exist
	rule, err := graph.Resolve("build")
	if err != nil {
		t.Fatal(err)
	}
	if !rule.isTask {
		t.Error("expected build to be a task")
	}

	// !test task should exist
	rule, err = graph.Resolve("test")
	if err != nil {
		t.Fatal(err)
	}
	if !rule.isTask {
		t.Error("expected test to be a task")
	}
}

func TestStdlibOverride(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	cvfile := `
cc = clang
include std/c.cv
`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// cc should remain clang because std/c.cv uses ?=
	if got := vars.Get("cc"); got != "clang" {
		t.Errorf("cc = %q, want %q (should not be overridden by std/c.cv)", got, "clang")
	}
}

func TestLocalFileOverridesStdlib(t *testing.T) {
	dir := t.TempDir()
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Create a local std/c.cv that sets cc to something custom
	os.MkdirAll(filepath.Join(dir, "std"), 0o755)
	os.WriteFile(filepath.Join(dir, "std", "c.cv"), []byte("cc = local-cc\n"), 0o644)

	cvfile := `include std/c.cv`
	f, err := Parse(strings.NewReader(cvfile))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Local file should take priority over embedded stdlib
	if got := vars.Get("cc"); got != "local-cc" {
		t.Errorf("cc = %q, want %q (local file should override embedded)", got, "local-cc")
	}
}

// createTarball creates a .tar.gz from the given files in the directory.
func createTarball(t *testing.T, dir, name string, files []string) {
	t.Helper()
	args := append([]string{"czf", filepath.Join(dir, name), "-C", dir}, files...)
	cmd := fmt.Sprintf("tar %s", strings.Join(args, " "))
	c := exec.Command("sh", "-c", cmd)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("creating tarball: %s: %v", string(out), err)
	}
}
