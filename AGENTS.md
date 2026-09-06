cv — a Make-model build tool with content-hashed staleness, written in Go as a
single zero-dependency package plus one CLI binary.

# Agent guide to the cv repository

This file is the canonical instruction file for agentic coding tools working
**on cv itself**. For writing *cvfiles* (using cv), read
[`agents-guide.md`](agents-guide.md) — that file is embedded in the binary and
printed by `cv --help-agent`.

## Layout

| Path | Role |
|------|------|
| `parse.go` | cvfile → AST. `Parse(io.Reader) (*File, error)`; rule headers and annotations in `parseRuleHeader` |
| `ast.go` | AST node types (`File`, `Rule`, `VarAssign`, …) |
| `pattern.go` | `{name}` pattern captures, including glob/regex constraints |
| `vars.go` | Variable store, expansion, `$[...]` built-in functions |
| `graph.go` | AST → dependency graph; `BuildGraph`, `Graph.Resolve`, `WhyRebuild`, `PrintGraph` |
| `state.go` | Build database (`.cv/state.json`): `LoadState`, `IsStale`, `WhyStale`, `Record`, `Save`, `HashCache` |
| `exec.go` | Parallel executor: `NewExecutor(graph, state, vars, *ExecutorArgs)`, `Build` |
| `depfile.go` | Depfile parsers for `[deps: gcc\|makefile\|msvc\|json\|lines]` |
| `trace_linux.go` / `trace_other.go` | `[deps: trace]` / `[writes: trace]`; **Linux only** |
| `stdlib.go` | `go:embed` of `std/*.cv` and `agents-guide.md` |
| `std/*.cv` | Embedded standard library (`c`, `cxx`, `go`) |
| `cmd/cv/main.go` | The only binary: flag parsing and the `run` driver |
| `completions/` | bash and zsh completions, hand-maintained against the flag set |

Data flow is linear: `Parse` → `BuildGraph` → `Executor.Build`, with
`BuildState` consulted for staleness and rewritten at the end of a successful
run.

## Build and test

```
go build ./...
go test ./...
go vet ./...
```

**Gotcha:** the primary checkout lives under
`~/work/github.com/marcelocantos/`, whose `go.work` does not list this module,
so a bare `go test ./...` there fails with *"directory prefix . does not
contain modules listed in go.work"*. Prefix with `GOWORK=off` when working in
that checkout. Worktrees outside that directory are unaffected.

cv builds itself: `cv test`, `cv build`, `cv vet` and `cv fmt` come from
`include std/go.cv` in the repo's own [`cvfile`](cvfile).

Requires Go 1.25.7+ (`go.mod`). Zero external dependencies — keep it that way.

## Conventions

- Go style follows `~/.claude/go.md`: **no functional options**. New
  multi-parameter constructors take a struct pointer, as `NewExecutor` does
  with `*ExecutorArgs`.
- Default branch is `master`.
- Every source file carries the Apache-2.0 SPDX header.
- Version comes from `-X main.version` at link time; `go install` builds report
  `dev`.

## Documentation map

| Document | Scope | Authority |
|----------|-------|-----------|
| [`README.md`](README.md) | Human overview, quickstart, mini tutorial | Introductory |
| [`DESIGN.md`](DESIGN.md) | Language and CLI specification | **Design intent**, which in §11 runs ahead of the code — see "Spec vs. shipped" below |
| [`agents-guide.md`](agents-guide.md) | Dense cvfile reference; embedded in the binary | Shipped behaviour |
| [`STABILITY.md`](STABILITY.md) | Pre-1.0 catalogue of every interaction surface and its stability | Shipped surface |
| [`docs/why-cv.md`](docs/why-cv.md) | Rationale versus Make | Explanation |
| [`docs/discovered-dependencies.md`](docs/discovered-dependencies.md) | Rationale and prior art for §11 | Explanation |
| [`docs/audits/`](docs/audits/) | Point-in-time audit reports | Historical |
| [`docs/audit-log.md`](docs/audit-log.md) | Append-only log of releases and maintenance | Historical |
| [`bullseye.yaml`](bullseye.yaml) | Convergence targets — the canonical record of followable work | Intent ledger |

`cmd/cv/main.go` is the authority on flags; when the tables in README,
DESIGN §13, `agents-guide.md`, STABILITY and `completions/` disagree with it,
the code wins and the docs are the bug.

## Spec vs. shipped

DESIGN.md is a specification, and parts of §11 are not implemented as of
v0.10.0. Do not implement *against the spec* without checking the code:

- `[deps: trace]` / `[writes: trace]` are **Linux-only** (`strace`). Other
  platforms return an explicit "not yet implemented" error
  (`trace_other.go:16`). DESIGN.md's macOS sandbox/`fs_usage` text is intent.
- `[verify]` as a per-rule annotation is **not parsed**. `parseRuleHeader`
  (`parse.go:427`) recognises only `keep`, `fingerprint`, `deps`, `scan`,
  `scan-format`, `writes` and `reads`; anything else survives into the target
  list. The `--verify` *flag* is implemented and tested.
- `[reads: …]` is checked **post hoc** against the discovered read set
  (`exec.go`, `envelopeViolations`); there is no sandbox.
- The `@` recipe prefix is accepted and stripped but has no effect
  (`exec.go:648`); echoing is a whole-recipe banner under `-v`/`-n`.
- `TargetState.OutputHash` is recorded (`state.go:329`) but never consulted by
  a staleness check.

## Targets

Followable work is tracked in `bullseye.yaml` via the bullseye MCP tools — never
hand-edit it, and never file a `docs/TODO.md`. `docs/audits/` holds the
findings that seeded the current targets.

## Testing

`cv_test.go`, `pattern_test.go` and `discovered_test.go` are table- and
fixture-driven, exercising the library through `Parse`/`BuildGraph`/
`NewExecutor` in temporary directories. `cmd/cv` has no tests: flag wiring,
`-C`, `--complete` and the final `state.Save` are unverified on the shipped
binary path. New CLI behaviour should come with a test that runs the built
binary.
