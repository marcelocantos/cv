// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Executor runs build recipes.
type Executor struct {
	graph        *Graph
	state        *BuildState
	vars         *Vars
	verbose      bool
	force        bool   // -B: unconditional rebuild
	dryRun       bool   // -n: print commands without executing
	jobs         int    // max concurrent recipes (0 = unlimited)
	verify       bool   // --verify: error on undeclared reads of in-graph targets
	configSuffix string // "" or e.g. "debug-asan"; partitions depfile and state paths

	mu       sync.Mutex
	building map[string]*buildResult // singleflight dedup
	sem      chan struct{}           // recipe concurrency limiter; nil = unlimited
	outputMu sync.Mutex              // serializes buffered output flushes
	cache    *HashCache              // file content hash cache
}

// buildResult tracks the in-progress or completed build of a target.
// Multiple targets from the same multi-output rule share one buildResult.
type buildResult struct {
	done chan struct{}
	err  error
}

// ExecutorArgs configures a new Executor. All fields are optional; the
// zero value is a reasonable serial-debug mode (one job at a time, no
// extras). `Jobs == -1` (the conventional "auto") is mapped to NumCPU.
type ExecutorArgs struct {
	Verbose      bool
	Force        bool   // -B: unconditional rebuild
	DryRun       bool   // -n: print commands without executing
	Verify       bool   // --verify: error on undeclared in-graph reads
	Jobs         int    // 0 = unlimited, -1 = NumCPU
	ConfigSuffix string // partitions depfile/state paths
}

// NewExecutor constructs an Executor. Pass nil for args to take all
// zero-value defaults.
func NewExecutor(graph *Graph, state *BuildState, vars *Vars, args *ExecutorArgs) *Executor {
	if args == nil {
		args = &ExecutorArgs{}
	}
	jobs := args.Jobs
	if jobs < 0 {
		jobs = runtime.NumCPU()
	}

	var sem chan struct{}
	if jobs > 0 {
		sem = make(chan struct{}, jobs)
	}
	// jobs == 0: sem stays nil → unlimited concurrency

	return &Executor{
		graph:        graph,
		state:        state,
		vars:         vars,
		verbose:      args.Verbose,
		force:        args.Force,
		dryRun:       args.DryRun,
		jobs:         jobs,
		verify:       args.Verify,
		configSuffix: args.ConfigSuffix,
		building:     make(map[string]*buildResult),
		sem:          sem,
		cache:        NewHashCache(),
	}
}

// Build builds the given target and all its dependencies.
// Safe to call concurrently from multiple goroutines.
func (e *Executor) Build(target string) error {
	e.mu.Lock()
	if res, ok := e.building[target]; ok {
		e.mu.Unlock()
		<-res.done
		return res.err
	}

	// Resolve rule under lock to discover co-targets for multi-output dedup.
	// Graph.Resolve is read-only and safe to call here.
	rule, err := e.graph.Resolve(target)
	if err != nil {
		e.mu.Unlock()
		return err
	}

	res := &buildResult{done: make(chan struct{})}
	for _, t := range rule.targets {
		e.building[t] = res
	}
	e.mu.Unlock()

	err = e.doBuild(target, rule)
	res.err = err
	close(res.done)
	return err
}

