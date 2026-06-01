// Copyright 2026 The cv Authors
// SPDX-License-Identifier: Apache-2.0

package cv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const stateDir = ".cv"

// StateFile returns the state file path for the given config suffix.
// Empty suffix uses the base state file.
func StateFile(configSuffix string) string {
	if configSuffix == "" {
		return filepath.Join(stateDir, "state.json")
	}
	return filepath.Join(stateDir, "state-"+configSuffix+".json")
}

// BuildState tracks build artifacts for content-based staleness detection.
type BuildState struct {
	mu      sync.RWMutex
	Targets map[string]*TargetState `json:"targets"`
}

// TargetState records the state of a target at its last successful build.
type TargetState struct {
	RecipeHash      string            `json:"recipe_hash"`
	InputHashes     map[string]string `json:"input_hashes"` // prereq path → content hash
	OutputHash      string            `json:"output_hash"`
	FingerprintHash string            `json:"fingerprint_hash,omitempty"` // hash of fingerprint command output
	Prereqs         []string          `json:"prereqs"`

	// Discovered prerequisites — soft edges learned from the recipe's
	// actual reads (e.g., a depfile). Tracked only for staleness, never
	// for scheduling: a vanished member counts as "changed" rather than
	// "missing input," and the set is wholesale-replaced on each
	// successful run. See DESIGN.md §11.
	DiscoveredPrereqs     []string          `json:"discovered_prereqs,omitempty"`
	DiscoveredInputHashes map[string]string `json:"discovered_input_hashes,omitempty"`

	// Discovered outputs — paths the recipe wrote that weren't statically
	// declared (DESIGN.md §11 dynamic outputs). Stored only when the rule
	// has a [writes: …] annotation.
	DiscoveredOutputs      []string          `json:"discovered_outputs,omitempty"`
	DiscoveredOutputHashes map[string]string `json:"discovered_output_hashes,omitempty"`
}

func LoadState(configSuffix string) *BuildState {
	s := &BuildState{Targets: make(map[string]*TargetState)}
	data, err := os.ReadFile(StateFile(configSuffix))
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, s)
	if s.Targets == nil {
		s.Targets = make(map[string]*TargetState)
	}
	return s
}

func (s *BuildState) Save(configSuffix string) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(StateFile(configSuffix), data, 0o644)
}

// GetTarget returns the recorded state for a target, or nil if not found.
func (s *BuildState) GetTarget(name string) *TargetState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Targets[name]
}

// IsStale determines if any of the targets need rebuilding.
// Only normal prereqs (not order-only) affect staleness.
// If fingerprint is non-empty, it is a shell command whose output replaces
// the file-stat check for the target.
func (s *BuildState) IsStale(targets []string, prereqs []string, recipeText, fingerprint string, cache *HashCache) bool {
	// Snapshot state under read lock, then release before I/O
	s.mu.RLock()
	snapshots := make([]*TargetState, len(targets))
	for i, t := range targets {
		snapshots[i] = s.Targets[t]
	}
	s.mu.RUnlock()

	for i, ts := range snapshots {
		if ts == nil {
			return true
		}

		// Check recipe changed
		rh := hashString(recipeText)
		if ts.RecipeHash != rh {
			return true
		}

		if fingerprint != "" {
			// Fingerprint mode: the fingerprint command output replaces
			// both target-file and prerequisite-hash checks.
			fph, err := runFingerprint(fingerprint)
			if err != nil {
				return true
			}
			if ts.FingerprintHash != fph {
				return true
			}
		} else {
			// File mode: check target exists and prereq hashes.
			if _, err := os.Stat(targets[i]); os.IsNotExist(err) {
				return true
			}

			// Check prerequisite set changed
			sortedPrereqs := make([]string, len(prereqs))
			copy(sortedPrereqs, prereqs)
			sort.Strings(sortedPrereqs)
			sortedOld := make([]string, len(ts.Prereqs))
			copy(sortedOld, ts.Prereqs)
			sort.Strings(sortedOld)
			if !stringSliceEqual(sortedPrereqs, sortedOld) {
				return true
			}

			// Check input content hashes
			for _, p := range prereqs {
				h, err := cache.Hash(p)
				if err != nil {
					return true
				}
				if ts.InputHashes[p] != h {
					return true
				}
			}

			// Check discovered prerequisites (DESIGN.md §11). A discovered
			// path that has vanished counts as "changed" — staleness, never
			// "missing input" — and triggers a rebuild that records a fresh
			// set without the vanished file. Self-healing.
			if discoveredStale(ts, cache) {
				return true
			}

			// Check discovered outputs: if any are missing or modified
			// outside the build, the target is stale (same contract as
			// the declared output's OutputHash check).
			if discoveredOutputsStale(ts, cache) {
				return true
			}
		}
	}

	return false
}

// discoveredStale returns true if any recorded discovered prerequisite has
// vanished or changed content. The recorded set is the floor of what the
// recipe last actually read; we never schedule on it, only invalidate.
func discoveredStale(ts *TargetState, cache *HashCache) bool {
	for p, recordedHash := range ts.DiscoveredInputHashes {
		h, err := cache.Hash(p)
		if err != nil {
			// Vanished or unreadable: treat as changed, not as error.
			return true
		}
		if h != recordedHash {
			return true
		}
	}
	return false
}

// discoveredOutputsStale returns true if any recorded discovered output
// is missing or has been modified outside the build.
func discoveredOutputsStale(ts *TargetState, cache *HashCache) bool {
	for p, recordedHash := range ts.DiscoveredOutputHashes {
		h, err := cache.Hash(p)
		if err != nil {
			return true
		}
		if h != recordedHash {
			return true
		}
	}
	return false
}

