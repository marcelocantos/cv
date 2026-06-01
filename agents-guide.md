# cv agents guide

Dense reference for AI coding agents. See [DESIGN.md](DESIGN.md) for full
specification, [README.md](README.md) for human-oriented overview.

## Syntax at a glance

```
# Variables
cc = gcc                              # immediate assignment
cflags = -Wall -O2
cflags += -Werror                     # append
cc ?= gcc                             # set only if undefined
lazy version = $[shell git describe]  # deferred until first use

# Rules
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target

# Tasks (always run, no staleness check)
!test: build/app
    ./$input --self-test

!clean:
    rm -rf build/ .cv/
```

## File name

The default build file is `cvfile` (no extension). Override with `cv -f FILE`.

## Variables

### Assignment operators

| Operator | Behavior |
|----------|----------|
| `=` | Immediate evaluation |
| `+=` | Append (space-separated) |
| `?=` | Set only if not already defined |
| `lazy ... =` | Defer evaluation until first use |

Recursive definitions are a parse error: `foo = $foo bar` fails.

### Variable references

| Syntax | Meaning |
|--------|---------|
| `$name` | Variable expansion (multi-char, no parens needed) |
| `${name}` | Delimited form for adjacency: `${foo}bar` |
| `$$` | Literal `$` |

`$foo` means variable `foo`, not `$(f)oo`. There is no single-character
parse rule.

### Substitution references

```
obj = $src:.c=.o       # replace .c suffix with .o in each word of $src
```

### Properties

`$target.dir`, `$target.file` — directory and filename parts.
Works on any variable: `$src.dir`, `$src.file`.

### Environment

All variables are exported to recipes automatically. No `export` keyword.

Priority: CLI args > cvfile > inherited environment.

```
cv cc=clang test       # overrides cc for this invocation
```

## Automatic variables

Available in recipes:

| Variable | Value |
|----------|-------|
| `$target` | Target being built (first target if multi-output) |
| `$input` | First prerequisite |
| `$inputs` | All prerequisites (space-separated) |
| `$changed` | Prerequisites changed since last build |
| `$stem` | Matched stem (single-capture shorthand) |
| `$target.dir` | Directory part of target |
| `$target.file` | Filename part of target |
| `$depfile` | Per-target depfile path; set only when the rule has `[deps: …]`. Hand it to the compiler (e.g., `cc -MMD -MF $depfile`); cv parses it after the recipe and folds the discovered reads into the build database. |

Order-only prerequisites (after `|`) are excluded from `$input`, `$inputs`,
`$changed`.

## Rules

```
target: prereq1 prereq2
    recipe line 1
    recipe line 2
```

Key behaviors:
- **Indentation**: any whitespace (spaces or tabs — no tab requirement)
- **Single shell**: entire recipe runs as one `sh -c` with `set -e`
- **Auto-mkdir**: parent directories of targets created automatically
- **Delete on error**: partial targets removed on failure (default)
- **Line continuation**: trailing `\` joins next line

### Recipe prefixes

| Prefix | Effect |
|--------|--------|
| `@` | Silent (don't echo) |
| `-` | Ignore errors |

### Multi-output rules

```
gen/{name}.pb.h gen/{name}.pb.cc: proto/{name}.proto
    protoc --cpp_out=gen/ $input
```

Multiple targets = one recipe invocation producing all outputs. Tracked
together in build database.

### Order-only prerequisites

```
build/{name}.o: src/{name}.c | build/
    $cc -c $input -o $target
```

After `|`: establish ordering without triggering rebuilds.

### Annotations

```
build/data.db [keep]: schema.sql       # don't delete on error
db/schema [fingerprint: ./version]:    # custom staleness check
    migrate up
```

#### Discovered dependencies (DESIGN.md §11)

cv replaces Make's `-include *.d` / `-MP` ritual with first-class
support for dependencies a recipe only reveals at run time. Two edge
kinds: **hard** (declared, ordering + staleness) and **soft**
(discovered, staleness only — a vanished discovered dep is "changed,"
not "missing"). Recorded discovered set is replaced wholesale on each
successful run.

| Annotation | Meaning |
|---|---|
| `[deps: gcc\|makefile\|msvc\|json\|lines]` | Recipe writes a depfile to `$depfile`; cv parses it post-run and folds the reads into the build DB. |
| `[deps: trace]` | cv runs the recipe under `strace` (Linux only; macOS returns a clear "not yet implemented" error) and records observed reads. |
| `[scan: <cmd>]` | Cheap pre-pass that emits depfile-format output on stdout. In-graph paths it discovers are built before the heavy recipe runs. |
| `[scan-format: <fmt>]` | Format the scan command emits. Defaults to `gcc`. |
| `[writes: manifest <path>]` | Recipe writes a newline-separated list of dynamic outputs; cv fingerprints them. |
| `[writes: trace]` | cv records observed writes (Linux only). |
| `[reads: <glob>…]` | Declared envelope; discovered reads outside the globs are flagged (warn / error under `--verify`). |

```
build/{name}.o [deps: gcc]: src/{name}.c
    $cc $cflags -MMD -MF $depfile -c $input -o $target

