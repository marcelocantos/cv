// Copyright 2026 The mk Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/marcelocantos/mk"
)

var version = "dev"

func main() {
	var (
		dir         = flag.String("C", "", "change to directory before doing anything")
		file        = flag.String("f", "mkfile", "mkfile to read")
		verbose     = flag.Bool("v", false, "verbose output")
		force       = flag.Bool("B", false, "unconditional rebuild (ignore state)")
		dryRun      = flag.Bool("n", false, "dry run (print commands without executing)")
		jobs        = flag.Int("j", -1, "parallel jobs (-1=auto, 0=unlimited)")
		why         = flag.Bool("why", false, "explain why targets are stale")
		graph       = flag.Bool("graph", false, "print dependency subgraph")
		showState   = flag.Bool("state", false, "show build database entries")
		verify      = flag.Bool("verify", false, "error on undeclared reads of in-graph targets (DESIGN.md §11)")
		complete    = flag.Bool("complete", false, "output completions (targets and configs)")
		agentsGuide = flag.Bool("help-agent", false, "print the mk agents guide")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	args := flag.Args()

	if *showVersion {
		fmt.Println("mk", version)
		return
	}

	if *agentsGuide {
		var buf bytes.Buffer
		flag.CommandLine.SetOutput(&buf)
		fmt.Fprintf(&buf, "Usage of mk:\n")
		flag.PrintDefaults()
		flag.CommandLine.SetOutput(os.Stderr)
		fmt.Print(buf.String())
		fmt.Print(mk.AgentsGuide)
		return
	}

	if *dir != "" {
		if err := os.Chdir(*dir); err != nil {
			fmt.Fprintf(os.Stderr, "mk: %s\n", err)
			os.Exit(1)
		}
	}

	if err := run(*file, *verbose, *force, *dryRun, *jobs, *why, *graph, *showState, *complete, *verify, args); err != nil {
		fmt.Fprintf(os.Stderr, "mk: %s\n", err)
		os.Exit(1)
	}
}

func run(file string, verbose, force, dryRun bool, jobs int, why, graph, showState, complete, verify bool, args []string) error {
	// Process command-line arguments: targets, configs, and variable overrides
	vars := mk.NewVars()
	var buildTargets []string
	var activeConfigs []string
	configSeen := map[string]bool{}

	for _, arg := range args {
		if name, value, ok := strings.Cut(arg, "="); ok {
			vars.Set(name, value)
			continue
		}
		// Check for target:config1+config2 syntax
		if target, configStr, ok := strings.Cut(arg, ":"); ok {
			buildTargets = append(buildTargets, target)
			for _, c := range strings.Split(configStr, "+") {
				c = strings.TrimSpace(c)
				if c != "" && !configSeen[c] {
					activeConfigs = append(activeConfigs, c)
					configSeen[c] = true
				}
			}
		} else {
			buildTargets = append(buildTargets, arg)
		}
	}

	// Config suffix for state file isolation
	configSuffix := strings.Join(activeConfigs, "-")

	// --complete: output target and config names for shell completion
	if complete {
		f, err := os.Open(file)
		if err != nil {
			return nil // silent failure for completion
		}
		defer f.Close()
		ast, err := mk.Parse(f)
		if err != nil {
			return nil
		}
		g, err := mk.BuildGraph(ast, vars, &mk.BuildState{Targets: make(map[string]*mk.TargetState)}, nil)
		if err != nil {
			return nil
		}
		for _, t := range g.Targets() {
			fmt.Println(t)
		}
		for _, c := range g.ConfigNames() {
			fmt.Println(c)
		}
		return nil
	}

	// --state only needs the build database
	if showState {
		state := mk.LoadState(configSuffix)
		if len(buildTargets) == 0 {
			return fmt.Errorf("--state requires at least one target")
		}
		for _, t := range buildTargets {
			ts := state.Targets[t]
			if ts == nil {
				fmt.Printf("no build state recorded for %q\n", t)
				continue
			}
			data, _ := json.MarshalIndent(ts, "", "  ")
			fmt.Printf("state for %q:\n%s\n", t, string(data))
		}
		return nil
	}

	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", file, err)
	}
	defer f.Close()

	ast, err := mk.Parse(f)
	if err != nil {
		return err
	}

	state := mk.LoadState(configSuffix)

	g, err := mk.BuildGraph(ast, vars, state, activeConfigs)
	if err != nil {
		return err
	}

	if len(buildTargets) == 0 {
		def := g.DefaultTarget()
		if def == "" {
			return fmt.Errorf("no targets specified and no default target")
		}
		buildTargets = []string{def}
	}

	// --why: explain why targets are stale, then exit
	if why {
		for _, t := range buildTargets {
			reasons, err := g.WhyRebuild(t)
			if err != nil {
				return err
			}
			if len(reasons) == 0 {
				fmt.Printf("%s is up to date\n", t)
			} else {
				fmt.Printf("%s needs rebuilding:\n", t)
				for _, r := range reasons {
					fmt.Printf("  - %s\n", r)
				}
			}
		}
		return nil
	}

	// --graph: print dependency subgraph as DOT, then exit
	if graph {
		return g.PrintGraph(buildTargets)
	}

	// Normal build
	exec := mk.NewExecutor(g, state, vars, &mk.ExecutorArgs{
		Verbose:      verbose,
		Force:        force,
		DryRun:       dryRun,
		Verify:       verify,
		Jobs:         jobs,
		ConfigSuffix: configSuffix,
	})

	// Build config requires targets first
	for _, req := range g.ConfigRequires() {
		if err := exec.Build(req); err != nil {
			return err
		}
	}

	// Build main targets
	for _, t := range buildTargets {
		if err := exec.Build(t); err != nil {
			return err
		}
	}

	if dryRun {
		return nil
	}
	return state.Save(configSuffix)
}
