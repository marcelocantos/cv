# Discovered dependencies

**Status:** rationale companion to DESIGN.md §11. The spec lives in
DESIGN.md; this doc carries the longer-form *why* — motivation,
mechanism comparison, prior art, and non-goals — that the spec
references.

The feature makes dependency discovery a first-class part of cv's
model, superseding the Make pastiche of `clang -MMD`, `-include *.d`,
and `-MP` empty-target hacks.

---

## 1. The problem

A C compile reads headers that nobody declared. Make's answer is a
two-phase ritual:

```make
%.o: %.c
	$(CC) -MMD -c $< -o $@
-include $(OBJS:.o=.d)
```

The compiler emits `foo.d` (`foo.o: foo.h …`) as a byproduct; `-include`
reads those rules back into the graph on the *next* run. It works, but it
is unsound in four distinct ways:

1. **It lags.** On a cold build no `.d` exists, so Make compiles with
   zero header knowledge; the graph that decides what to build is always
   one build behind reality.
2. **Deleted files crash it.** A stale `foo.d` still asserts `foo.o:
   foo.h`. Delete `foo.h` (and its `#include`s) and Make dies —
   `No rule to make target 'foo.h'` — *before* reaching the recompile
   that would refresh `foo.d`. The `-MP` flag papers over this by
   emitting an empty phony rule per header.
3. **It is tool-specific.** `-MMD` is a gcc/clang flag. protoc imports,
   sass `@use`, bundlers, and codegen each need their own mechanism or
   get none.
4. **Untracked reads are silent corruption.** A tool that reads a file no
   depfile captures yields stale builds with no warning.

The root cause of (2) is structural: Make stores discovered deps as
**hard, ordering-bearing prerequisites**, and a prerequisite with no file
and no rule is fatal. The stale edge from the previous build poisons this
build before the recipe that would heal it can run.

---

## 2. Core model: hard and soft edges

Every dependency edge is one of two kinds.

| Edge | Source | Constrains | Required to exist? |
|---|---|---|---|
| **Hard** | Declared in the cvfile (`a: b`) | Ordering **and** staleness | Yes — it is an ordering constraint |
| **Soft** | Discovered by running the recipe | Staleness only | No — absence is just "changed" |

A hard edge is a promise about build *order*: `b` must be built before
`a`'s recipe runs. A soft edge cannot constrain order, because by
definition you do not learn it until *after* you have run the recipe. It
can only invalidate.

This single distinction is the whole design. It is also the precise frame
that *Build Systems à la Carte* (Mokhov, Mitchell & Peyton Jones, ICFP
2018) formalizes: dynamic dependencies are exactly the edges a
topological scheduler cannot see up front, which is why discovering them
must be invalidation-only.

### The authoring rule

> **Declare in-graph targets. Discover leaves.**

An edge **must** be hard if and only if the path is an *in-graph target* —
some rule can produce it. Everything else (a leaf with no producing rule)
is safe to discover.

The criterion is **structural, not provenance-based.** Whether a file is
checked into the repo, and whether it was once emitted by some out-of-band
generator, are both irrelevant. Committed generated artefacts (vendored
protobuf output, checked-in parsers, lockfiles) are common and
legitimate; committedness tells you nothing about whether *this* build
produces the file.

- **Has a producing rule** → hard edge required. At read-time the file
  might be absent or stale, so the consumer must order after it. Content
  hashing still rebuilds it correctly if its own inputs changed; the
  consumer waits.
- **No producing rule** → leaf → discover it like any other source.

Relying on "it's committed, so it's present" is an active trap: a
discover-only build serves a *stale* committed-generated file whenever
that file's inputs changed. The structural rule is what makes it sound.

Because cv knows its own target set, this rule is **enforced, not just
advised** (see §7).

---

## 3. The staleness contract

Two rules make the model airtight and deliver the deleted-file behaviour
Make never gets right:

