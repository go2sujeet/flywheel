#!/usr/bin/env bash
# Test harness for scripts/next-version.sh against throwaway git repos.
# Prints PASS/FAIL per case, exits nonzero if any case fails, cleans up temp dirs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NEXT_VERSION="$SCRIPT_DIR/next-version.sh"

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

fail=0

new_repo() {
	local repo="$ROOT/$1"
	mkdir -p "$repo"
	git -C "$repo" init -q
	git -C "$repo" config user.name test
	git -C "$repo" config user.email test@example.com
	git -C "$repo" commit --allow-empty -q -m "chore: initial"
	printf '%s' "$repo"
}

mkcommit() {
	local repo="$1" subject="$2" body="${3:-}"
	if [ -n "$body" ]; then
		git -C "$repo" commit --allow-empty -q -F - <<EOF
$subject

$body
EOF
	else
		git -C "$repo" commit --allow-empty -q -m "$subject"
	fi
}

mktag() {
	git -C "$1" tag "$2"
}

expect() {
	local name="$1" expected="$2" got="$3"
	if [ "$expected" = "$got" ]; then
		echo "PASS $name"
	else
		echo "FAIL $name: expected [$expected] got [$got]"
		fail=1
	fi
}

run_script() {
	local repo="$1"
	(cd "$repo" && "$NEXT_VERSION" 2>/dev/null)
}

repo="$(new_repo a)"
mktag "$repo" v0.1.0
mkcommit "$repo" "fix: x"
expect "a. patch bump" "v0.1.1" "$(run_script "$repo")"

repo="$(new_repo b)"
mktag "$repo" v0.1.0
mkcommit "$repo" "feat: y"
mkcommit "$repo" "fix: z"
expect "b. feat wins over fix" "v0.2.0" "$(run_script "$repo")"

repo="$(new_repo c)"
mktag "$repo" v0.1.0
mkcommit "$repo" "feat!: drop x"
expect "c. breaking pre-1.0 is minor" "v0.2.0" "$(run_script "$repo")"

repo="$(new_repo d)"
mktag "$repo" v1.2.3
mkcommit "$repo" "feat(api)!: x"
expect "d. breaking post-1.0 is major" "v2.0.0" "$(run_script "$repo")"

repo="$(new_repo e)"
mktag "$repo" v0.1.0
mkcommit "$repo" "chore: x"
mkcommit "$repo" "ci: y"
expect "e. non-release types skip" "" "$(run_script "$repo")"

repo="$(new_repo f)"
mkcommit "$repo" "feat: init"
expect "f. no tag, feat from scratch" "v0.1.0" "$(run_script "$repo")"

repo="$(new_repo g)"
mktag "$repo" v0.1.0
mkcommit "$repo" "fix: z" "BREAKING CHANGE: gone"
expect "g. breaking body pre-1.0 is minor" "v0.2.0" "$(run_script "$repo")"

repo="$(new_repo h)"
mktag "$repo" v0.1.0
mkcommit "$repo" "docs: y"
expect "h. docs is patch" "v0.1.1" "$(run_script "$repo")"

repo="$(new_repo i)"
mktag "$repo" v0.1.0
expect "i. no commits since tag" "" "$(run_script "$repo")"

repo="$(new_repo j)"
mktag "$repo" v0.1.0
mkcommit "$repo" "Merge pull request #5 from a/feat/b"
expect "j. merge commit skips" "" "$(run_script "$repo")"

repo="$(new_repo k)"
mktag "$repo" v0.1.0
mkcommit "$repo" "fix(init): x (#6)"
expect "k. scoped fix is patch" "v0.1.1" "$(run_script "$repo")"

if [ "$fail" -ne 0 ]; then
	echo "SOME TESTS FAILED" >&2
	exit 1
fi

echo "ALL TESTS PASSED"
exit 0