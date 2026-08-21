# Entropy audit — cv

Date: 2026-08-22
Mode: full (entropy + hygiene)
Auditor: entropy-audit owner (this campaign)

## Executive summary

- **Snapshot:** `/Users/marcelo/work/github.com/marcelocantos/cv`, branch `master`, HEAD `e5f5219fd47fef382c8a4b2fcc5eb88dcb49d644` (`v0.10.0`, "Remove stray mk binary committed in rename PR (#5)").
- **Initial dirty state (user-owned, not touched):** ` D .mk/state.json`.
- **Scope:** whole repository (Go library `package cv`, CLI `cmd/cv`, embedded `std/*.cv`, completions, docs, `.github/workflows/release.yml`). No vendored trees. Runtime state under `.cv/` is gitignored and was not treated as source.
- **Headline mechanism:** a clean parse → graph → execute pipeline with a single content-hashed build database, undermined by the staleness policy being implemented twice and incompletely, and by DESIGN.md §11 describing a larger machine than the executor actually runs.
- **Highest-consequence findings:** declared output hashes are recorded but never consulted (ENT-001); `[scan]` runs before both dry-run and staleness (ENT-002); cyclic graphs deadlock (ENT-003); no PR/push CI and master is unprotected (ENT-004).
- **Unverified residue:** live Linux `strace` trace path (this host is darwin/arm64); Windows path handling (declared out of scope); Homebrew tap contents; whether fingerprint mode is *intended* to ignore discovered edges.

## Scope and exclusions

Included: all tracked source, tests, `std/*.cv`, `completions/`, docs, CI, `bullseye.yaml`, `cvfile`.

Named exclusions (not silent omissions):

| Path | Role | Treatment |
|------|------|-----------|
| `.cv/` | runtime build database | gitignored; not source |
| `.claude/` | local editor settings | gitignored |
| `.mk/state.json` | rename residue, still tracked in HEAD; deleted in the working tree | cited as ENT-009; not modified |

No `vendor/`, no generated protobuf/SQL, no fixture snapshots. `std/*.cv` and `agents-guide.md` are embedded via `go:embed` (`stdlib.go`) — source, not generated output.

Languages from manifests: Go (`go.mod`, `go 1.25.7`). Shell completions (`completions/cv.bash`, `completions/cv.zsh`) inspected. No Python, C/C++, Rust, SQL, or web frontend in this repo.

No `AGENTS.md` / `CLAUDE.md` in the repo.

## Commands run

Parent `go.work` at `/Users/marcelo/work/github.com/marcelocantos/go.work` lists only `./claudia` and `./jevons`. Bare `go test ./...` from this repo therefore fails with `directory prefix . does not contain modules listed in go.work`. All Go commands below used `GOWORK=off`. This is an environment limitation, not a cv-repo file.

| Command | Version / notes | Exit | Shipped path? | Result |
|---------|-----------------|------|---------------|--------|
| `git rev-parse HEAD`; `git status --porcelain=v1 -b` | git | 0 | n/a | `master...origin/master`; dirty ` D .mk/state.json` |
| `GOWORK=off go test ./...` | `go version go1.26.4 darwin/arm64` | 0 | library shipped path; **not** `cmd/cv` | `ok github.com/marcelocantos/cv`; `cmd/cv [no test files]` |
| `GOWORK=off go vet ./...` | same | 0 | auxiliary | clean |
| `GOWORK=off go test -race ./...` | same | 0 | auxiliary (race instrumented) | `ok` |
| `GOWORK=off go test -count=1 -cover ./...` | same | 0 | library shipped path | `coverage: 81.5% of statements` (`cmd/cv` 0.0%) |
| `gofmt -l .` | gofmt from go1.26.4 | 0 | auxiliary | `depfile.go` (godoc sample indent only) |
| `GOWORK=off staticcheck ./...` | staticcheck built with go1.25.0 | 1 | auxiliary, **failed** | `module requires at least go1.25.7, but Staticcheck was built with go1.25.0` |
| `go test ./...` (GOWORK inherited) | same | 1 | n/a | setup failed on parent `go.work` |
| `gh api repos/marcelocantos/cv/branches/master/protection` | gh | 1 | GitHub settings | HTTP 404 "Branch not protected" |
| `ls .github/workflows/` | | 0 | n/a | only `release.yml` |
| `test -f hygiene.yaml` | | 1 | n/a | absent |

Not run: `govulncheck` (not installed); `golangci-lint` (would add style noise without a repo config; not a declared gate); live Linux strace (wrong OS). No analyzers were installed.

## Observed architecture

### Entry points and deployable units

One CLI binary, one library package:

```
cmd/cv/main.go  →  package cv (root)
                     parse.go / ast.go
                     graph.go / pattern.go
                     exec.go / depfile.go / trace_*.go
                     state.go / vars.go / util.go
                     stdlib.go  (embed std/*.cv, agents-guide.md)
```

`cvfile` at the repo root includes `std/go.cv` (`!build`, `!test`, `!vet`) and adds `!fmt` / `!clean`. Default target is empty (all rules are tasks).

Release artifacts: `cv-${version}-{darwin/arm64,linux/amd64,linux/arm64}.tar.gz` from `.github/workflows/release.yml`, plus Homebrew tap update.

### Directional dependencies (observed)

```
CLI (cmd/cv) → cv.Parse → cv.BuildGraph → cv.NewExecutor.Build → BuildState.Save
                    │            │                │
                    │            │                ├─ HashCache / IsStale / Record
                    │            │                ├─ ParseDepfile / runTraced
                    │            │                └─ Vars.Expand / Environ
                    │            └─ stdlibFS, configs, pattern match
                    └─ AST only
```

Package graph is acyclic (two packages, one edge). No internal layering is enforced by tests or `internal/` packages; the pipeline is convention.

### Declared vs observed rules

| Rule | Where declared | Observed | Class |
|----------------------|----------|-------|
| Content-hash staleness for inputs | DESIGN.md §7, docs/why-cv.md | `IsStale` hashes prereqs | **Agree** |
| Content-hash staleness for declared outputs | DESIGN.md §7, docs/why-cv.md | `OutputHash` written, never read | **Contradicted** (ENT-001) |
| Scan is a first-class graph node | DESIGN.md §11, T1.4 | Inline `runScan` in `doBuild`, before staleness | **Contradicted** (ENT-002) |
| `-n` prints without executing | DESIGN.md §13, flag help | Recipes skipped; scans still run | **Contradicted** (ENT-002) |
| Trace on macOS (sandbox / `fs_usage`) | DESIGN.md §11 | Linux `strace` only; `trace_other.go` errors | **Contradicted** (ENT-007) |
| Per-target `[verify]` | DESIGN.md §11 syntax table | Not parsed; becomes a target name | **Contradicted** (ENT-007) |
| `[reads]` enforced by sandbox | DESIGN.md §11 | Post-hoc glob check | **Contradicted** (ENT-007) |
| `[deps: gcc]` via `std/c.cv` | DESIGN.md, STABILITY, agents-guide | Implemented; E2E tests | **Agree** |
| `--verify` flags undeclared in-graph reads | STABILITY, agents-guide, README | Implemented; tests | **Agree** |
| Zero external modules | CONTRIBUTING.md, `go.mod` | `go.mod` has no `require` | **Agree** |
| Cycle detection | none (Make has it) | Singleflight wait → deadlock | **Inferred gap** (ENT-003) |

### Public surfaces

- CLI flags in `cmd/cv/main.go`.
- Cvfile language (parse + graph).
- `.cv/state.json` schema (STABILITY.md).
- Embedded stdlib `include std/c.cv` etc.
- Exported Go API (secondary; STABILITY marks `Graph.Resolve` as Fluid).

### Cross-cutting concerns

Staleness, hashing, and the build DB live in `state.go`. Variable expansion is global (`Vars`). Discovery (depfile / scan / trace) is folded in `exec.go` after the recipe. There is no plugin, auth, or network surface in the tool itself. Recipes are `sh -c` with `set -e` and inherit the full variable environment.

## Dimension vector

| Dimension | State | Evidence summary | Change from baseline |
|---|---|---|---|
| Architecture topology | healthy | Two packages, one-way CLI→library, parse→graph→exec; no package cycles | n/a (first audit) |
| Redundancy / sources of truth | concern | Flag catalogues in six places; DESIGN.md §11 vs code vs STABILITY vs bullseye; `IsStale`/`WhyStale` already drifted | n/a |
| Change amplification | concern | New flag or staleness reason must be edited in many files; auto-vars cloned in four functions | n/a |
| Local code quality | healthy | Linear, readable, ~8k LOC including tests; zero deps; `@` prefix is a no-op (ENT-010) | n/a |
| Correctness / verification | concern | Library tests 81.5% and race-clean; output-hash, scan/dry-run, and cycles untested and wrong; `cmd/cv` 0% | n/a |
| Security / dependencies | healthy | No third-party Go modules; recipes are the threat boundary (user-authored `sh -c`); Apache-2.0 | n/a |
| Build / release / operations | concern | Tests only on tag-push; master unprotected; `go install` reports `dev`; `skip_checksum: true` | n/a |
| Documentation / governance | concern | DESIGN ahead of code; STABILITY snapshot still "v0.8.0"; bullseye T1–T1.6 still `converging`; hygiene undeclared | n/a |