// WhyStale returns human-readable reasons why any of the targets are stale.
func (s *BuildState) WhyStale(targets []string, prereqs []string, recipeText, fingerprint string, cache *HashCache) []string {
	s.mu.RLock()
	snapshots := make([]*TargetState, len(targets))
	for i, t := range targets {
		snapshots[i] = s.Targets[t]
	}
	s.mu.RUnlock()

	var reasons []string

	for i, ts := range snapshots {
		target := targets[i]
		if ts == nil {
			reasons = append(reasons, fmt.Sprintf("%s: no previous build recorded", target))
			continue
		}

		rh := hashString(recipeText)
		if ts.RecipeHash != rh {
			reasons = append(reasons, "recipe has changed")
		}

		if fingerprint != "" {
			fph, err := runFingerprint(fingerprint)
			if err != nil {
				reasons = append(reasons, fmt.Sprintf("%s: fingerprint command failed: %v", target, err))
			} else if ts.FingerprintHash != fph {
				reasons = append(reasons, fmt.Sprintf("%s: fingerprint has changed", target))
			}
		} else {
			if _, err := os.Stat(target); os.IsNotExist(err) {
				reasons = append(reasons, fmt.Sprintf("%s: target file does not exist", target))
			}

			sortedPrereqs := make([]string, len(prereqs))
			copy(sortedPrereqs, prereqs)
			sort.Strings(sortedPrereqs)
			sortedOld := make([]string, len(ts.Prereqs))
			copy(sortedOld, ts.Prereqs)
			sort.Strings(sortedOld)
			if !stringSliceEqual(sortedPrereqs, sortedOld) {
				reasons = append(reasons, "prerequisite set has changed")
			}

			for _, p := range prereqs {
				h, err := cache.Hash(p)
				if err != nil {
					reasons = append(reasons, fmt.Sprintf("cannot hash prerequisite %q: %v", p, err))
					continue
				}
				if ts.InputHashes[p] != h {
					reasons = append(reasons, fmt.Sprintf("prerequisite %q has changed", p))
				}
			}

			// Discovered prerequisites — vanished and changed are both
			// staleness triggers (DESIGN.md §11).
			for p, recordedHash := range ts.DiscoveredInputHashes {
				h, err := cache.Hash(p)
				if err != nil {
					reasons = append(reasons, fmt.Sprintf("discovered prerequisite %q is gone", p))
					continue
				}
				if h != recordedHash {
					reasons = append(reasons, fmt.Sprintf("discovered prerequisite %q has changed", p))
				}
			}
		}
	}

	return reasons
}

// Record records a successful build for all targets.
//
// discovered is the set of prerequisites the recipe actually read, learned
// post-run (e.g., from a depfile). discoveredOutputs is the set of outputs
// the recipe wrote beyond the declared targets ([writes: …]). Both are
// recorded wholesale — never unioned with prior state — so the recorded
// sets always reflect the most recent run's actual reads/writes
// (DESIGN.md §11).
func (s *BuildState) Record(targets []string, prereqs []string, recipeText, fingerprint string, discovered, discoveredOutputs []string, cache *HashCache) {
	// Build TargetState objects (I/O: hashing) without holding the lock.
	states := make(map[string]*TargetState, len(targets))
	for _, target := range targets {
		ts := &TargetState{
			RecipeHash:  hashString(recipeText),
			InputHashes: make(map[string]string),
			Prereqs:     prereqs,
		}
		for _, p := range prereqs {
			h, err := cache.Hash(p)
			if err == nil {
				ts.InputHashes[p] = h
			}
		}
		if len(discovered) > 0 {
			ts.DiscoveredPrereqs = discovered
			ts.DiscoveredInputHashes = make(map[string]string, len(discovered))
			for _, p := range discovered {
				h, err := cache.Hash(p)
				if err == nil {
					ts.DiscoveredInputHashes[p] = h
				}
			}
		}
		if len(discoveredOutputs) > 0 {
			ts.DiscoveredOutputs = discoveredOutputs
			ts.DiscoveredOutputHashes = make(map[string]string, len(discoveredOutputs))
			for _, p := range discoveredOutputs {
				h, err := cache.Hash(p)
				if err == nil {
					ts.DiscoveredOutputHashes[p] = h
				}
			}
		}
		if fingerprint != "" {
			if fph, err := runFingerprint(fingerprint); err == nil {
				ts.FingerprintHash = fph
			}
		} else {
			if h, err := cache.Hash(target); err == nil {
				ts.OutputHash = h
			}
		}
		states[target] = ts
	}

	// Write to map under lock.
	s.mu.Lock()
	for target, ts := range states {
		s.Targets[target] = ts
	}
	s.mu.Unlock()
}

// runFingerprint executes the fingerprint command and returns the hash of its output.
func runFingerprint(command string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("fingerprint command %q: %w", command, err)
	}
	return hashString(out.String()), nil
}

// HashCache caches file content hashes using (path, mtime, size) as cache key.
// Thread-safe for concurrent use.
type HashCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	mtime time.Time
	size  int64
	hash  string
}

func NewHashCache() *HashCache {
	return &HashCache{entries: make(map[string]cacheEntry)}
}

// Hash returns the content hash of the file at path, using the cache
// when the file's mtime and size haven't changed.
func (c *HashCache) Hash(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	mtime := info.ModTime()
	size := info.Size()

	c.mu.Lock()
	if e, ok := c.entries[path]; ok && e.mtime.Equal(mtime) && e.size == size {
		c.mu.Unlock()
		return e.hash, nil
	}
	c.mu.Unlock()

	h, err := hashFile(path)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.entries[path] = cacheEntry{mtime: mtime, size: size, hash: h}
	c.mu.Unlock()

	return h, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CleanPath normalizes paths for consistent state tracking.
func CleanPath(p string) string {
	return filepath.Clean(p)
}