func (e *Executor) doBuild(target string, rule *resolvedRule) error {
	// Build all prerequisites concurrently
	allPrereqs := make([]string, 0, len(rule.prereqs)+len(rule.orderOnlyPrereqs))
	allPrereqs = append(allPrereqs, rule.prereqs...)
	allPrereqs = append(allPrereqs, rule.orderOnlyPrereqs...)

	errs := make([]error, len(allPrereqs))
	var wg sync.WaitGroup
	for i, p := range allPrereqs {
		wg.Add(1)
		go func(idx int, prereq string) {
			defer wg.Done()
			errs[idx] = e.Build(prereq)
		}(i, p)
	}
	wg.Wait()

	// Check for prereq errors
	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("building %q for %q: %w", allPrereqs[i], target, err)
		}
	}

	// No recipe = leaf node or prerequisite-only rule
	if len(rule.recipe) == 0 {
		return nil
	}

	// Scan pre-pass (DESIGN.md §11.4): if this rule declares a [scan: …]
	// cheap analysis command, run it and build any in-graph paths it
	// discovers before checking staleness or running the heavy recipe.
	// Discovered paths are folded into the soft-edge set so they also
	// invalidate on the next run.
	var scanDiscovered []string
	if !rule.isTask && rule.scan != "" {
		var serr error
		scanDiscovered, serr = e.runScan(rule)
		if serr != nil {
			return fmt.Errorf("scan for %q: %w", rule.target, serr)
		}
		// Build any in-graph paths the scan discovered, before the heavy
		// recipe runs. Leaf paths need no build.
		for _, p := range scanDiscovered {
			if e.graph != nil && e.graph.HasRuleFor(p) {
				if berr := e.Build(p); berr != nil {
					return fmt.Errorf("scan-discovered %q for %q: %w", p, rule.target, berr)
				}
			}
		}
	}

	// Check staleness (only normal prereqs affect staleness)
	recipeText := e.expandRecipe(rule)
	fingerprint := e.expandFingerprint(rule)
	if !rule.isTask && !e.force && !e.state.IsStale(rule.targets, rule.prereqs, recipeText, fingerprint, e.cache) {
		if e.verbose {
			e.outputMu.Lock()
			fmt.Fprintf(os.Stderr, "cv: %q is up to date\n", rule.target)
			e.outputMu.Unlock()
		}
		return nil
	}

	// Acquire semaphore slot to limit concurrent recipes
	if e.sem != nil {
		e.sem <- struct{}{}
		defer func() { <-e.sem }()
	}

	return e.executeRecipe(rule, recipeText, fingerprint, scanDiscovered)
}

// runScan executes the rule's [scan: …] command and parses its stdout in
// the configured format. The output is the cheap pre-pass equivalent of
// what the heavy recipe will discover; it is used to schedule in-graph
// reads before the heavy work runs.
func (e *Executor) runScan(rule *resolvedRule) ([]string, error) {
	vars := e.vars.Clone()
	vars.Set("target", rule.target)
	if len(rule.prereqs) > 0 {
		vars.Set("input", rule.prereqs[0])
	}
	vars.Set("inputs", strings.Join(rule.prereqs, " "))
	if rule.stem != "" {
		vars.Set("stem", rule.stem)
	}
	cmdText := vars.Expand(rule.scan)

	cmd := exec.Command("sh", "-c", cmdText)
	cmd.Env = e.vars.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scan command failed: %w; stderr: %s", err, stderr.String())
	}

	format := rule.scanFormat
	if format == "" {
		format = "gcc"
	}
	return parseDepfileBytes(stdout.Bytes(), format)
}

