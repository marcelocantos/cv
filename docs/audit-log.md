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
