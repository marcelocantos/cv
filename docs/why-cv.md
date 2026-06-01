# Why cv?

Make's execution model — declare a dependency DAG, run recipes to
produce targets from prerequisites, rebuild only what's stale — is one
of the best ideas in software engineering. It has survived forty-eight
years because the core abstraction is right.

Everything else about Make is a source of friction. cv keeps the model
and fixes the rest.

## At a glance

- **Content hashing** instead of timestamps — no phantom rebuilds after
  `git checkout`, CI cache restores, or archive extraction.
- **Clean, readable syntax** — any indentation, `$target` instead of
  `$@`, no `$$` escaping in recipes.
- **A build database** that tracks recipe text, prerequisite sets, and
  content hashes — if anything relevant changes, the target rebuilds.
- **A single dependency graph** across the whole project — no recursive
  make, no subprocess boundaries, correct incrementals everywhere.

The sections below go into detail on each of these and more.

---

## Timestamps lie

Make decides whether to rebuild a target by comparing the modification
time of the target against those of its prerequisites. If any
prerequisite is newer, the target is stale. This is fast and simple, but
it breaks constantly in practice.

**git operations.** `git checkout`, `git rebase`, `git stash pop`, and
`git bisect` all rewrite files with timestamps that reflect when the
checkout happened, not when the content last changed. After switching
branches, Make often either rebuilds everything (wasting time) or
rebuilds nothing (producing wrong results), depending on the direction
of the timestamp change.

**CI and caching.** CI systems routinely restore build caches, download
artifacts, or mount shared volumes. The timestamps on restored files
bear no relation to the build that produced them. This leads to either
unnecessary full rebuilds (cache miss on every file) or stale artifacts
sneaking through (cache hit on a file whose content actually changed).

**Archive extraction.** `tar xf`, `unzip`, and `rsync` can all produce
files with timestamps that predate or postdate the actual content
change. A common pattern — extract a tarball, then `make` — frequently
triggers either a full rebuild or no rebuild, neither of which is
correct.

**Touch-based workarounds.** The Make ecosystem is littered with
`touch` calls, `.PHONY` markers on things that aren't actually phony,
and recursive-make wrappers that exist solely to paper over timestamp
unreliability. These workarounds add complexity, obscure intent, and
introduce their own bugs.

cv uses SHA-256 content hashes instead. Modify a file then revert it?
The hash matches the recorded value, so nothing rebuilds. Extract
unchanged files from a fresh archive? Same content, same hash, no
rebuild. The only thing that triggers a rebuild is an actual change to
the content that the build depends on.

For performance, cv caches hashes using `(path, mtime, size)` as a
cache key. If a file's metadata hasn't changed, its hash is served
from cache without re-reading the file. This makes staleness checks
nearly as fast as `stat()` in the common case while remaining correct
in all cases.

---

## Syntax

Make's syntax has accumulated decades of special cases. cv replaces
them with a small set of consistent rules.

### Indentation

Make requires tabs for recipe lines. A space where a tab should be is a
silent, invisible error. cv accepts any whitespace — tabs, spaces, or a
mix. Indentation is indentation.

### Variable references

In Make, `$x` means the single-character variable `x`, while `$(foo)`
means the multi-character variable `foo`. This single-character rule is
a constant source of bugs: `$cflags` expands to the value of `c`
followed by the literal text `flags`. cv has no single-character rule:
`$cflags` means the variable `cflags`. Use `${name}` when the variable
is adjacent to other identifier characters: `${prefix}_suffix`.

### Sigil separation

Make overloads `$(...)` for three different purposes: variable
references (`$(CC)`), function calls (`$(patsubst ...)`), and — in
recipes — shell command substitution. This means every literal `$` in a
recipe must be doubled (`$$`), and it's never obvious at a glance
whether `$(...)` is a Make expansion or a shell expansion.

cv assigns each sigil exactly one meaning:

| Syntax | Meaning | Expanded by |
|--------|---------|-------------|
| `$name` / `${name}` | Variable reference | cv |
| `$[func args]` | cv function call | cv |
| `$(...)` | Shell command substitution | shell |

`$(...)` is **never** interpreted by cv. It passes through to the shell
verbatim. This means recipes look like normal shell scripts:

```
build/app: $obj
    commit=$(git rev-parse --short HEAD)
    $cxx -DCOMMIT="\"$commit\"" -o $target $inputs
```

`$cxx`, `$target`, and `$inputs` are cv variables. `$(git ...)` is
shell command substitution. No escaping, no ambiguity.

### Automatic variables

| Make | cv | Meaning |
|------|----|---------|
| `$@` | `$target` | Target being built |
| `$<` | `$input` | First prerequisite |
| `$^` | `$inputs` | All prerequisites |
| `$?` | `$changed` | Changed prerequisites |
| `$*` | `$stem` | Pattern stem |

There is also `$target.dir` and `$target.file` for the directory and
filename parts. No more `$(dir $@)` / `$(notdir $@)`.

### Recipe execution

Each recipe in Make runs each line as a separate shell invocation by
default. A `cd` on one line has no effect on the next. Multi-line shell
logic requires backslash continuations and careful `&&` chaining. Make
added `.ONESHELL` as an opt-in fix, but most projects don't use it.

In cv, the entire recipe block runs as a single `sh -c` invocation with
`set -e`. `cd` persists. Multi-line logic works naturally. This is
always on, not opt-in.

---

## Incremental builds

Make tracks one thing: file timestamps. cv's build database (stored in
`.cv/`) tracks four:

### Recipe text

