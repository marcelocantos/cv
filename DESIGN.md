# mk Design Spec

A build tool with Make's dependency-graph model, minus 48 years of
accumulated pain.

## Philosophy

Same execution model as Make: a declared dependency DAG, recipes that
produce targets from prerequisites, parallel execution, only stale
targets rebuilt. What changes: content hashing, sane defaults, clean
syntax, first-class support for things Make bolted on after the fact.

mk is not a radical reimagination. It is Make with the mistakes fixed.

---

## 1. Variables

### Assignment

```
cc = gcc                        # immediate (always)
cflags = -Wall -O2              # immediate
cflags += -Werror               # append
lazy version = $[shell git describe]   # explicit deferred evaluation
```

All assignments are immediate by default. `lazy` defers evaluation
until first use. Recursive definitions (`foo = $foo bar`) are a
parse error.

### Reference

`$name` references a variable. Multi-character names work without
delimiters — there is no single-character parse rule. `$foo` means
the variable `foo`, not `$(f)` followed by `oo`.

`${name}` delimits when the variable is adjacent to identifier
characters: `${foo}bar`.

### Sigil summary

| Syntax | Meaning | Context |
|--------|---------|---------|
| `$name` | Variable reference | Everywhere |
| `${name}` | Variable reference (delimited) | Everywhere |
| `$[func args]` | mk function call | Everywhere |
| `$(...)` | Shell command substitution | Recipes (passed through to shell) |

`$name` and `${name}` are expanded by mk everywhere. `$[...]` is
expanded by mk everywhere. `$(...)` is **never** interpreted by mk —
it is passed through verbatim to the shell. This eliminates the `$$`
escaping dance that Make requires for shell commands in recipes.

### Substitution references

```
obj = $src:.c=.o
```

Replaces the suffix `.c` with `.o` in every word of `$src`.

### Environment

All variables are environment variables. Recipes see them without
`export`. Command-line overrides beat mkfile assignments beat
inherited environment. One rule, no flags.

```
$ mk cc=clang test        # overrides cc for this invocation
```

### Conditional assignment

```
csp_include ?= include          # set only if not already defined
```

---

## 2. Rules

```
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
```

- **Indentation:** any whitespace (spaces or tabs).
- **Single shell:** the entire recipe block runs as one `sh -c`
  invocation with `set -e`. `cd` persists across lines. No `\`
  continuation needed for multi-line logic.
- **Auto-mkdir:** parent directories of targets are created
  automatically.
- **Delete on error:** if a recipe fails, the partial target is
  removed. This is Make's `.DELETE_ON_ERROR`, but default.
- **Line continuations:** a trailing `\` joins the next line, for
  readability of long variable values or prerequisite lists.

### Recipe prefixes

| Prefix | Meaning |
|--------|---------|
| `@`    | Silent — don't echo this line |
| `-`    | Ignore errors on this line |

### Automatic variables

| Name | Meaning |
|------|---------|
| `$target` | Target being built |
| `$input` | First prerequisite |
| `$inputs` | All prerequisites (space-separated) |
| `$changed` | Prerequisites that changed since last build |
| `$stem` | Matched stem (single-capture shorthand) |
| `$target.dir` | Directory part of target |
| `$target.file` | Filename part of target |

No `$@`, `$<`, `$^`. One set of names.

### Shell interop

`$(...)` in recipes is shell command substitution, not mk expansion.
mk variables and shell variables coexist naturally:

```
build/app: $obj
    commit=$(git rev-parse --short HEAD)
    date=$(date +%Y-%m-%d)
    $cxx -DCOMMIT="\"$commit\"" -DDATE="\"$date\"" -o $target $inputs
```

`$cxx`, `$target`, `$inputs` are mk variables (expanded before the
shell sees the script). `$(git ...)` and `$(date ...)` are shell
command substitution (passed through verbatim). mk functions are
available in recipes via `$[...]`:

```
build/report: $obj
    echo "building $[words $inputs] objects"
    $cxx -o $target $inputs
```

### Order-only prerequisites

Prerequisites after `|` establish build ordering without triggering
rebuilds:

```
build/{name}.o: src/{name}.c | build/
    $cc $cflags -c $input -o $target
