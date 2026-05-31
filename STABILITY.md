# Stability

## Commitment

Version 1.0 represents a backwards-compatibility contract. After 1.0, breaking
changes to the CLI interface, mkfile syntax, build state format, or Go API
require a major version bump (which in this project means forking to a new
product). The pre-1.0 period exists to get these right.

## Interaction surface catalogue

Snapshot as of v0.8.0.

### CLI flags

| Flag | Type | Default | Stability |
|------|------|---------|-----------|
| `-B` | bool | `false` | **Stable** |
| `-C` | string | `""` | **Stable** |
| `-f` | string | `"mkfile"` | **Stable** |
| `-j` | int | `-1` | **Stable** |
| `-n` | bool | `false` | **Stable** |
| `-v` | bool | `false` | **Stable** |
| `--graph` | bool | `false` | **Stable** |
| `--help-agent` | bool | `false` | **Stable** |
| `--state` | bool | `false` | **Stable** |
| `--version` | bool | `false` | **Stable** |
| `--why` | bool | `false` | **Stable** |
| `--complete` | bool | `false` | **Needs review** — internal flag for shell completion; may be replaced by a subcommand or hidden flag |
| `--verify` | bool | `false` | **Needs review** — new in v0.9.0; errors on undeclared reads of in-graph targets and on envelope violations (DESIGN.md §11) |

Positional arguments:

| Form | Stability |
|------|-----------|
| `target` | **Stable** |
| `target:config1+config2` | **Needs review** — config composition syntax may evolve |
| `var=value` | **Stable** |

### Mkfile syntax

#### Variable assignments

| Syntax | Stability |
|--------|-----------|
| `name = value` | **Stable** |
| `name += value` | **Stable** |
| `name ?= value` | **Stable** |
| `lazy name = expr` | **Stable** |

Recursive definitions (`foo = $foo bar`) are a parse error — **Stable**.

#### Variable references

| Syntax | Stability |
|--------|-----------|
| `$name` | **Stable** |
| `${name}` | **Stable** |
| `$$` (literal `$`) | **Stable** |
| `$[func args]` | **Stable** |
| `$(...)` (shell passthrough) | **Stable** |
| `$name.dir`, `$name.file` (properties) | **Stable** |
| `$src:.c=.o` (substitution refs) | **Stable** |

#### Rules

| Feature | Syntax | Stability |
|---------|--------|-----------|
| Basic rule | `target: prereqs\n\trecipe` | **Stable** |
| Multi-output | `a b: prereqs` | **Stable** |
| Order-only prereqs | `target: normal \| order-only` | **Stable** |
| Tasks | `!name: prereqs` | **Stable** |
| Pattern rules | `build/{name}.o: src/{name}.c` | **Stable** |
| Constrained captures (glob) | `{name:c,cc,cpp}` | **Needs review** — syntax may evolve |
| Constrained captures (regex) | `{name/\d+}` | **Needs review** — syntax may evolve |
| `[keep]` annotation | `target [keep]: ...` | **Stable** |
| `[fingerprint: cmd]` annotation | `target [fingerprint: cmd]: ...` | **Stable** |
| `[deps: <format>]` annotation | `target [deps: gcc\|makefile\|msvc\|json\|lines\|trace]: ...` (DESIGN.md §11) | **Needs review** — new in v0.9.0. `trace` is Linux-only (strace); macOS returns a clear "not yet implemented" error. |
| `[scan: <cmd>]` annotation | `target [scan: cc -M -MG $input]: ...` (DESIGN.md §11) | **Needs review** — new in v0.9.0. Cheap pre-pass that produces schedulable soft edges before the heavy recipe runs. |
| `[scan-format: <format>]` annotation | defaults to `gcc`. Same formats as `[deps: …]`. | **Needs review** — new in v0.9.0 |
| `[writes: <spec>]` annotation | `target [writes: manifest path/to/manifest]: ...` or `[writes: trace]` (Linux). DESIGN.md §11. | **Needs review** — new in v0.9.0 |
| `[reads: <glob>…]` annotation | declared read envelope; mk warns (or errors under `--verify`) when discovered reads fall outside the globs. | **Needs review** — new in v0.9.0; supports simple `*` and `**` globs. |
| Recipe prefix `@` (silent) | **Stable** |
| Recipe prefix `-` (ignore errors) | **Stable** |
| Inline comments | `target: dep # comment` | **Stable** |

