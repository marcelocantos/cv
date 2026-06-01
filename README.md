# cv

A build tool with Make's dependency-graph model, minus 48 years of
accumulated pain.

cv keeps what works — dependency DAGs, parallel execution, only stale
targets rebuilt — and fixes what doesn't: content hashing instead of
timestamps, clean syntax, no tab-vs-space traps, no `$$` escaping, no
implicit rules.

See [DESIGN.md](DESIGN.md) for the full specification. There is also an
[agents guide](agents-guide.md) optimized for AI coding assistants.

## Why cv?

Make's core model is excellent. The rest of it is a source of friction:
timestamps lie after `git checkout` and CI cache restores, `$$` escaping
trips everyone up, incremental builds break when you change a flag or
delete a source file, and recursive make hides dependencies across
directories. cv fixes all of this — content hashing, clean syntax,
a build database that tracks everything, and a single dependency graph
across the whole project — while keeping the model that works.

[See the detailed analysis.](docs/why-cv.md)

## Install

```
go install github.com/marcelocantos/cv/cmd/cv@latest
```

## Quick start

Create a file called `cvfile`:

```
greeting = world

hello.txt: name.txt
    echo "Hello, $(cat $input)!" > $target

name.txt:
    echo $greeting > $target
```

Build it:

```
$ cv
$ cat hello.txt
Hello, world!
```

Only stale targets rebuild. Change `greeting` in the cvfile and run `cv`
again — both targets rebuild because the recipe changed. Change
`name.txt` by hand — only `hello.txt` rebuilds because cv tracks content
hashes, not timestamps.

## Key differences from Make

| Make | cv |
|---|---|
| Tabs required | Any whitespace |
| `$@`, `$<`, `$^` | `$target`, `$input`, `$inputs` |
| `$(func ...)` overloaded | `$[func ...]` for cv, `$(...)` for shell |
| `$$` in recipes | Not needed — `$(...)` is always shell |
| `.PHONY: clean` | `!clean:` |
| Timestamp-based | Content hash-based |
| Implicit rules | `include std/c.cv` (opt-in) |
| `%` patterns | `{name}` named captures |
| `.DELETE_ON_ERROR` | Default behaviour |
| `.ONESHELL` | Default behaviour |
| `-include *.d` / `-MP` ritual | `[deps: gcc]` annotation; deleted headers self-heal |

## Mini tutorial

### Variables

```
cc = gcc
cflags = -Wall -O2
cflags += -Werror
```

All assignments are immediate. Use `lazy` for deferred evaluation:

```
lazy version = $[shell git describe --tags]
```

### Rules and patterns

```
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
```

Named captures (`{name}`) replace Make's `%`. Parent directories of
targets are created automatically. The entire recipe runs as one
`sh -c` invocation with `set -e`.

### Tasks

```
!test: build/app
    ./$input --self-test

!clean:
    rm -rf build/ .cv/
```

The `!` prefix means "always run, this isn't a file."

### Configs

```
config debug:
    cflags += -O0 -g
end

config release:
    excludes debug
    cflags += -O2 -DNDEBUG
end
```

```
$ cv test:debug          # debug build
$ cv test:debug+asan     # compose configs
```

### Parallel builds

```
$ cv -j0 test            # all available cores
$ cv -j8 test            # 8 jobs
```

### Diagnostics

```
$ cv --why build/app     # explain why a target is stale
$ cv --graph build/app   # print dependency graph (DOT format)
$ cv -n test             # dry run
```

### Discovered dependencies (no `-MMD` ritual)

`include std/c.cv` gets correct header tracking with no `-include *.d`
and no `-MP` ceremony. The standard library annotates the compile rule
with `[deps: gcc]`; cv hands the compiler a depfile path in `$depfile`,
reads it after each compile, folds the discovered prereqs into the
content-hashed build database, then deletes the depfile. Deleted
headers self-heal — a missing recorded header is "changed," not
"missing input," so the very rebuild it triggers also drops the stale
edge.

For tools without a depfile, an `[scan: <cmd>]` pre-pass or
`[deps: trace]` (Linux only, via strace) provides the same model. See
[DESIGN.md §11](DESIGN.md#11-discovered-dependencies) for the full
semantics, [`docs/discovered-dependencies.md`](docs/discovered-dependencies.md)
for the rationale and prior art.

## Flags

| Flag | Meaning |
|------|---------|
| `-f FILE` | Read FILE instead of `cvfile` |
| `-j N` | Parallel jobs (`-1`=auto, `0`=all cores) |
| `-v` | Verbose |
| `-n` | Dry run |
| `-B` | Unconditional rebuild |
| `--why` | Explain staleness |
| `--graph` | Print dependency subgraph |
| `--state` | Show build database entries |
| `--verify` | Error on undeclared reads of in-graph targets (DESIGN.md §11) |

## License

Apache 2.0 — see [LICENSE](LICENSE).