```

The `build/` directory is created before the recipe runs, but
changes to it do not make the target stale. Order-only prerequisites
are excluded from `$inputs`, `$input`, and `$changed`.

Use cases: directory creation, tool installation, any dependency
where existence matters but content does not.

---

## 3. Tasks

```
!clean:
    rm -rf build/ .mk/

!test: build/app
    ./build/app --self-test

!deploy: build/app.img
    docker push myapp:latest
```

The `!` prefix declares "this is an action, not a file." Tasks always
run when requested — there is no staleness check. In prerequisite
position, tasks are referenced by name without `!`:

```
!test-dist: test test:dist
```

---

## 4. Patterns

Named captures replace Make's `%`:

```
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
```

Same name on both sides means values must match. Multiple captures
are allowed:

```
build/{arch}/{config}/{name}.o: src/{name}.c
    ${cc_$arch} ${cflags_$config} -c $input -o $target
```

Captures bind when a target is requested. Requesting
`build/arm64/release/foo.o` binds `arch=arm64`, `config=release`,
`name=foo`. Capture values are available as variables in the recipe.

Captures must not contain `/` — each capture matches within a single
path segment.

### Constrained captures

Captures can be restricted with glob or regex constraints:

```
# Glob — comma-separated alternatives, shell wildcards
src/{name}.{ext:c,cc,cpp}
build/{name:test_*}.o: test/{name}.cc

# Regex — full regular expression
v{ver/\d+\.\d+}/release.tar.gz
build/{name/[a-z]\w+}.o: src/{name}.c
```

`{name:glob}` uses shell glob syntax (`*`, `?`, `[...]`) with `,` for
alternation. `{name/regex}` uses Go regular expressions. Both still
enforce the no-`/` rule. Unconstrained `{name}` is unchanged.

### Multiple matching patterns

When multiple pattern rules match a target, mk merges their
prerequisites. At most one matching rule may have a recipe;
multiple recipes for the same target is an error.

```
{name}.o: {name}.c
    $cc $cflags -c $input -o $target

{name}.o: {name}.h       # adds header dependency, no recipe
```

---

## 5. Multi-output rules

```
gen/{name}.pb.h gen/{name}.pb.cc: proto/{name}.proto
    protoc --cpp_out=gen/ $input
```

Multiple targets on the left of `:` means one invocation produces
all outputs. Always. No ambiguity, no special syntax.

The build database tracks all outputs together. If any output is
missing or stale, the recipe runs once. The `$target` variable
refers to the first listed target.

---

## 6. Configs

Named configurations for build variants. Configs compose.

### Declaration

```
config debug:
    cxxflags += -O0 -g -DDEBUG

config release:
    excludes debug
    cxxflags += -O2 -DNDEBUG

config asan:
    cxxflags += -fsanitize=address -fno-omit-frame-pointer
    ldflags += -fsanitize=address

config dist:
    requires dist
    csp_include = dist
```

### Properties

| Property | Meaning |
|----------|---------|
| `excludes <config>` | Mutual exclusion. `mk test:debug+release` is an error. |
| `requires <target>` | Prerequisite. Ensures the named target has been built before any `:config` builds proceed. |
| Variable assignments | Override or append to base variables. |

### Usage

```
$ mk test              # base config
$ mk test:debug        # debug config
$ mk test:debug+asan   # debug + asan composed
$ mk test:dist         # test against distribution build
```

### Composition

`:` separates target from config. `+` combines configs. Configs
stack left-to-right: `test:debug+asan` applies `debug` overrides,
then `asan` on top. `+=` accumulates; `=` from a later config
overrides an earlier one.

### Build directory

mk auto-derives the build directory by appending config names to the
base `builddir`:

```
builddir = build
# mk test:debug+asan → builddir = build-debug-asan
```

The build database tracks each config combination independently.

---

## 7. Build database

Stored in `.mk/` (like `.git/`). Tracks per target:

- **Prerequisite set.** If the set changes — additions or deletions —
  the target is stale. Delete a source file? Prerequisite set changed.
  Rebuild.
- **Recipe text** (after variable expansion). Change `-O2` to `-O0`?
  Recipe changed. Rebuild. Change a comment in the mkfile? Recipe
  unchanged. No rebuild.
- **Input fingerprints.** Content hash (SHA-256) of each prerequisite
  at last build time. Modify a file then revert? Hash matches. No
  rebuild. Extract an unchanged file from a new archive? Hash
  matches. No rebuild. Timestamps lie after git operations, archive
  extraction, rsync, and CI cache restores; content hashes don't.
- **Output fingerprint.** Detects targets modified outside the build.

### Performance

Content hashing uses an `(path, mtime, size) → hash` cache. Only
re-reads files whose metadata changed. Nearly as fast as `stat()`.

### Non-file artifacts

Annotation for custom fingerprinting:

```
app.img [fingerprint: docker inspect --format '{{.Id}}' myapp]:
        build/app Dockerfile
    docker build -t myapp .