#### Automatic variables

| Variable | Stability |
|----------|-----------|
| `$target` | **Stable** |
| `$input` | **Stable** |
| `$inputs` | **Stable** |
| `$changed` | **Stable** |
| `$stem` | **Stable** |
| `$depfile` (set when rule has `[deps: …]`) | **Needs review** — new in v0.9.0 |

#### Include directives

| Form | Stability |
|------|-----------|
| `include path.mk` (unscoped) | **Stable** |
| `include dir/mkfile as alias` (scoped) | **Stable** |
| `include {path}/mkfile as {path}` (pattern discovery) | **Stable** |
| `include std/*.mk` (embedded stdlib) | **Stable** |

#### Conditionals

| Feature | Stability |
|---------|-----------|
| `if $var == value` / `elif` / `else` / `end` | **Stable** |
| Operators `==`, `!=` | **Stable** — additional operators (e.g. regex match) may be added |

#### Loops

| Feature | Stability |
|---------|-----------|
| `for var in $list:` / `end` | **Needs review** — syntax settled but limited testing in complex scenarios |

#### User-defined functions

| Feature | Stability |
|---------|-----------|
| `fn name(params): return expr` | **Needs review** — single-expression body may be too limiting; multi-line functions may be needed |
| `$[name args]` invocation | **Stable** |

#### Config blocks

| Feature | Stability |
|---------|-----------|
| `config name:` block | **Needs review** — overall design may evolve |
| `excludes other` | **Needs review** |
| `requires target` | **Needs review** |
| Variable assignments in config | **Stable** |

#### Built-in functions

| Function | Stability |
|----------|-----------|
| `$[wildcard pattern]` | **Stable** |
| `$[shell command]` | **Stable** |
| `$[patsubst pat,repl,text]` | **Stable** |
| `$[subst from,to,text]` | **Stable** |
| `$[filter pattern,text]` | **Stable** |
| `$[filter-out pattern,text]` | **Stable** |
| `$[dir paths]` | **Stable** |
| `$[notdir paths]` | **Stable** |
| `$[basename paths]` | **Stable** |
| `$[suffix paths]` | **Stable** |
| `$[addprefix prefix,list]` | **Stable** |
| `$[addsuffix suffix,list]` | **Stable** |
| `$[sort list]` | **Stable** |
| `$[word n,list]` | **Stable** |
| `$[words list]` | **Stable** |
| `$[strip text]` | **Stable** |
| `$[findstring needle,haystack]` | **Stable** |
| `$[if cond,then,else]` | **Stable** |

### Standard library (`std/*.mk`)

| File | Variables | Rules/Tasks | Stability |
|------|-----------|-------------|-----------|
| `std/c.mk` | `cc`, `cflags`, `ldflags`, `ar` | `{name}.o [deps: gcc]: {name}.c` | **Stable** — recipe now emits `-MMD -MF $depfile` (v0.9.0) |
| `std/cxx.mk` | `cxx`, `cxxflags`, `ldflags` | `{name}.o [deps: gcc]: {name}.cc` | **Stable** — recipe now emits `-MMD -MF $depfile` (v0.9.0) |
| `std/go.mk` | `go`, `goflags` | `!build`, `!test`, `!vet` | **Needs review** — may need more tasks (e.g. `!lint`, `!fmt`) |

### Build state format (`.mk/state.json`)