gen/.stamp [writes: manifest gen/.manifest]: schema.idl
    idlc --emit-manifest gen/.manifest $input
    touch $target

main.o [deps: gcc] [reads: src/** include/**]: main.c
    $cc -MMD -MF $depfile -c $input -o $target
```

`include std/c.cv` carries `[deps: gcc]` already, so C/C++ projects
get correct, self-healing header tracking with no ritual visible.

## Pattern rules

Named captures replace Make's `%`:

```
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
```

Same `{name}` on both sides: values must match. Multiple captures allowed:

```
build/{arch}/{config}/{name}.o: src/{name}.c
    ${cc_$arch} ${cflags_$config} -c $input -o $target
```

Captures bind when target is requested. Capture values available as variables
in the recipe. Captures cannot contain `/` (single path segment only).

### Constrained captures

```
src/{name}.{ext:c,cc,cpp}             # glob constraint (comma-separated)
build/{name:test_*}.o: test/{name}.cc # glob with wildcards
v{ver/\d+\.\d+}/release.tar.gz       # regex constraint
build/{name/[a-z]\w+}.o: src/{name}.c # regex constraint
```

`{name:glob}` — shell glob syntax. `{name/regex}` — Go regexp.

### Multiple matching patterns

Prerequisites merge across all matching patterns. At most one may have a
recipe:

```
{name}.o: {name}.c
    $cc $cflags -c $input -o $target

{name}.o: {name}.h     # adds header dependency, no recipe — OK
```

## Tasks

```
!clean:
    rm -rf build/ .cv/

!test: build/app
    ./$input --self-test
```

`!` prefix = always run, not a file. In prerequisite position, reference
without `!`:

```
!test-dist: test test:dist
```

## Configs

```
config debug:
    cxxflags += -O0 -g -DDEBUG

config release:
    excludes debug              # mutual exclusion
    cxxflags += -O2 -DNDEBUG

config asan:
    cxxflags += -fsanitize=address
    ldflags += -fsanitize=address
```

Usage: `cv test:debug`, `cv test:debug+asan`. Configs compose left-to-right
with `+`. `builddir` auto-appends config names: `build` becomes
`build-debug-asan`.

Config properties:
- `excludes <config>` — mutual exclusion (error if both active)
- `requires <target>` — build target before any `:config` builds

## Functions

`$[func args]` syntax. Distinct from shell `$(...)` and variable `${name}`.

### Built-in functions

| Function | Example |
|----------|---------|
| `wildcard` | `$[wildcard src/*.c]` |
| `shell` | `$[shell git describe]` |
| `patsubst` | `$[patsubst %.c,%.o,$src]` |
| `subst` | `$[subst old,new,$text]` |
| `filter` | `$[filter %.c,$files]` |
| `filter-out` | `$[filter-out %.test.c,$files]` |
| `dir` | `$[dir $paths]` |
| `notdir` | `$[notdir $paths]` |
| `basename` | `$[basename $paths]` |
| `suffix` | `$[suffix $paths]` |
| `addprefix` | `$[addprefix build/,$objs]` |
| `addsuffix` | `$[addsuffix .o,$names]` |
| `sort` | `$[sort $list]` (also deduplicates) |
| `word` | `$[word 1,$list]` (1-indexed) |
| `words` | `$[words $list]` |
| `strip` | `$[strip $text]` |
| `if` | `$[if $debug,yes,no]` (empty = false) |
| `findstring` | `$[findstring needle,$haystack]` |

### User-defined functions

```
fn objpath(src):
    return $src:src/%.c=build/%.o
```

Invoked as `$[objpath $src]`. Named parameters, not positional.

## Loops

```
configs = debug release

for config in $configs:
    cflags_$config = $cflags ${cflags_extra_$config}
end
```

## Conditionals

```
if $cc == gcc
    cflags += -Wextra
elif $cc == clang
    cflags += -Weverything
else
    cflags += -Wall
end
```

Comparisons: `==`, `!=`. Operands expanded before comparison.

## Includes

```
include common.cv                     # unscoped (paste into current scope)
include lib/cvfile as lib             # scoped (variable/path isolation)
include {path}/cvfile as {path}       # pattern discovery across directories
include std/c.cv                      # embedded standard library
```

### Scoped includes

- Child's `src = ...` becomes `lib.src` in parent
- Targets rebased: child's `build/foo` becomes `lib/build/foo` globally
- Child inherits parent variables, doesn't leak back
- All scopes merge into one DAG (no subprocess boundary)

### Standard library

Embedded in binary, no installation needed. Local files take priority.
All variables use `?=` (overridable before include).

| File | Provides |
|------|----------|
| `std/c.cv` | `cc`, `cflags`, `ldflags`, `ar`, `{name}.o: {name}.c` pattern |
| `std/cxx.cv` | `cxx`, `cxxflags`, `ldflags`, `{name}.o: {name}.cc` pattern |
| `std/go.cv` | `go`, `goflags`, `!build`, `!test`, `!vet` tasks |

## Shell interop

`$(...)` in recipes is **always** shell command substitution — never
interpreted by cv. No `$$` escaping needed:

```
build/app: $obj
    commit=$(git rev-parse --short HEAD)
    $cxx -DCOMMIT="\"$commit\"" -o $target $inputs
```

`$cxx`, `$target`, `$inputs` = cv variables (expanded first).
`$(git ...)` = shell substitution (passed through verbatim).

## Build database

Stored in `.cv/`. Target is stale if any of:
- No previous build recorded
- Recipe text changed (after variable expansion)
- Prerequisite set changed (additions or deletions)
- Any prerequisite content hash differs
- Target file missing

Content hashing uses `(path, mtime, size) -> hash` cache. Nearly as fast
as `stat()`.

## CLI

```
cv [flags] [target...] [var=value...]
```

| Flag | Effect |
|------|--------|
| `-f FILE` | Read FILE instead of `cvfile` |
| `-j N` | Parallel jobs (`-1`=auto, `0`=all cores) |
| `-v` | Verbose |
| `-n` | Dry run |
| `-B` | Unconditional rebuild |
| `--why` | Explain why targets are stale |
| `--graph` | Print dependency subgraph (DOT) |
| `--state` | Show build database entries |
| `--verify` | Error on undeclared reads of in-graph targets and on envelope violations (DESIGN.md §11) |

Default target: first non-task rule. Targets and `var=value` can be
intermixed.

## Sigil summary

| Sigil | Meaning | Interpreted by |
|-------|---------|---------------|
| `$name` / `${name}` | Variable | cv |
| `$[func args]` | Function call | cv |
| `$(...)` | Command substitution | Shell (passthrough) |
| `$$` | Literal `$` | cv |

## Common patterns

### C project

```
include std/c.cv
cc = clang
cflags = -Wall -O2

src = $[wildcard src/*.c]
obj = $src:.c=.o

build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target

build/app: $[addprefix build/,$obj]
    $cc $ldflags -o $target $inputs

!clean:
    rm -rf build/ .cv/
```

### Go project

```
include std/go.cv

!fmt:
    $go fmt ./...

!clean:
    rm -f myapp
```

`std/go.cv` provides `!build`, `!test`, `!vet`.

### Multi-directory project

```
# root cvfile
cc = clang
cflags = -Wall -O2

include {path}/cvfile as {path}

build/app: lib/build/libfoo.a app/build/main.o
    $cc -o $target $inputs
```

```
# lib/cvfile — inherits $cc, $cflags from parent
src = foo.c bar.c
obj = $[patsubst %.c,build/%.o,$src]

build/libfoo.a: $obj
    ar rcs $target $inputs

build/{name}.o: {name}.c
    $cc $cflags -c $input -o $target
```

### Multi-config project

```
include std/cxx.cv
cxx = c++ -std=c++17
builddir = build

config debug:
    excludes release
    cxxflags += -O0 -g

config release:
    excludes debug
    cxxflags += -O2 -DNDEBUG

config asan:
    cxxflags += -fsanitize=address
    ldflags += -fsanitize=address

$builddir/{name}.o: src/{name}.cc
    $cxx $cxxflags -c $input -o $target

$builddir/app: $builddir/main.o $builddir/lib.o
    $cxx $ldflags -o $target $inputs

!test: $builddir/app
    ./$input
```

```
cv test:debug+asan     # compose configs
cv bench:release -j0   # all cores
```

## Key differences from Make

| Make | cv |
|------|-----|
| Tabs required | Any whitespace |
| `$@`, `$<`, `$^` | `$target`, `$input`, `$inputs` |
| `$(func ...)` | `$[func ...]` (shell `$(...)` untouched) |
| `$$` in recipes | Not needed |
| `.PHONY: clean` | `!clean:` |
| Timestamp-based | Content hash-based |
| Implicit rules | `include std/c.cv` (opt-in) |
| `%` | `{name}` (named, multiple captures) |
| `.DELETE_ON_ERROR` | Default |
| `.ONESHELL` | Default |
| `$(MAKE)` recursive | Scoped includes, single DAG |
| `export VAR` | All variables exported automatically |
| `ifeq ($(X),val)` | `if $X == val` |