db/schema [fingerprint: ./schema-version]:
    migrate up
```

The fingerprint command outputs a stable string. If it changes since
last build, the target is stale.

---

## 8. Conditionals

```
if $cc == gcc
    cflags += -Wextra
elif $cc == clang
    cflags += -Weverything
else
    cflags += -Wall
end
```

Comparisons: `==`, `!=`. Operands are expanded before comparison.
Conditionals can appear at file scope or inside other conditionals.

---

## 9. Functions

### Syntax

mk functions use `$[func args]`. This is distinct from shell
`$(...)` and variable `${name}` — each sigil has exactly one meaning:

```
obj = $[patsubst %.cc,$builddir/%.o,$lib_srcs]
src = $[wildcard src/*.c]
lazy version = $[shell git describe]
```

### Built-in functions

| Function | Description |
|----------|-------------|
| `$[wildcard pattern]` | Glob file paths |
| `$[shell command]` | Run a shell command, capture stdout |
| `$[patsubst pat,repl,text]` | Pattern substitution across words |
| `$[subst from,to,text]` | Simple string substitution |
| `$[filter pattern,text]` | Keep words matching pattern |
| `$[filter-out pattern,text]` | Remove words matching pattern |
| `$[dir paths]` | Directory part of each path |
| `$[notdir paths]` | Filename part of each path |
| `$[basename paths]` | Strip suffix from each path |
| `$[suffix paths]` | Extract suffix from each path |
| `$[addprefix prefix,list]` | Prepend to each word |
| `$[addsuffix suffix,list]` | Append to each word |
| `$[sort list]` | Sort and deduplicate |
| `$[word n,list]` | Nth word (1-indexed) |
| `$[words list]` | Word count |
| `$[strip text]` | Normalize whitespace |
| `$[if cond,then,else]` | Conditional expansion |
| `$[findstring needle,haystack]` | Search for substring |

### User-defined functions

```
fn objpath(src):
    return $src:src/%.c=build/%.o
```

Invoked as `$[objpath $src]`. Named parameters, no positional
`$(1)`/`$(2)`.

### Loops

For generating rules across a matrix:

```
configs = debug release

for config in $configs:
    cflags_$config = $cflags ${cflags_extra_$config}
```

---

## 10. Includes

```
include std/c.mk              # opt-in standard rules
include lib/mkfile as lib     # scoped: lib.obj, lib.cflags, etc.
include common.mk             # unscoped paste
include {path}/mkfile as {path}   # auto-discover subdirectory mkfiles
```

### Unscoped includes

`include common.mk` pastes the file's contents into the current
scope. Variables and rules merge directly — same as C `#include`.

### Scoped includes

`include lib/mkfile as lib` includes the file with isolation:

- **Variable scoping.** The child's assignments live under the alias
  prefix. The child's `src = foo.c bar.c` becomes `lib.src` from
  the parent's perspective. The child inherits the parent's
  variables as defaults (`$cc`, `$cflags`) but its own assignments
  do not leak back.

- **Path rebasing.** Targets and prerequisites declared in the child
  are rebased relative to the child's directory. The child writes
  `build/libfoo.a`; mk inserts `lib/build/libfoo.a` into the
  global graph. Cross-references between siblings use relative
  paths: `../lib/build/libfoo.a` from `app/mkfile` resolves to
  `lib/build/libfoo.a` in the global graph.

- **Single graph.** All scoped includes merge into one dependency
  DAG. There is no subprocess boundary, no opaque `$(MAKE)` call.
  mk sees every target and every dependency across the entire
  project, enabling correct incremental builds, parallel execution
  across directory boundaries, and accurate `--why` diagnostics.

### Pattern discovery

```
include {path}/mkfile as {path}
```

The `{path}` capture globs across directories. Each matching
`mkfile` is included with its directory as the scope name. This
is the primary mechanism for multi-directory projects:

```
# root mkfile
cc = clang
cflags = -Wall -O2

include {path}/mkfile as {path}

build/app: lib/build/libfoo.a app/build/main.o
    $cc -o $target $inputs
```

```
# lib/mkfile — sees $cc from parent
src = foo.c bar.c
obj = $[patsubst %.c,build/%.o,$src]

build/libfoo.a: $obj
    ar rcs $target $inputs

build/{name}.o: {name}.c
    $cc $cflags -c $input -o $target
```

After inclusion, the global graph contains targets
`lib/build/libfoo.a`, `lib/build/foo.o`, `lib/build/bar.o`, etc.
The root mkfile references them by their rebased file paths. The
variable `$lib.src` is `foo.c bar.c`.

### Standard library

The standard library (`std/`) provides conventional rules for common
languages:
- `std/c.mk` — C compilation (`cc`, `cflags`, pattern rules)
- `std/cxx.mk` — C++ compilation
- `std/go.mk` — Go build

These are opt-in. mk has no implicit rules and no built-in variables.

Standard library files are embedded in the mk binary — `include std/c.mk`
works without any installation step. A local `std/c.mk` file takes
priority over the embedded version. All variables use `?=` so they can be
overridden before the include.

---

## 11. Discovered dependencies

Some prerequisites are unknown until a recipe runs. A C compile reads
headers no mkfile declared; a codegen step may emit files no mkfile
listed. Make papers over this with `-MMD` plus `-include *.d` plus `-MP`
empty-rule hacks. mk gives discovery a first-class place in the model.

### Hard and soft edges

Every dependency edge is one of two kinds.

| Edge | Source | Constrains | Required to exist? |
|---|---|---|---|
| **Hard** | Declared in the mkfile (`a: b`) | Ordering **and** staleness | Yes — it is an ordering constraint |
| **Soft** | Discovered by running the recipe | Staleness only | No — absence is just "changed" |

A hard edge promises build *order*: `b` is built before `a`'s recipe
runs. A soft edge cannot constrain order — you do not learn it until
*after* the recipe has run — so it can only invalidate.

### Authoring rule

> **Declare in-graph targets. Discover leaves.**

An edge must be hard iff the path is an *in-graph target* — some rule
can produce it. Everything else is safe to discover.

The criterion is **structural**, not based on provenance. Whether a file
is committed, and whether it was once emitted by some out-of-band
generator, are both irrelevant. Committed generated artefacts (vendored
protobuf output, checked-in parsers, lockfiles) are common and
legitimate; committedness tells you nothing about whether *this* build
produces the file.

Because mk knows its own target set, this rule is enforced: a discovered
read whose path matches a known target or pattern that was not declared
as a prerequisite is reported (or auto-promoted to a hard edge, per
policy). It is a latent ordering race.

### Staleness contract

Two rules make the model airtight and fix Make's deleted-file failure:

1. **A recorded soft-dep that has vanished counts as "changed," never as
   "missing input."** Deleting a header that no source still references
   means the recorded input set no longer matches reality → the target
   is stale → rebuild. The rebuild records a fresh read-set that omits
   the deleted file; the stale edge is gone. The edge *triggers the very
   rebuild that erases it* — no `-MP`, no empty phony rules, no `.d`
   files in the tree.

2. **Replace the discovered set wholesale on each successful run — never
   union.** Unioning lets stale edges accumulate. Wholesale replacement
   guarantees the recorded set always reflects the *last actual*
   execution.

Deleting a header while *leaving* an `#include` of it produces a real
compiler error, which mk surfaces — it always defers to the recipe's
exit status rather than pre-judging from a stale edge.

### Mechanisms

Discovery is one capability with three producers.

**Depfile adapter.** The recipe emits a dependency list as a byproduct;
mk parses it, normalizes paths, folds it into the build database, and
discards the file:

```
build/{name}.o [deps: gcc]: src/{name}.c
    $cc $cflags -MMD -MF $depfile -c $input -o $target
```

`[deps: <format>]` names a parser; `$depfile` is a path mk allocates
under `.mk/deps/` (partitioned by config, mirroring the target path).
Formats: `gcc`/`makefile`, `msvc`, `json`, `lines`. This is strictly
better than `-include`: mk owns the parse, folds into the content-hashed
DB, and no `.d` enters the source tree. `std/c.mk` and `std/cxx.mk`
carry this annotation, so `include std/c.mk` gets correct header
tracking with no ritual visible.

**Trace.** Observe the recipe's actual file accesses with zero tool
cooperation:

```
build/{name}.o [deps: trace]: src/{name}.c
    $cc $cflags -c $input -o $target
```

mk runs the recipe under observation (macOS: sandbox profile /
`fs_usage`; Linux: seccomp-bpf, ptrace, or `fanotify`) and records every
path opened for read. Works for any tool — protoc, sass, bundlers — not
just compilers. Platform-specific and slower than a depfile, so it is
opt-in. Trace also powers verification (below).

**Scan nodes.** Soft edges cannot inform *this* build's schedule —
except via a cheap pre-pass. A scan node is a separate lightweight
recipe whose output *is* the dependency set, scheduled before the heavy
recipe:

```
build/{name}.o [scan: cc -M $cflags $input]: src/{name}.c
    $cc $cflags -c $input -o $target
```

A scan node is a first-class graph node: it has its own deps (including
any generated headers it scans through, which forces the correct
interleaving for free) and is scheduled like any other target.
"Two-phase" *emerges* wherever a scan node exists rather than being a
second execution mode.

### Record-and-reuse is the spine

Two-phase analysis-then-execution is the right capability but the wrong
default. **The previous build's recorded soft-edges *are* the analysis
pass.** On an incremental build, last run's discovered set is an
exact-as-of-last-build approximation of the dependency shape; mk
schedules against it, executes, and re-records. This is safe even when
the shape has drifted, because correctness comes from the post-hoc
content-hash check plus wholesale re-record, never from the schedule.
Scheduling on a stale recorded edge can produce a slightly suboptimal
parallel schedule that self-corrects next run; it can never miss a
rebuild.

So execute-and-record is the default; scan nodes are an opt-in
optimization for large graphs and remote execution, where an action's
inputs must be known before it ships off-machine.

### Build database

The build database (§7) gains, per target+config:

- **Discovered input set** — soft edges from the last successful run,
  with content fingerprints at that time.
- **Discovered output set** — for dynamic outputs (below).

Staleness becomes: declared set changed, discovered set changed
(including a vanished member), any input fingerprint changed, recipe
text changed, or an output fingerprint changed. Everything is
partitioned by config exactly as the existing database is.

### Dynamic outputs

Symmetric to dynamic inputs — a recipe may produce a set of outputs not
known statically:

```
gen/ [writes: manifest gen/.manifest]: schema.idl
    idlc --emit-manifest gen/.manifest schema.idl
```

`[writes: manifest <path>]` reads a producer-emitted list of outputs;
`[writes: trace]` observes them. mk records the discovered output set
and fingerprints each, so downstream consumers and `mk clean` see the
real artefacts.

### Verification

Under trace, mk knows the complete read/write set, so it can assert the
build is correctly specified — something Make cannot do at all. Run
with `--verify` (or per-target `[verify]`) and mk flags:

- **Undeclared reads of an in-graph target** — a recipe read a path
  this build also produces but did not declare. A latent ordering race.
- **Undeclared writes** — output outside the declared/discovered output
  set; graph pollution.
- **Reads outside a declared envelope** — see below.

A recipe may bound its dynamism without enumerating it:

```
build/{name}.o [reads: include/** src/**]: src/{name}.c
    …
```

`[reads: <glob>…]` is a static bound on what the recipe may read. The
sandbox enforces it; the verifier checks against it; a remote scheduler
can pre-stage it. Optional.

### Syntax summary

| Annotation | Meaning |
|---|---|
| `[deps: gcc\|makefile\|msvc\|json\|lines]` | Recipe emits a depfile at `$depfile`; folded post-run |
| `[deps: trace]` | mk observes the recipe's read-set |
| `[scan: <cmd>]` | Separate cheap node producing schedulable edges before the recipe |
| `[scan-format: <fmt>]` | Format of `[scan]` output (default `gcc`) |
| `[writes: manifest <path>]` | Recipe emits a list of its outputs |
| `[writes: trace]` | mk observes the recipe's write-set |
| `[reads: <glob>…]` | Declared read envelope (static bound) |
| `[verify]` | Force hermetic verification for this target |

New recipe variable: `$depfile` — path mk allocates under `.mk/deps/`
for this target's depfile, set only when the rule has a `[deps: …]`
annotation. Sits alongside `$target`, `$input`, `$inputs`, `$stem`,
and `$changed`.

See [`docs/discovered-dependencies.md`](docs/discovered-dependencies.md)
for the full rationale, prior art, and non-goals.

---

## 12. Parallel execution

```
$ mk -j8 test
$ mk -j0 test          # number of CPUs
```

mk builds independent targets concurrently. The dependency graph
determines ordering; siblings in the DAG run in parallel.

Parallel execution respects rule boundaries — a recipe is atomic.
Two recipes never interleave their output. Stdout and stderr from
each recipe are buffered and printed together on completion.

---

## 13. Command-line interface

```
mk [flags] [target...] [var=value...]
```

| Flag | Meaning |
|------|---------|
| `-f FILE` | Read FILE instead of `mkfile` |
| `-j N` | Parallel jobs (0 = number of CPUs) |
| `-v` | Verbose — print recipe commands |
| `-n` | Dry run — print what would be built |
| `-B` | Unconditional rebuild (ignore build database) |

Targets and variable assignments can be intermixed:

```
$ mk cc=clang test:asan -j0
```

If no target is specified, mk builds the first non-task rule.

### Diagnostic flags

| Flag | Meaning |
|------|---------|
| `--why` | Explain why each target is stale |
| `--graph` | Print the dependency subgraph |
| `--state` | Show build database entries |

---

## 14. What's removed

| Make feature | mk stance |
|---|---|
| Tab-only indentation | Any whitespace |
| `$x` as `$(x)` single-char parse | `$name` means `name` |
| `$(func ...)` overloaded for functions and shell | `$[func ...]` for mk functions; `$(...)` is always shell |
| `=` (recursive/lazy by default) | `=` is immediate; `lazy` keyword for deferred |
| `$$` escaping in recipes | Not needed — `$(...)` is shell, `$[...]` is mk |
| Suffix rules (`.c.o:`) | Removed |
| Implicit rules | Removed — use `include std/c.mk` |
| Built-in variables (`CC`, `CFLAGS`) | Removed — use `include std/c.mk` |
| `.PHONY` | `!` prefix |
| `.DELETE_ON_ERROR` | Default behavior |
| `.PRECIOUS` / `.INTERMEDIATE` / `.SECONDARY` | Single `[keep]` annotation |
| `.ONESHELL` | Default behavior |
| `VPATH` / `vpath` | Removed — use explicit paths or scoped includes |
| `$(eval)` | `for` loops + `fn` |
| `define`/`endef` | `fn` |
| `$(call func,$(1),$(2))` | `$[func arg1 arg2]` with named params |
| `$(MAKE)` recursive make | Scoped includes build a single graph — no subprocess boundary |
| Double-colon rules | Removed |
| Archive members `lib(member)` | Removed |
| `-include *.d` / `-MP` ritual | Recipes report what they read; mk records discovered edges in the content-hashed DB (§11). No `.d` files; deleted headers self-heal. |
| `%` (single anonymous stem) | `{name}` (named, multiple) |
| `export` / `unexport` | All variables are environment |
| `override` | Command-line always wins |
| `ifeq ($(X),val)` | `if $X == val` |
| `.RECIPEPREFIX` | Any whitespace |
| `MAKEFLAGS` | `-j` flag, not a variable |

---

## 15. What's kept

| Feature | Notes |
|---|---|
| Dependency DAG execution | Core model unchanged |
| Timestamp-free staleness | Upgraded: content hashing replaces mtime |
| Pattern rules | `{name}` replaces `%`, but same concept |
| Parallel execution (`-j`) | Same |
| `@` / `-` recipe prefixes | Same |
| `$[wildcard]`, `$[shell]`, `$[patsubst]` | `$[...]` syntax, same semantics |
| `include` | Extended with `as` scoping, path rebasing, pattern discovery |
| `-n` dry run | More accurate with build database |
| Command-line variable overrides | Same: `mk cc=clang` |
| Substitution references | `$var:.c=.o` |
| Order-only prerequisites | Same `\|` syntax: `target: prereqs \| order-only` |
| Multi-output rules | Same syntax, explicit grouping semantics |

---

## 16. Example: full project

```
# C++ project with tests, benchmarks, sanitizer support

include std/cxx.mk

cxx = c++ -std=c++17 -stdlib=libc++
cxxflags = -O2 -g -Wall -Wextra
ldflags =
ldlibs =
builddir = build

includes = -Iinclude -Ithird_party

config debug:
    excludes release
    cxxflags += -O0 -DDEBUG

config release:
    excludes debug
    cxxflags += -O2 -DNDEBUG

config asan:
    excludes tsan
    cxxflags += -fsanitize=address,undefined -fno-omit-frame-pointer
    ldflags += -fsanitize=address,undefined

config tsan:
    excludes asan
    cxxflags += -fsanitize=thread
    ldflags += -fsanitize=thread

config dist:
    requires dist
    csp_include = dist
    includes = -Ithird_party

# --- Sources ---

lib_srcs = src/csp.cc src/channel.cc src/runtime.cpp \
           src/reactor.cc src/stack_pool.cc
test_srcs = test/main.cc $[wildcard test/*.test.cc]
bench_srcs = $[wildcard bench/*.bench.cc]

lib_objs = $[patsubst %.cc,$builddir/%.o,$[patsubst %.cpp,$builddir/%.o,$lib_srcs]]
test_objs = $[patsubst %.cc,$builddir/%.o,$test_srcs]
bench_objs = $[patsubst %.cc,$builddir/%.o,$bench_srcs]

# --- Rules ---

$builddir/src/{name}.o: src/{name}.cc
    $cxx $cxxflags $includes -c $input -o $target

$builddir/src/{name}.o: src/{name}.cpp
    $cxx $cxxflags $includes -c $input -o $target

$builddir/test/{name}.o: test/{name}.cc
    $cxx $cxxflags $includes -Itest -c $input -o $target

$builddir/bench/{name}.o: bench/{name}.cc
    $cxx $cxxflags $includes -c $input -o $target

$builddir/csp_tests: $lib_objs $test_objs
    $cxx $cxxflags $ldflags -o $target $inputs $ldlibs

$builddir/csp_bench: $lib_objs $bench_objs
    $cxx $cxxflags $ldflags -o $target $inputs $ldlibs

# --- Tasks ---

!test: $builddir/csp_tests
    ./$input

!bench: $builddir/csp_bench
    ./$input

!dist:
    python3 scripts/amalgamate.py

!test-dist: test test:dist

!clean:
    rm -rf build build-* dist .mk/
```

```
$ mk                     # build + run tests
$ mk test:asan           # ASan + UBSan
$ mk test:debug+asan     # debug + ASan
$ mk test:dist           # test distribution build
$ mk bench:release -j0   # release benchmarks, all cores
$ mk clean               # remove everything
$ mk --why build/src/csp.o # explain why it's stale
```