```json
{
  "targets": {
    "<path>": {
      "recipe_hash": "<sha256-hex>",
      "input_hashes": {"<prereq>": "<sha256-hex>"},
      "output_hash": "<sha256-hex>",
      "fingerprint_hash": "<sha256-hex>",
      "prereqs": ["<prereq>"],
      "discovered_prereqs": ["<discovered-path>"],
      "discovered_input_hashes": {"<discovered-path>": "<sha256-hex>"}
    }
  }
}
```

Config-specific state: `.mk/state-<config1>-<config2>.json`.
Per-target depfiles emitted by `[deps: …]` rules: `.mk/deps/[<config>/]<target>.d`
(written by the recipe, parsed and removed by mk after a successful run).

Stability: **Needs review** — format is functional but may gain fields (e.g. build timestamps, output size). Existing fields are unlikely to change.

### Go exported API

mk is primarily a CLI tool. The Go API is exported for testability and potential embedding but is not the primary interface.

| Symbol | Stability |
|--------|-----------|
| `Parse(io.Reader) (*File, error)` | **Stable** |
| `BuildGraph(*File, *Vars, *BuildState, []string) (*Graph, error)` | **Needs review** — signature may change as features are added |
| `NewExecutor(...)` | **Needs review** — parameter list is long; may become an options struct |
| `NewVars() *Vars` | **Stable** |
| `Vars.Get`, `Set`, `Expand`, `Clone`, etc. | **Stable** |
| `LoadState(string) *BuildState` | **Stable** |
| `BuildState.IsStale`, `WhyStale`, `Record`, `Save` | **Stable** |
| `NewHashCache() *HashCache` | **Stable** |
| AST types (`File`, `Rule`, `VarAssign`, etc.) | **Needs review** — fields may be added |
| `Graph.Resolve` returns unexported `*resolvedRule` | **Fluid** — exported method with unexported return type is a design issue |
| `Graph.Targets`, `Tasks`, `ConfigNames`, `DefaultTarget` | **Stable** |
| `Graph.PrintGraph`, `WhyRebuild` | **Stable** |
| `AgentsGuide string` | **Stable** |
| `ParsePattern(string) (Pattern, bool, error)` | **Needs review** — may become internal |

### Shell completions

| File | Stability |
|------|-----------|
| `completions/mk.bash` | **Stable** — tracks CLI flags |
| `completions/mk.zsh` | **Stable** — tracks CLI flags |

## Gaps and prerequisites for 1.0

- **`Graph.Resolve` return type**: Returns unexported `*resolvedRule`. Either export the type or unexport the method.
- **`NewExecutor` signature**: 7 parameters — consider an options struct.
- **`BuildGraph` signature**: May need additional parameters as features grow — consider an options struct.
- **Config composition**: The `target:config1+config2` CLI syntax and config block semantics need more real-world usage before locking in.
- **Constrained captures**: Glob and regex constraint syntax (`{name:glob}`, `{name/regex}`) needs more usage to confirm the design.
- **Loop syntax**: `for var in $list:` — functional but limited testing in complex real-world mkfiles.
- **User-defined functions**: Single-expression body (`fn name(params): return expr`) may prove too limiting. Multi-line function bodies may be needed.
- **Error messages**: Parse and build error messages should include file path (not just line number) for multi-file projects.
- **Test coverage**: Config blocks, loops, and user-defined functions have limited integration tests.
- **Documentation**: DESIGN.md is comprehensive but some newer features (inline comments, `-C` flag) may not be fully documented.

## Out of scope for 1.0

- **Parallel recipe execution within a single rule** (e.g. multi-command recipes where lines are independent).
- **Remote build caching** (shared `.mk/` state across machines).
- **Watch mode** (automatic rebuilds on file change).
- **Plugin system** for custom functions beyond `fn`.
- **Windows native support** — builds cross-compile for Windows but native path handling (`\`) is deferred.