1. **A recorded soft-dep that has vanished counts as "changed," never as
   "missing input."** This is the entire fix for deleted files. A missing
   discovered input means the recorded input set no longer matches reality
   → the target is stale → rebuild. The rebuild's recipe records a fresh
   read-set that omits the deleted file; the stale edge is gone. The edge
   *triggers the very rebuild that erases it* — it is self-healing, with
   no `-MP`, no empty phony rules, no `.d` files in the tree.

2. **Replace the discovered set wholesale on each successful run — never
   union.** Unioning lets stale edges accumulate and slowly reintroduces
   Make's problem. Wholesale replacement guarantees the recorded set
   always reflects the *last actual* execution.

The model also gives the right answer in the case people confuse with the
above: delete `foo.h` but *leave* an `#include` of it, and the rerun's
compile genuinely fails — cv surfaces that compiler error, because it
always defers to the recipe's own exit status rather than pre-judging from
a stale edge. Deletion-with-dangling-ref is a real error;
deletion-with-refs-also-removed self-heals. Same mechanism, both correct.

---

## 4. Mechanisms

Discovery is **one capability with three producers.** They differ only in
*who* learns the read-set and *when*.

### 4.1 Execute-and-record (the universal fallback)

Run the recipe, learn what it touched, record it. This needs no analysis
pass and is the default for everything.

**Depfile adapter** — generalizes `-MMD`. The recipe emits a dependency
list as a byproduct; cv parses it, normalizes to project-relative paths,
folds it into the build database (§5), and discards the file:

```
build/{name}.o [deps: gcc]: src/{name}.c
    $cc $cflags -MMD -MF $depfile -c $input -o $target
```

`[deps: <format>]` names a parser; `$depfile` expands to a path cv
allocates under `.cv/`. Supported formats: `gcc`/`makefile` (Makefile
depfile syntax), `msvc` (`/showIncludes`), `json` (array of paths),
`lines` (newline- or NUL-separated). This is strictly better than
`-include`: cv owns the parse, folds into a content-hashed DB instead of
replaying stale rules, and the file never enters the source tree. It is
also exactly what Ninja's `deps = gcc` does — the closest mainstream prior
art — with content hashing added on top.

**Trace** — observe the recipe's actual file accesses with zero tool
cooperation:

```
build/{name}.o [deps: trace]: src/{name}.c
    $cc $cflags -c $input -o $target
```

cv runs the recipe under observation (macOS: sandbox profile / `fs_usage`
/ `DYLD` interpose; Linux: seccomp-bpf, ptrace, or `fanotify`) and records
every path opened for read. Works for any tool — protoc, sass, bundlers,
codegen — not just compilers. This is tup/Bazel territory; it is
platform-specific and slower than a depfile, so it is **opt-in**, not the
default. Trace is also what powers verification (§7).

### 4.2 Scan nodes (the optional two-phase escape hatch)

Soft edges are invalidation-only, so they cannot inform *this* build's
schedule — except via a cheap pre-pass. A **scan node** is a separate,
lightweight recipe whose output *is* the dependency set, scheduled before
the heavy recipe and fed to the scheduler:

```
build/{name}.o [scan: cc -M $cflags $input]: src/{name}.c
    $cc $cflags -c $input -o $target
```

The scan command's output (in `[scan-format: …]`, default `gcc`) becomes
schedulable soft edges before `$cc -c` runs. Crucially, **a scan node is a
first-class graph node**: it is scheduled like any other target, it has
its own (declarable and discoverable) dependencies — including any
generated headers it must read through, which forces the correct
scan→generate→re-scan interleaving for free — and "two-phase" *emerges*
wherever a scan node exists rather than being a second execution mode.

This is the amplification-shader pattern from GPU pipelines: a cheap stage
that computes fan-out before the expensive stage. It earns its cost only
where the graph is large enough that mis-scheduling a late-discovered dep
causes a long serial tail, or under **remote execution**, where an
action's inputs must be known before it ships off-machine. For everything
else, §4.3 makes it unnecessary.

