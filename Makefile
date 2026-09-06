# Owner gate (think 🎯T8). The pre-push hook (scripts/hooks/pre-push) runs
# `make gate` before every push and refuses the push when it is red; CI
# (.github/workflows/ci.yml) runs the same target on master and on PRs.
#
# This repo's own build file is `cvfile` — cv builds itself. The gate does
# NOT go through cv: a bootstrap gate that needs an installed cv cannot
# refuse a push that breaks cv. Recipes here call the Go toolchain directly
# and mirror the tasks in std/go.cv.
.PHONY: gate vet test bullseye hooks

gate: vet test

vet:
	go vet ./...

test:
	go test ./... -count=1

# Wire the pre-push gate. A relative core.hooksPath resolves against
# whichever worktree the push runs from, so one setting covers them all.
hooks:
	@git config core.hooksPath scripts/hooks && chmod +x scripts/hooks/* && \
	 echo "✓ core.hooksPath=scripts/hooks (pre-push runs make gate)"

# Standing invariants checked by bullseye_convergence.
bullseye: gate
	@test -z "$$(git status --porcelain)" && echo "✓ clean" || \
	 (echo "✗ dirty tree"; git status --short; exit 1)