## Findings

### ENT-001: Declared output content-hash is recorded and never consulted

- **Priority:** P1
- **Dimensions:** Correctness / verification; Redundancy / sources of truth
- **Status:** observed fact
- **Evidence:**
  - DESIGN.md:334 promises an "Output fingerprint. Detects targets modified outside the build."
  - docs/why-cv.md:177-181: "cv also records the hash of the target itself. This detects targets modified outside the build system … and triggers a rebuild"
  - `TargetState.OutputHash` is written in `Record` (`state.go:328-330`)
  - `IsStale` file mode only checks existence (`state.go:128-130`), then prereq set/hashes and discovered edges
  - Comment at `state.go:162-164` claims discovered-output hashing is "the same contract as the declared output's OutputHash check" — that check is not in `IsStale`
  - `OutputHash` has no readers in `*.go` except the struct field and the write site (repo grep)
  - No test asserts rebuild after a hand-edited target
- **Mechanism:** After a successful build, a user (or another tool) can overwrite the artefact. The next `cv` sees the file still exists, input hashes unchanged, and skips the recipe. Downstream targets that list it as a prereq *will* rebuild (their input hash changes) against the tampered bytes; the declared target itself is not restored.
- **Blast radius:** every file target. This is half of the content-hash sales pitch versus Make timestamps.
- **Counterevidence checked:** fingerprint mode is a separate path and *does* compare `FingerprintHash`. Discovered outputs *are* content-hashed (`discoveredOutputsStale`, `state.go:191-200`, tested in `discovered_test.go`). Input hashing is tested and works. The omission is specific to declared outputs.
- **Smallest coherent remediation:** In `IsStale`/`WhyStale` file mode, compare `cache.Hash(targets[i])` to `ts.OutputHash` (treat hash error as stale). One test: build, overwrite target, assert stale, rebuild restores content.
- **Verification:** that test on the library path; a CLI journey `cv && echo dirty > $target && cv` must rebuild.
- **Ratchet candidate:** `go test` case `TestIsStaleOutputModifiedOutsideBuild`; later a hygiene `command:` if hygiene is onboarded.

### ENT-002: `[scan]` runs before dry-run and staleness, and is not a graph node

- **Priority:** P1
- **Dimensions:** Correctness / verification; Architecture topology; Change amplification
- **Status:** observed fact
- **Evidence:**
  - DESIGN.md:712: "`[scan: <cmd>]` — Separate cheap node producing schedulable edges before the recipe"
  - DESIGN.md:632-635: "A scan node is a first-class graph node: it has its own deps … and is scheduled like any other target"
  - `doBuild` (`exec.go:148-169`) always calls `runScan` when `rule.scan != ""`, **before** `IsStale` (`exec.go:174`)
  - `executeRecipe` honours `-n` only later (`exec.go:259-264`)
  - `runScan` (`exec.go:196-221`) is `exec.Command("sh", "-c", cmdText)` with no dry-run or staleness guard
  - Tests cover parse/propagation and one cold-build interleaving (`TestScanBuildsInGraphDiscoveredDep`); none cover `-n` or an up-to-date rescan
- **Mechanism:** Two failures share one shape. (1) `cv -n` still executes scan commands. (2) Every visit of a scanned target re-runs the compiler-style pre-pass, even when the heavy recipe is up to date — the opposite of "record-and-reuse is the spine" (DESIGN.md §11). Scan is an inline side effect, not a node with its own recorded recipe/inputs.
- **Blast radius:** any rule with `[scan: …]`. Dry-run is no longer a pure print. Incremental builds pay a full analysis cost per target per invocation.
- **Counterevidence checked:** Scan-discovered in-graph deps *are* built before the heavy recipe (T1.4 interleaving works on a cold build). Depfile `[deps: gcc]` does *not* have this problem — it runs only after the recipe. The defect is specific to the scan placement.
- **Smallest coherent remediation:** Skip `runScan` when `dryRun`. For incremental: either treat scan as its own recorded node (matches the spec) or run it only when the parent is already stale-or-forced. Prefer the skip-unless-stale placement first; promoting scan to a real node is a follow-up if scheduling-through-generated-headers needs cached scan outputs.
- **Verification:** (a) `cv -n` on a `[scan:]` rule must not spawn the scan command; (b) second `cv` on an up-to-date scanned target must not re-run the scan.
- **Ratchet candidate:** tests `TestScanSkippedOnDryRun` and `TestScanSkippedWhenUpToDate`.

### ENT-003: Cyclic graphs deadlock in `Executor.Build`