cv hashes the recipe text after variable expansion. Change `-O2` to
`-O0` in your flags? The recipe hash changes. Rebuild. Change a comment
in the cvfile that doesn't affect any recipe? No rebuild.

Make doesn't track recipe text at all. Changing compiler flags requires
`make clean && make` to take effect reliably.

### Prerequisite sets

cv records which prerequisites a target was built from. If the set
changes — a source file added, removed, or renamed — the target is
stale.

In Make, deleting a source file from the prerequisite list leaves the
old `.o` file in place. The linker happily uses the stale object,
producing a binary that includes code from a file you thought you
removed. The only reliable fix is a clean build.

### Input content

cv records the SHA-256 hash of each prerequisite's content at the time
of the last successful build. Only actual content changes trigger
rebuilds, not metadata changes.

### Output content

cv also records the hash of the target itself. This detects targets
modified outside the build system (e.g., by hand-editing a generated
file) and triggers a rebuild to restore consistency.

### Non-file artifacts

For targets that aren't files (Docker images, database schemas, deployed
services), cv supports a `[fingerprint: command]` annotation. The
command outputs a stable string (an image ID, a schema version, etc.).
If it changes since the last build, the target is stale.

```
app.img [fingerprint: docker inspect --format '{{.Id}}' myapp]: build/app Dockerfile
    docker build -t myapp .
```

Make has no equivalent. Non-file targets must be `.PHONY` (always
rebuild) or use fragile marker-file patterns.

---

## Multi-directory builds

### The recursive make problem

The standard Make approach to multi-directory projects is recursive make:
each directory has its own Makefile, and the top-level Makefile shells
out with `$(MAKE) -C subdir`. This creates fundamental problems:

- **Hidden dependencies.** The top-level make can't see inside the
  sub-makes. If `app/main.o` depends on `lib/libfoo.a`, Make doesn't
  know that — you have to manually encode the ordering, and it's easy
  to get wrong.

- **Broken parallelism.** Because each `$(MAKE)` is an opaque
  subprocess, the top-level `-j` flag doesn't coordinate across
  directories. Either you under-parallelise (sequential sub-makes) or
  you over-parallelise (each sub-make spawns its own `-j8`).

- **Incorrect incrementals.** Without visibility into the full graph,
  Make can't determine the correct global build order. Changes in one
  directory may require rebuilds in another, but recursive make won't
  notice unless you've manually expressed every cross-directory
  dependency.

Peter Miller's 1998 paper [*Recursive Make Considered
Harmful*](https://aegis.sourceforge.net/auug97.pdf) documented these
problems in detail. The recommended fix — a single top-level Makefile
that includes all rules — is correct but awkward in Make because it
offers no scoping or path-rebasing mechanism.

### cv's approach

cv's scoped includes solve this cleanly:

```
include lib/cvfile as lib
include app/cvfile as app
```

Each included cvfile is evaluated with:

- **Variable isolation.** The child's variables live under the alias
  prefix (`$lib.src`, `$lib.obj`). The child inherits parent variables
  as defaults but can't overwrite them.

- **Path rebasing.** Targets and prerequisites are automatically
  prefixed with the child's directory. The child writes `build/foo.o`;
  the global graph sees `lib/build/foo.o`.

- **A single graph.** Everything merges into one dependency DAG. cv
  sees every target, every dependency, every recipe — across the entire
  project. Parallelism, incrementals, and `--why` diagnostics all work
  correctly across directory boundaries.

Pattern discovery (`include {path}/cvfile as {path}`) auto-discovers
subdirectory cvfiles, so adding a new directory to a project is just
creating a `cvfile` in it.

---

## Patterns

Make's `%` wildcard matches a single anonymous stem. You get one capture
per rule, and you can't name it or use multiple captures.

cv uses named captures in braces:

```
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
```

Same name on both sides means values must match. Multiple captures are
supported:

```
build/{arch}/{config}/{name}.o: src/{name}.c
    ${cc_$arch} ${cflags_$config} -c $input -o $target
```

Requesting `build/arm64/release/foo.o` binds `arch=arm64`,
`config=release`, `name=foo`. Capture values are available as variables
in the recipe. Each capture matches within a single path segment (no `/`).

---

## Configs

Build variants (debug, release, sanitizers) are a first-class concept:

```
config debug:
    cxxflags += -O0 -g -DDEBUG
end

config asan:
    cxxflags += -fsanitize=address
    ldflags += -fsanitize=address
end
```

Compose them on the command line with `+`:

```
$ cv test:debug+asan
```

cv auto-derives the build directory (`build` becomes `build-debug-asan`)
and isolates build state per config combination. Configs can declare
mutual exclusion (`excludes release`) and prerequisites (`requires
dist`).

In Make, build variants typically require either duplicated rules,
`$(eval)` metaprogramming, or external wrapper scripts.

---

## Tasks, defaults, and cleanup

cv replaces Make's `.PHONY` with a `!` prefix:

```
!test: build/app
    ./$input --self-test

!clean:
    rm -rf build/ .cv/
```

Tasks always run when requested. If no target is given, cv builds the
first non-task rule (no need for a `.DEFAULT_GOAL`).

If a recipe fails, cv deletes the partial target by default (Make's
`.DELETE_ON_ERROR` behaviour, but always on). The `[keep]` annotation
overrides this for targets that should survive a failed build.

---

## What cv is not

cv is not a package manager, a meta-build system, or a configuration
tool. It doesn't generate Makefiles, CMakeLists, or Ninja files. It
doesn't fetch dependencies, manage toolchains, or abstract across
platforms.

cv is a build tool. It reads a dependency graph, determines what's
stale, and runs recipes to bring targets up to date. It does that one
thing, and it does it correctly.