func (e *Executor) executeRecipe(rule *resolvedRule, recipeText, fingerprint string, scanDiscovered []string) error {
	// Auto-create parent directories for all targets
	if !rule.isTask {
		for _, t := range rule.targets {
			dir := filepath.Dir(t)
			if dir != "." && dir != "" {
				if !e.dryRun {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						return fmt.Errorf("creating directory %q: %w", dir, err)
					}
				}
			}
		}
	}

	// Allocate and prepare the depfile directory if this rule discovers deps.
	depfilePath := e.depfilePathFor(rule)
	if depfilePath != "" && !e.dryRun {
		if err := os.MkdirAll(filepath.Dir(depfilePath), 0o755); err != nil {
			return fmt.Errorf("creating depfile dir: %w", err)
		}
		// Remove any stale depfile from a prior aborted run; the recipe will
		// recreate it.
		_ = os.Remove(depfilePath)
	}

	// Build banner
	var banner strings.Builder
	fmt.Fprintf(&banner, "cv: building %q\n", rule.target)
	if e.verbose || e.dryRun {
		for _, line := range strings.Split(recipeText, "\n") {
			fmt.Fprintf(&banner, "  %s\n", line)
		}
	}

	if e.dryRun {
		e.outputMu.Lock()
		fmt.Fprint(os.Stderr, banner.String())
		e.outputMu.Unlock()
		return nil
	}

	// Determine output mode: serial streams directly, parallel buffers
	serial := e.sem != nil && cap(e.sem) == 1
	var stdout, stderr io.Writer
	var outBuf, errBuf bytes.Buffer

	if serial {
		// Serial mode: stream banner and output directly
		e.outputMu.Lock()
		fmt.Fprint(os.Stderr, banner.String())
		e.outputMu.Unlock()
		stdout = os.Stdout
		stderr = os.Stderr
	} else {
		// Parallel mode: buffer output, flush atomically on completion
		stdout = &outBuf
		stderr = &errBuf
	}

	// Execute recipe — either directly or under trace, depending on
	// whether the rule asked for traced reads or writes.
	wantTrace := rule.depsFormat == "trace" || rule.writes == "trace"
	var tracedReads, tracedWrites []string
	var err error
	if wantTrace {
		if ok, msg := traceSupported(); !ok {
			return fmt.Errorf("trace mode requested by %q: %s", rule.target, msg)
		}
		tracedReads, tracedWrites, err = runTraced(recipeText, stdout, stderr, e.vars.Environ())
	} else {
		fullScript := "set -e\n" + recipeText
		cmd := exec.Command("sh", "-c", fullScript)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Env = e.vars.Environ()
		err = cmd.Run()
	}

	if !serial {
		// Flush buffered output atomically
		e.outputMu.Lock()
		fmt.Fprint(os.Stderr, banner.String())
		outBuf.WriteTo(os.Stdout)
		errBuf.WriteTo(os.Stderr)
		e.outputMu.Unlock()
	}

	if err != nil {
		// Delete partial output on failure (for file targets), unless [keep]
		if !rule.isTask && !rule.keep {
			for _, t := range rule.targets {
				os.Remove(t)
			}
		}
		return fmt.Errorf("recipe for %q failed: %w", rule.target, err)
	}

	// Fold discovered prerequisites (DESIGN.md §11). Sources:
	//   - depfile (depsFormat ∈ {gcc, makefile, msvc, json, lines})
	//   - traced reads (depsFormat == "trace")
	// Soft edges are invalidation-only — never scheduling.
	var discovered []string
	if depfilePath != "" {
		paths, derr := ParseDepfile(depfilePath, rule.depsFormat)
		if derr != nil {
			return fmt.Errorf("parsing depfile %q for %q: %w", depfilePath, rule.target, derr)
		}
		discovered = filterDiscovered(paths, rule)
		// cv owns the depfile; remove it after folding into the DB.
		_ = os.Remove(depfilePath)
	}
	if rule.depsFormat == "trace" && len(tracedReads) > 0 {
		discovered = mergeDiscovered(discovered, filterDiscovered(tracedReads, rule))
	}

	// Envelope verification (DESIGN.md §11): if [reads: <glob>…] is
	// declared, every discovered read must match at least one glob.
	if rule.reads != "" && len(discovered) > 0 {
		if outside := envelopeViolations(rule.reads, discovered); len(outside) > 0 {
			msg := fmt.Sprintf("recipe for %q read paths outside its declared envelope: %s", rule.target, strings.Join(outside, ", "))
			if e.verify {
				return fmt.Errorf("%s (--verify)", msg)
			}
			e.outputMu.Lock()
			fmt.Fprintf(os.Stderr, "cv: warning: %s\n", msg)
			e.outputMu.Unlock()
		}
	}

	// Verification: a discovered read whose path is itself an
	// in-graph target this build can produce, but is not declared
	// as a prerequisite, is a latent ordering race (DESIGN.md §11).
	if len(discovered) > 0 {
		if violations := e.verifyDiscovered(rule, discovered); len(violations) > 0 {
			msg := fmt.Sprintf("recipe for %q read in-graph targets without declaring them as prereqs: %s", rule.target, strings.Join(violations, ", "))
			if e.verify {
				return fmt.Errorf("%s (--verify)", msg)
			}
			e.outputMu.Lock()
			fmt.Fprintf(os.Stderr, "cv: warning: %s\n", msg)
			e.outputMu.Unlock()
		}
	}

	// Union scan-discovered paths into the recorded discovered set. Both
	// flow from the recipe's actual reads (one pre-pass cheap, one post-pass
	// exact); their union is what should invalidate next time.
	if len(scanDiscovered) > 0 {
		discovered = mergeDiscovered(discovered, filterDiscovered(scanDiscovered, rule))
	}

	// Dynamic outputs (DESIGN.md §11): if the rule declares [writes: …],
	// read the producer-emitted manifest (or traced writes) and record
	// them as the discovered output set.
	var discoveredOutputs []string
	if rule.writes == "trace" {
		discoveredOutputs = filterDiscoveredOutputs(tracedWrites, rule)
	} else if rule.writes != "" {
		var werr error
		discoveredOutputs, werr = readWrites(rule)
		if werr != nil {
			return fmt.Errorf("reading writes manifest for %q: %w", rule.target, werr)
		}
	}

	// Record successful build for all outputs
	if !rule.isTask {
		e.state.Record(rule.targets, rule.prereqs, recipeText, fingerprint, discovered, discoveredOutputs, e.cache)
	}

	return nil
}