- **Priority:** P1
- **Dimensions:** Correctness / verification
- **Status:** observed fact (mechanism); hang not reproduced in this run (would not terminate)
- **Evidence:**
  - `Executor.Build` (`exec.go:91-97`): if `target` is already in `building`, wait on `res.done`
  - `doBuild` (`exec.go:119-134`) waits on all prereqs via recursive `Build`
  - Repo grep for `cycle`, `deadlock`, `circular`: no matches in `*.go` or `*.md`
  - Make, ninja, and bazel all error on cycles; cv has no equivalent
- **Mechanism:** For `a: b` and `b: a`, A inserts itself into `building` and waits for B; B finds A in `building` and waits on A's `done`. Neither closes. The process hangs with no error.
- **Blast radius:** any cvfile with a cycle, including accidental self-prereqs and pattern-generated loops. Operator-visible hang, not a structured error.
- **Counterevidence checked:** multi-output singleflight is intentional and correct (`exec.go:107-110` shares one `buildResult` across co-targets). Diamond graphs are tested (`TestParallelDiamond`) and complete. The wait-on-in-flight path is only unsafe when the waiter is an ancestor, which is exactly a cycle.
- **Smallest coherent remediation:** thread a `buildingStack` (or colour) through `Build`; if `target` is on the current goroutine's stack, return `fmt.Errorf("cycle involving %q", …)` instead of waiting. Keep the singleflight map for sibling diamond dedup.
- **Verification:** `a: b` / `b: a` must error; diamond must still pass; a test must not use a timeout as the oracle (error string is the oracle).
- **Ratchet candidate:** `TestCycleSelf`, `TestCycleTwo`, kept in `go test ./...`.

### ENT-004: Tests are not a PR/push gate; `master` is unprotected

- **Priority:** P1
- **Dimensions:** Build / release / operations; Correctness / verification; Documentation / governance
- **Status:** observed fact
- **Evidence:**
  - `.github/workflows/` contains only `release.yml`
  - `release.yml:3-6` triggers solely on `push: tags: ['v*']`
  - Tests run there (`release.yml:21-22`: `go test ./...`) only at release time, `ubuntu-latest` only
  - `gh api …/branches/master/protection` → 404 "Branch not protected"
  - CONTRIBUTING.md:7-9, 26-27 document `go test ./...` and `go vet ./...` as a human checklist