### 4.3 Why execute-and-record is the spine, not two-phase

Two-phase analysis-then-execution is the right *capability* but the wrong
*default*, because the common case already has the analysis result for
free:

**The previous build's recorded soft-edges *are* the analysis pass.** On
an incremental build, last build's discovered set is an
exact-as-of-last-build approximation of the dependency shape. cv schedules
using it, executes, and re-records. This is safe even when the shape has
drifted, because **correctness comes from the post-hoc content-hash check
plus the wholesale re-record, never from the schedule.** Scheduling on a
stale recorded edge can only produce a slightly suboptimal *parallel*
schedule that self-corrects next run; it can never miss a rebuild.

So live analysis adds nothing on incremental builds (the record is
already there) and little on cold builds (everything is being built
anyway; the critical path runs through *declared* edges). Scan nodes are a
targeted optimization for the narrow window where shape-before-execution
genuinely pays — not the engine's spine.

---

## 5. Build database representation

The build database (`.cv/`, see DESIGN.md §7) already stores, per target,
the declared prerequisite set, expanded recipe text, and per-input content
fingerprints. Discovered dependencies add:

- **Discovered input set** — the soft edges from the last successful run,
  stored alongside the declared set, each with its content fingerprint at
  that time.
- **Discovered output set** — for dynamic outputs (§6).

Staleness for a target is now: declared set changed **or** discovered set
changed (including a member that vanished, per §3) **or** any input
fingerprint (declared or discovered) changed **or** recipe text changed
**or** an output fingerprint changed.

Everything is **partitioned by config** exactly as the existing database
is — `build-debug-asan` and `build-release` keep independent discovered
sets, because a compile under `-DDEBUG` may include different headers.

No `.d` files, no `-include` glob, no `-MP`. `cv clean` removes the
discovered sets with everything else under `.cv/`.

---

## 6. Dynamic outputs (the symmetric case)

A recipe may also produce a set of outputs not known statically — a
codegen step that emits N files from one input. The same machinery,
mirrored:

```
gen/ [writes: manifest gen/.manifest]: schema.idl
    idlc --emit-manifest gen/.manifest schema.idl
```

`[writes: manifest <path>]` reads a producer-emitted list of outputs;
`[writes: trace]` observes them. cv records the discovered output set and
fingerprints each, so downstream consumers and `cv clean` see the real
artefacts. This closes the model: discovery is symmetric across inputs and
outputs.

---

## 7. Verification (hermetic mode)

Under trace, cv knows the *complete* read/write set, so it can assert the
build is correctly specified — something Make cannot do at all:

```
cv --verify        # or per-target [verify] annotation
```

cv flags:

- **Undeclared reads of an in-graph target** — a recipe read a path *this
  build also produces* but did not declare it as a prerequisite. This is
  the single most valuable check: it is a latent ordering race that
  happened to schedule correctly this time. (Note it has nothing to do
  with whether the file is committed — see §2.)
- **Undeclared writes** — output not in the declared or discovered output
  set; graph pollution.
- **Reads outside a declared envelope** — see below.

Because cv knows its own target set, the in-graph-read check needs no
configuration: any discovered read whose path matches a known target or
pattern is either auto-promoted to a hard edge or reported, per policy.

### Declared envelopes

A recipe may bound its dynamism without enumerating it — the build
analogue of a geometry shader's `max_vertices` declaration:

```
build/{name}.o [reads: include/** src/**]: src/{name}.c
    …
```

The envelope is a static *bound* on what the recipe may read. It turns
unbounded discovery into bounded discovery: the sandbox can enforce it,
the verifier can check against it, and a remote scheduler can pre-stage
it. Optional; absence means "no bound asserted."

Run verification in CI as a gate; leave it off for fast local iteration.

---

## 8. Syntax summary