// readWrites parses the rule's [writes: <spec>] annotation and reads the
// list of dynamic outputs the recipe produced.
//
// Supported specs:
//
//   - "manifest <path>": the recipe writes a newline-separated list of
//     output paths to <path>. Most flexible, no execution observation.
//
// "trace" is reserved for the future tracing mode and currently rejected.
func readWrites(rule *resolvedRule) ([]string, error) {
	spec := strings.TrimSpace(rule.writes)
	kind, rest, _ := strings.Cut(spec, " ")
	switch kind {
	case "manifest":
		path := strings.TrimSpace(rest)
		if path == "" {
			return nil, fmt.Errorf("[writes: manifest …] requires a path")
		}
		return parseLinesDepfile(readFileOrEmpty(path)), nil
	case "trace":
		return nil, fmt.Errorf("[writes: trace] not implemented yet")
	default:
		return nil, fmt.Errorf("unknown [writes: …] kind %q", kind)
	}
}

func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// mergeDiscovered merges two discovered-path lists, deduplicating in
// first-seen order.
func mergeDiscovered(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, p := range a {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range b {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// depfilePathFor returns the path cv allocates for this rule's depfile, or
// "" if the rule has no [deps: …] annotation. The path mirrors the target
// name under .cv/deps/[config/] so it is inspectable when debugging.
func (e *Executor) depfilePathFor(rule *resolvedRule) string {
	if rule.depsFormat == "" || rule.isTask {
		return ""
	}
	// Use the primary target name; for multi-output rules all outputs share
	// the same recipe and therefore the same depfile.
	base := filepath.Clean(rule.target)
	// Defensive: refuse paths that would escape .cv/deps via .. or absolute
	// paths. Fall back to a flattened name in that case.
	if filepath.IsAbs(base) || strings.HasPrefix(base, "..") {
		base = strings.ReplaceAll(strings.ReplaceAll(base, string(filepath.Separator), "_"), "..", "_")
	}
	parts := []string{stateDir, "deps"}
	if e.configSuffix != "" {
		parts = append(parts, e.configSuffix)
	}
	parts = append(parts, base+".d")
	return filepath.Join(parts...)
}

// verifyDiscovered returns paths in `discovered` that are themselves
// in-graph targets — i.e., paths some rule can produce — but were not
// declared as prereqs on this rule. By the time we reach this check the
// discovered set has already had declared prereqs filtered out, so any
// match here is genuinely undeclared.
func (e *Executor) verifyDiscovered(rule *resolvedRule, discovered []string) []string {
	if e.graph == nil || len(discovered) == 0 {
		return nil
	}
	var bad []string
	for _, p := range discovered {
		if e.graph.HasRuleFor(p) {
			bad = append(bad, p)
		}
	}
	return bad
}

// filterDiscoveredOutputs drops paths that are already declared as
// targets of this rule (those are tracked by the normal output-hash
// machinery, not the discovered-output set).
func filterDiscoveredOutputs(paths []string, rule *resolvedRule) []string {
	if len(paths) == 0 {
		return nil
	}
	declared := make(map[string]bool, len(rule.targets))
	for _, t := range rule.targets {
		declared[filepath.Clean(t)] = true
	}
	out := paths[:0]
	for _, p := range paths {
		if declared[p] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// envelopeViolations returns the subset of `discovered` paths that
// match none of the globs in `envelope` (whitespace-separated).
func envelopeViolations(envelope string, discovered []string) []string {
	globs := strings.Fields(envelope)
	if len(globs) == 0 {
		return nil
	}
	var bad []string
	for _, p := range discovered {
		matched := false
		for _, g := range globs {
			if ok, _ := filepath.Match(g, p); ok {
				matched = true
				break
			}
			// Handle simple ** prefix/suffix globs by doubleStarMatch.
			if doubleStarMatch(g, p) {
				matched = true
				break
			}
		}
		if !matched {
			bad = append(bad, p)
		}
	}
	return bad
}

// doubleStarMatch supports the common "dir/**" recursive-glob shape
// that filepath.Match doesn't handle natively.
func doubleStarMatch(pattern, path string) bool {
	const star = "**"
	if !strings.Contains(pattern, star) {
		return false
	}
	prefix, suffix, _ := strings.Cut(pattern, star)
	prefix = strings.TrimRight(prefix, "/")
	suffix = strings.TrimLeft(suffix, "/")
	if prefix != "" && !strings.HasPrefix(path, prefix) {
		return false
	}
	if suffix != "" {
		ok, _ := filepath.Match("*"+suffix, filepath.Base(path))
		return ok
	}
	return true
}

// filterDiscovered drops paths that are already declared prereqs (declared
// edges are hard and tracked separately) and the rule's own outputs.
func filterDiscovered(paths []string, rule *resolvedRule) []string {
	if len(paths) == 0 {
		return nil
	}
	declared := make(map[string]bool, len(rule.prereqs)+len(rule.orderOnlyPrereqs)+len(rule.targets))
	for _, p := range rule.prereqs {
		declared[filepath.Clean(p)] = true
	}
	for _, p := range rule.orderOnlyPrereqs {
		declared[filepath.Clean(p)] = true
	}
	for _, t := range rule.targets {
		declared[filepath.Clean(t)] = true
	}
	out := paths[:0]
	for _, p := range paths {
		if declared[p] {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (e *Executor) expandFingerprint(rule *resolvedRule) string {
	if rule.fingerprint == "" {
		return ""
	}
	vars := e.vars.Clone()
	vars.Set("target", rule.target)
	if len(rule.prereqs) > 0 {
		vars.Set("input", rule.prereqs[0])
	}
	vars.Set("inputs", strings.Join(rule.prereqs, " "))
	if rule.stem != "" {
		vars.Set("stem", rule.stem)
	}
	return vars.Expand(rule.fingerprint)
}

func (e *Executor) expandRecipe(rule *resolvedRule) string {
	vars := e.vars.Clone()
	vars.Set("target", rule.target)
	if len(rule.prereqs) > 0 {
		vars.Set("input", rule.prereqs[0])
	}
	vars.Set("inputs", strings.Join(rule.prereqs, " "))

	// Set stem if available from pattern match
	if rule.stem != "" {
		vars.Set("stem", rule.stem)
	}

	// $depfile — set when the rule has a [deps: …] annotation so the recipe
	// can hand the path to its compiler (e.g., gcc -MF $depfile). The path
	// is partitioned by config to match the state file (DESIGN.md §11).
	if depfile := e.depfilePathFor(rule); depfile != "" {
		vars.Set("depfile", depfile)
	}

	// Find changed prerequisites (only normal prereqs)
	var changed []string
	ts := e.state.GetTarget(rule.target)
	for _, p := range rule.prereqs {
		if ts == nil {
			changed = append(changed, p)
			continue
		}
		h, err := e.cache.Hash(p)
		if err != nil || ts.InputHashes[p] != h {
			changed = append(changed, p)
		}
	}
	vars.Set("changed", strings.Join(changed, " "))

	var lines []string
	for _, line := range rule.recipe {
		ignoreErr := false
		l := line
		for len(l) > 0 && (l[0] == '@' || l[0] == '-') {
			if l[0] == '-' {
				ignoreErr = true
			}
			l = l[1:]
		}

		expanded := vars.Expand(l)
		if ignoreErr {
			expanded += " || true"
		}
		lines = append(lines, expanded)
	}

	return strings.Join(lines, "\n")
}