- **Mechanism:** A broken PR can squash-merge to `master` with no required check. The only automated test run is the tag-driven release job, after the tag exists. macOS (this project's primary Darwin binary) and `-race` never run in CI.
- **Blast radius:** every future change, including the defects in ENT-001–ENT-003, which green local tests would catch only if someone writes the missing cases *and* remembers to run them.
- **Counterevidence checked:** library tests exist and passed in this audit (`GOWORK=off go test ./...`, `-race`). Release workflow *does* test before uploading assets. That does not gate `master`.
- **Smallest coherent remediation:** add `.github/workflows/ci.yml` on `pull_request` and `push` to `master`: `go test ./...` and `go vet ./...` on `ubuntu-latest` and `macos-latest`. Optionally `-race` on one OS. Then enable required status checks on `master`.
- **Verification:** a PR that fails `go test` cannot merge; a green push is visible in Actions.
- **Ratchet candidate:** CI job `ci.yml#test`; hygiene `ci_job` once hygiene is declared.

### ENT-005: Staleness policy has two implementations that have already drifted

- **Priority:** P2
- **Dimensions:** Redundancy / sources of truth; Change amplification; Correctness / verification
- **Status:** observed fact
- **Evidence:** `IsStale` (`state.go:96-172`) vs `WhyStale` (`state.go:207-278`). Shared: recipe hash, fingerprint, existence, prereq set, input hashes, discovered-prereq vanish/change. Divergent: `IsStale` calls `discoveredOutputsStale`; `WhyStale` does not mention discovered outputs at all. Neither consults `OutputHash` (ENT-001).
- **Mechanism:** `--why` can say "up to date" (empty reason list from `WhyRebuild` → `WhyStale`) while `Build` rebuilds because a discovered output changed, or the reverse once output-hash is added to only one side.
- **Blast radius:** `--why` diagnostics and any future staleness reason (ENT-001's fix is the next likely miss).
- **Counterevidence checked:** `TestWhyStale` (`cv_test.go:1244`) only asserts the "no previous build" path. Deliberate split of boolean vs reasons is fine; duplicated *policy* is the defect.
- **Smallest coherent remediation:** one function that returns reasons; `IsStale` is `len(reasons) > 0`. Add discovered outputs (and output hash) once.
- **Verification:** a table test where each staleness reason is asserted in both `IsStale` and `WhyStale`.
- **Ratchet candidate:** that table test.

### ENT-006: CLI surface is catalogued in six places and they disagree

- **Priority:** P2
- **Dimensions:** Redundancy / sources of truth; Change amplification; Documentation / governance
- **Status:** observed fact
- **Evidence:** authoritative flags are `cmd/cv/main.go:20-34`: `-C -f -v -B -n -j --why --graph --state --verify --complete --help-agent --version`.

  | Source | Missing vs main.go | Wrong |
  |--------|--------------------|-------|
  | README.md:167-176 | `-C`, `--help-agent`, `--version`, `--complete` | |
  | DESIGN.md:753-773 | `-C`, `--verify`, `--help-agent`, `--version`, `--complete`; `-j 0` described as "number of CPUs" | `exec.go:63-71`: `-1` = `NumCPU`, `0` = unlimited |
  | agents-guide.md:379-389 | `-C`, `--help-agent`, `--version`, `--complete` | |
  | STABILITY.md:16-30 | (complete, snapshot labelled v0.8.0) | |
  | completions/cv.bash:6 | `--verify` | |
  | completions/cv.zsh:6-17 | `--verify` | |

- **Mechanism:** Adding a flag requires coordinated edits across docs, both shells, STABILITY, and DESIGN. `--verify` (v0.9.0) already missed both completion files. DESIGN's `-j 0` semantics contradict the code an agent will implement against.
- **Blast radius:** users of tab-completion miss `--verify`; agents following DESIGN.md implement the wrong `-j` contract.
- **Counterevidence checked:** STABILITY.md is the most complete catalogue and already exists for this purpose. Completions omit `--complete` on purpose (internal).
- **Smallest coherent remediation:** treat STABILITY.md as the human catalogue; generate or test-assert completion flag lists from `flag.VisitAll` in `cmd/cv`. Fix DESIGN.md `-j` to match `ExecutorArgs`.
- **Verification:** a test that the bash/zsh flag words equal the `flag` set minus `--complete`.
- **Ratchet candidate:** that test in `cmd/cv`.

### ENT-007: DESIGN.md §11 specifies features the parser and executor do not implement

- **Priority:** P2
- **Dimensions:** Documentation / governance; Redundancy / sources of truth; Architecture topology
- **Status:** observed fact
- **Evidence:**

  | Spec claim | Code |
  |------------|------|
  | macOS trace via sandbox / `fs_usage` (DESIGN.md:616-618) | `trace_other.go:17-18` — not implemented on non-Linux; STABILITY.md:78 and agents-guide.md:156 correctly say Linux/`strace` |
  | per-target `[verify]` (DESIGN.md:717) | `parseRuleHeader` (`parse.go:427`) extracts fingerprint/deps/scan/writes/reads/keep only. `[verify]` remains in `targetStr` and becomes an extra target via `strings.Fields` (`parse.go:524`) |
  | `[reads]` "sandbox enforces it" (DESIGN.md:702-704) | post-hoc `envelopeViolations` (`exec.go:340-351`, `522-546`); no sandbox |
  | T1.6 acceptance: trace "on macOS and Linux" (`bullseye.yaml:78`) | Linux only; audit-log.md:13 honestly says "trace mode on Linux" |

- **Mechanism:** Agents and humans implementing from DESIGN.md will write `[verify]` rules that silently create a target named `[verify]`, expect macOS tracing, and expect a sandbox. STABILITY.md and agents-guide.md are closer to the binary and already disagree with DESIGN.md.
- **Blast radius:** anyone using DESIGN.md as the spec (this repo's README points there). Misparsed `[verify]` is a real graph mutation, not just missing behaviour.
- **Counterevidence checked:** `--verify` CLI flag *is* implemented and tested (`discovered_test.go` T1.3). `[deps: gcc]` E2E works. agents-guide.md is the better agent-facing doc for §11 as shipped.
- **Smallest coherent remediation:** Either implement `[verify]` as a rule flag that sets `Executor.verify` for that target, or remove it from DESIGN.md / discovered-dependencies.md and parse unknown `[…]` as an error. Mark macOS trace and sandbox as non-goals in DESIGN.md to match STABILITY. Close or split T1.6 so macOS is not still promised as current acceptance.
- **Verification:** `foo.o [verify]: foo.c` must not create a target `[verify]`; DESIGN.md grep for unimplemented annotations is empty or explicitly "not in v0.10".
- **Ratchet candidate:** parser test that unknown annotations error; later a doc-vs-STABILITY check if wanted.

### ENT-008: The shipped CLI has no tests

- **Priority:** P2
- **Dimensions:** Correctness / verification
- **Status:** observed fact
- **Evidence:** `GOWORK=off go test -cover ./...` → `cmd/cv coverage: 0.0% of statements`; `cmd/cv [no test files]`. Library tests call `NewExecutor` / `BuildGraph` / `Parse` directly (`mustBuild` in `discovered_test.go:814-824`). No test executes `cmd/cv/main.go` (`run` at `cmd/cv/main.go:68`).
- **Mechanism:** Flag wiring, `-C`, `--complete` silent failure, `--state` without opening the cvfile, config-suffix state files, and `state.Save` after a successful CLI build are untested on the shipped path. journeys.md: owner-visible product paths need a thin whole-product slice; this repo has none.
- **Blast radius:** regressions in CLI argument parsing and diagnostic short-circuits will not fail `go test ./...`.
- **Counterevidence checked:** Homebrew formula in `release.yml:55-62` runs `cv -n hello.txt` **after** a tag release, not on PRs. Library E2E (C compile) is real and valuable; it is not the binary.
- **Smallest coherent remediation:** one `cmd/cv` test (or `exec.Command` of the built binary) that writes a cvfile, runs `cv -C dir target`, and asserts the artefact and `.cv/state.json`. That is the journey.
- **Verification:** that test fails if `state.Save` is skipped or `-C` is ignored.
- **Ratchet candidate:** `go test ./cmd/cv`.

### ENT-009: mk → cv rename still has a tracked `.mk/` state file

- **Priority:** P2
- **Dimensions:** Redundancy / sources of truth; Build / release / operations
- **Status:** observed fact
- **Evidence:**
  - `git ls-files` includes `.mk/state.json` (empty `{"targets": {}}` at HEAD)
  - Working tree at audit start: ` D .mk/state.json` (user-owned; not staged here)
  - `.gitignore:1-2` ignores `/cv` and `.cv/` but not `.mk/`
  - `discovered_test.go:764` still named `TestEndToEndStdCMkAnnotation`
  - `cv_test.go:971` comment: "Create two subdirectories with mkfiles" (the files created are `cvfile`)
  - HEAD `e5f5219` already removed a stray `mk` binary from the rename PR
- **Mechanism:** A contributor without the local deletion can still have `.mk/state.json` in the tree. `.gitignore` will not prevent re-adding it. Dual state directories (`.mk/` vs `.cv/`) are a competing runtime SoT if an old binary and a new binary share a working tree.
- **Blast radius:** low for users of v0.10.0 (`stateDir = ".cv"` in `state.go:21`); high confusion during the rename window; dirty tree on this clone.
- **Counterevidence checked:** code, stdlib, module path, and default filename have been renamed. This is leftover tracking, not a second implementation.
- **Smallest coherent remediation:** stop tracking `.mk/state.json`; optionally ignore `.mk/` for a release or two. Rename the test. Do not touch the user's current working-tree deletion as part of this audit.
- **Verification:** `git ls-files '.mk/*'` is empty.
- **Ratchet candidate:** none required once untracked; `.gitignore` if the directory should stay ignored.

### ENT-010: `@` recipe prefix is specified and parsed, and does nothing

- **Priority:** P2
- **Dimensions:** Local code quality; Documentation / governance
- **Status:** observed fact
- **Evidence:** DESIGN.md:103, agents-guide.md:114, STABILITY.md:83 mark `@` as **Stable** "Silent — don't echo this line". `expandRecipe` (`exec.go:649-654`) strips `@` and only acts on `-` (`|| true`). Echoing is a whole-recipe banner under `-v` / `-n` (`exec.go:250-257`), with no per-line filter.
- **Mechanism:** Authors following Make habits or DESIGN.md will believe `@` hides a line. It never does. STABILITY calling it Stable freezes a no-op.
- **Blast radius:** cosmetic for default (non-verbose) runs, where recipes are not echoed anyway; misleading under `-v`.
- **Counterevidence checked:** `-` prefix *does* work. No test mentions `@`.
- **Smallest coherent remediation:** either implement per-line silent under `-v`/`-n`, or demote `@` in STABILITY/DESIGN to "accepted no-op / stripped for Make familiarity".
- **Verification:** with `-v`, a line prefixed `@` must not appear in the echoed recipe (if implementing); or a doc test if demoting.
- **Ratchet candidate:** a `-v` echo test if implemented.

### ENT-011: `go install` binaries always report version `dev`

- **Priority:** P2
- **Dimensions:** Build / release / operations
- **Status:** observed fact
- **Evidence:** `cmd/cv/main.go:17` `var version = "dev"`. README.md:29 documents `go install github.com/marcelocantos/cv/cmd/cv@latest` as the install path. Version is injected only in `release.yml:30` via `-X main.version=…` for tarball builds. `debug.ReadBuildInfo` is unused.
- **Mechanism:** The documented install path cannot answer `--version` with the module version. Homebrew/release tarballs can.
- **Blast radius:** anyone following README.
- **Counterevidence checked:** release ldflags work for tagged GitHub assets. Pre-1.0, not a compatibility break.
- **Smallest coherent remediation:** default `version` from `debug.ReadBuildInfo().Main.Version` when `version == "dev"`.
- **Verification:** `go install` of a tagged version prints that tag (or the pseudo-version), not `dev`.
- **Ratchet candidate:** a `cmd/cv` test that the version string is non-empty and not always `dev` when `BuildInfo` has a version.

### ENT-012: Intent ledgers still describe shipped work as in progress

- **Priority:** P2
- **Dimensions:** Documentation / governance; Redundancy / sources of truth
- **Status:** observed fact
- **Evidence:** `bullseye.yaml` T1 and T1.1–T1.6 are all `status: converging`. `docs/audit-log.md:11-13` records v0.9.0 as having bundled T1.1–T1.6. STABILITY.md:12 still says "Snapshot as of v0.8.0" at tag `v0.10.0`. T1.6 acceptance still requires macOS trace (`bullseye.yaml:78`) which is not shipped (ENT-007).
- **Mechanism:** Three authorities (bullseye, audit-log, STABILITY snapshot header) disagree on whether discovered-deps is done. The next agent will either re-implement shipped phases or skip remaining macOS work because "v0.9.0 shipped §11".
- **Blast radius:** planning and `/cv` convergence, not runtime.
- **Counterevidence checked:** code for depfile/verify/scan/writes/linux-trace exists and is tested at library level. The ledger is stale, not the depfile adapter.
- **Smallest coherent remediation:** achieve T1.1–T1.5 against current code; split T1.6 into Linux-done vs macOS-not; bump the STABILITY snapshot header to v0.10.0 and diff the table.
- **Verification:** `bullseye` frontier no longer lists shipped phases; STABILITY header matches `git describe`.
- **Ratchet candidate:** none in CI until hygiene/bullseye gates exist; this is ledger hygiene.

### ENT-013: Release supply chain skips checksums; Linux-only test in the release job

- **Priority:** P3
- **Dimensions:** Build / release / operations; Security / dependencies
- **Status:** observed fact
- **Evidence:** `release.yml:68` `skip_checksum: true`; `release.yml:13` `runs-on: ubuntu-latest` only; `release.yml:55` `target_darwin_amd64: false`. No SBOM, signing, or provenance.
- **Mechanism:** Homebrew consumers rely on the tap token path without checksum verification as configured. Darwin/amd64 is explicitly dropped. Trace code is never exercised in CI (Linux runner could, but `[deps: trace]` tests on this repo skip/error on missing platform rather than running strace).
- **Blast radius:** release consumers; Intel Macs; Linux-only features.
- **Counterevidence checked:** zero Go dependencies shrink the SBOM need. Pre-1.0. STABILITY.md:238 defers Windows native support.
- **Smallest coherent remediation:** stop skipping checksums; add `macos-latest` to a CI workflow (ENT-004). Signing/SBOM can wait for 1.0.
- **Verification:** release assets have checksums; CI log shows a macOS job.
- **Ratchet candidate:** hygiene `scanner` / `ci_job` when declared.

## Redundancy and competing sources of truth

| Fact | Authorities | Drift already seen |
|------|-------------|-------------------|
| CLI flags | `cmd/cv/main.go`, README, DESIGN §13, agents-guide, STABILITY, completions | ENT-006 |
| Staleness | `IsStale`, `WhyStale`, DESIGN §7, why-cv.md | ENT-001, ENT-005 |
| §11 feature set | DESIGN.md, agents-guide.md, STABILITY.md, bullseye.yaml, audit-log.md | ENT-007, ENT-012 |
| State directory | `.cv/` in code and gitignore; `.mk/state.json` still tracked | ENT-009 |
| Automatic variables | `expandRecipe`, `expandFingerprint`, `runScan`, `Graph.WhyRebuild` | four near-copies of `target`/`input`/`inputs`/`stem` setup (`exec.go:197-205`, `594-607`, `610-621`; `graph.go:59-64`) — not yet drifted enough for its own ENT, but the next auto-var (`$depfile` is already only in `expandRecipe`) will miss `--why` and scan |

Deliberate duplication: `trace_linux.go` / `trace_other.go` build-tagged split is the right OS boundary. `std/c.cv` vs `std/cxx.cv` are parallel language packs, not competing truths.

## Healthy structure worth retaining

- **Parse → graph → execute** with `Vars` and `BuildState` as the only shared mutable stores. Readable in one sitting; do not introduce a service/hexagonal overlay.
- **Zero Go dependencies** (`go.mod` is module path + `go 1.25.7`). Keep it.
- **`NewExecutor(..., *ExecutorArgs)`** (`exec.go:44-87`) already follows the Go companion's struct-arg rule; STABILITY.md:199 still talks as if the 7-arg form were current — update the sentence, keep the struct.
- **Hard vs soft edges** as implemented for `[deps: gcc]` (parse, fold, wholesale replace, vanished=changed) with E2E C tests (`discovered_test.go:698-762`, `764-789`). This is the product's actual differentiator and it works.
- **`embed.FS` stdlib** (`stdlib.go`) with local-file override (`graph.go:431-446`) and `?=` in `std/*.cv`.
- **Library tests** covering parse, patterns, includes, configs, loops, parallel diamonds, fingerprints, and discovery formats — 81.5% of `package cv`, race-clean in this run.
- **STABILITY.md** as a pre-1.0 surface catalogue — the right *kind* of document; it needs a snapshot bump (ENT-012), not replacement.
- **Apache-2.0 LICENSE**, README, CONTRIBUTING, `.gitignore`.

## Hygiene posture

**Hygiene posture not declared.** `hygiene.yaml` is absent. The validator was not run and was not initialized.

Informal overlay (not a held-tier claim):

| Dimension | What exists | Gap vs a typical floor-2 |
|-----------|-------------|--------------------------|
| correctness | local `go test ./...` (library) | no PR CI (ENT-004); no CLI journey (ENT-008) |
| security | no third-party Go deps | no secret scan, no `govulncheck` in CI |
| quality | `go vet` in CONTRIBUTING | not in CI; no format/lint gate |
| release | tag workflow, Homebrew | `skip_checksum`; version `dev` on `go install` |
| docs | README, DESIGN, STABILITY, agents-guide | competing catalogues (ENT-006, ENT-007) |
| governance | LICENSE, CONTRIBUTING | no CODEOWNERS, no branch protection |
| vcs | git, default `master` | unprotected `master` |

Entropy findings suitable as future hygiene items (do not write them until onboarded): ENT-004 CI job, ENT-001/002/003 tests, ENT-008 `go test ./cmd/cv`.

## Oracle coverage and residue

| Property | Decided by | Notes |
|----------|------------|-------|
| Parser/graph/exec library behaviour | shipped `go test` (81.5%) | auxiliary `-race` also green this run |
| `[deps: gcc]` header self-heal | shipped library E2E, skip if no `cc` | ran here (`cc` present) |
| CLI flag wiring, `-C`, `state.Save` | **nothing** | ENT-008 |
| Output-hash restore | **nothing** (and implementation missing) | ENT-001 |
| Dry-run does not execute scans | **nothing** (and implementation missing) | ENT-002 |
| Cycle → error | **nothing** (and implementation missing) | ENT-003 |
| Linux `strace` trace | **unverified on this host** | `TestTraceUnsupportedReturnsClearError` covers the non-Linux error path only |
| macOS trace | accepted unimplemented | ENT-007 |
| Windows paths | accepted out of scope (STABILITY.md:238) | |
| PR cannot merge red tests | **nothing** | ENT-004 |
| staticcheck | **failed** (tool older than `go 1.25.7`) | not a repo gate |
| Parent `go.work` hiding this module | environment | `GOWORK=off` required on this machine |

Owner-residue (intent, not more grepping):

1. Should fingerprint mode ignore discovered edges (current code) or compose with them?
2. Keep `@` as a Make-familiar no-op, or implement per-line silent?
3. Is macOS `[deps: trace]` still in scope for 1.0, or a documented non-goal?
4. Onboard `hygiene.yaml`, or keep posture undeclared until CI exists?

## Remediation sequence

1. **Oracles first:** CI on PR/push (ENT-004); `TestIsStaleOutputModifiedOutsideBuild` (ENT-001); `TestScanSkippedOnDryRun` / `TestScanSkippedWhenUpToDate` (ENT-002); cycle tests (ENT-003); one `cmd/cv` journey (ENT-008). Unify `IsStale`/`WhyStale` while adding output-hash (ENT-005).
2. **Converge truths:** parse unknown annotations as errors or implement `[verify]` (ENT-007); fix DESIGN.md `-j` and §11 claims that contradict STABILITY/agents-guide; generate or test completion flags (ENT-006); achieve or split bullseye T1.* and bump the STABILITY snapshot (ENT-012); drop tracked `.mk/state.json` (ENT-009).
3. **Remove residue only after tests exist:** skip scan on dry-run/up-to-date; cycle error; `ReadBuildInfo` version (ENT-011); `@` decision (ENT-010).
4. **Ratchet:** required CI checks; then, if requested, `hygiene.yaml` with `ci_job` evidence. Do not invent hygiene in this audit.
5. **Re-audit** against this file's finding IDs and the same commands (`GOWORK=off go test -cover ./...`, presence of a non-release CI workflow).
