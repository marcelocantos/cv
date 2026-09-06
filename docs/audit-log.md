# Audit Log

Chronological record of audits, releases, documentation passes, and other
maintenance activities. Append-only — newest entries at the bottom.

## 2026-03-02 — /release v0.8.0

- **Commit**: `968a8e4`
- **Outcome**: Released v0.8.0 (darwin-arm64, linux-amd64, linux-arm64). Added inline comment stripping, STABILITY.md, and expanded include test coverage. Homebrew formula updated.

## 2026-05-31 — /release v0.9.0

- **Outcome**: Released v0.9.0 — discovered dependencies (DESIGN.md §11). Bundled phases T1.1–T1.6 in a single PR: depfile adapter ([deps: gcc|makefile|msvc|json|lines|trace]), --verify, scan nodes, dynamic outputs, trace mode on Linux, [reads: …] envelopes. NewExecutor collapsed into a struct-arg constructor per the Go style directive. Last release shipped under the mk name.

## 2026-06-01 — project renamed: mk → cv

- **Reason**: Name conflict with [Plan 9 mk](https://9p.io/sys/doc/mk.html) (Hume & Flandrena, 1989) — same project category, same `mkfile` filename, prior art by 35+ years. New name `cv` (short for *converge*) re-frames the tool as a convergence engine: declare desired state, run `cv`, the world is arranged to satisfy it. Pairs with the [bullseye](https://github.com/marcelocantos/bullseye) target system and the `/cv` skill which audits convergence — one verb across the whole convergence vocabulary.
- **Scope**: package mk → package cv; binary `mk` → `cv`; conventional filename `mkfile` → `cvfile`; state dir `.mk/` → `.cv/`; stdlib files `std/*.mk` → `std/*.cv`; Go module `github.com/marcelocantos/mk` → `github.com/marcelocantos/cv`; GitHub repo renamed (old URL redirects); shell completions; CI workflow; Homebrew formula. All historical audit-log entries above this one preserve the `mk` name; future entries use `cv`.
- **Plan 9 mk is preserved verbatim** wherever the docs reference it as prior art (DESIGN.md, docs/why-cv.md, docs/discovered-dependencies.md). The naming change is about disambiguation, not severing the design lineage.

## 2026-09-06 — documentation pass (docs skill)

- **Scope**: reconciled every user-facing description of cv with the shipped
  binary at `v0.10.0-1-g1f37609`, addressing ENT-006 (CLI catalogued in six
  disagreeing places), ENT-007 (DESIGN.md §11 ahead of the code) and ENT-010
  (`@` prefix is a no-op) from `docs/audits/entropy-audit-2026-08-22.md`.
- **Added**: `AGENTS.md` as the canonical instruction file for agents working
  on cv itself (layout, build/test with the `GOWORK=off` gotcha, conventions,
  doc map, spec-vs-shipped divergences), plus a thin `CLAUDE.md` importing it.
- **Corrected**: `-j` semantics (`-1` = one job per CPU, `0` = unlimited) in
  README, DESIGN §13 and `agents-guide.md`, all of which said `0` meant "all
  cores"; added `-C`, `--version`, `--help-agent`, `--complete` and `--verify`
  where each table was missing them; added `--verify` to both shell
  completions.
- **Marked as unimplemented**: macOS trace, the `[verify]` annotation, sandbox
  enforcement of `[reads: …]`, and the `@` recipe prefix.
- **Documented for the first time**: comment and indentation rules (DESIGN §0,
  agents-guide) — comments are stripped from statements but not recipes.
- **STABILITY.md**: snapshot bumped to v0.10.0; `NewExecutor` entry corrected
  to the `*ExecutorArgs` struct signature shipped in v0.9.0; gap list refreshed.