Annotations attach in `[...]` before the rule's colon, consistent with the
existing `[fingerprint: …]` and `[keep]` annotations. Multiple annotations
use multiple brackets.

| Annotation | Meaning |
|---|---|
| `[deps: gcc\|makefile\|msvc\|json\|lines]` | Recipe emits a depfile at `$depfile`; cv folds it post-run |
| `[deps: trace]` | cv observes the recipe's read-set |
| `[scan: <cmd>]` | Separate cheap node producing schedulable edges before the recipe |
| `[scan-format: <fmt>]` | Format of `[scan]` output (default `gcc`) |
| `[writes: manifest <path>]` | Recipe emits a list of its outputs |
| `[writes: trace]` | cv observes the recipe's write-set |
| `[reads: <glob>…]` | Declared read envelope (a static bound) |
| `[verify]` | Force hermetic verification for this target |

New recipe variable: `$depfile` — the path cv allocates under
`.cv/deps/` for this target's depfile, set only when the rule has a
`[deps: …]` annotation. Sits alongside `$target`, `$input`, `$inputs`,
`$stem`, and `$changed`.

`std/c.cv` and `std/cxx.cv` carry the `[deps: gcc]` annotation, so a
project that does `include std/c.cv` gets correct, self-healing header
tracking with no `.d` ritual visible anywhere.

---

## 9. Interaction with existing features

- **Configs** — discovered sets are per-config; already handled by the
  config-partitioned database (§5).
- **Multi-output rules** — discovered reads attach to the single rule
  invocation; the declared output grouping is unchanged, and `[writes:
  trace]` may extend it.
- **Pattern rules / captures** — orthogonal; discovery operates on the
  resolved target, after captures bind.
- **Scoped includes** — discovered paths are normalized to the global
  graph's coordinate space (the same rebasing applied to declared
  prerequisites), so a child cvfile's discovered deps merge correctly.

---

## 10. Non-goals

Explicitly rejected:

- **Committed-vs-generated as the edge-kind criterion.** Wrong axis (§2);
  the criterion is whether a producing rule exists.
- **Mandatory tracing.** Trace is opt-in. The depfile adapter is the
  portable, stable default.
- **Two-phase as the engine's spine.** Record-and-reuse is the spine;
  scan nodes are an opt-in optimization (§4.3).
- **Arbitrary graph mutation from recipes.** Discovered edges are
  monotonic and content-addressed, hence safe. Letting recipes rewrite the
  graph at eval time is the road to Make's `$(eval)` / secondary-expansion
  madness. Graph *queries* (`cv why`, `cv deps`, `cv rdeps`) are welcome;
  graph *rewriting* is not.

---

## 11. Prior art

| Idea | Realized in |
|---|---|
| Fold depfiles into a binary DB instead of re-`include` | **Ninja** (`deps = gcc`/`msvc`, `.ninja_deps`) |
| Trace execution to learn the read-set | **tup** (Shal 2009), `memoize`/`fabricate`, **Bazel**/**Buck** sandboxing |
| Dependencies discovered by running the recipe | **Shake** (Mitchell, ICFP 2012) |
| Absence/nonexistence as a first-class dependency | **redo** (djb / apenwarr), **Vesta** (Heydon et al., PLDI 2000) |
| Verify declared vs actual inputs | **Bazel** C++ include validation |
| Two axes: dynamic deps × scheduler (topological/restarting/suspending) | *Build Systems à la Carte* (Mokhov et al., ICFP 2018) |
| Bound dynamic fan-out so it stays schedulable | GPU geometry/amplification shaders (`max_vertices`); two-phase = mesh+amplification split |

cv's contribution is synthesis, not invention: Ninja-style depfile folding
*and* tup/Bazel-style tracing behind one `[deps: …]` annotation, on a
content-hash staleness engine, with the hard/soft edge distinction
packaged as an enforced authoring rule, and Vesta/redo's
absence-as-dependency correctness inherited for free from prereq-set
diffing.

