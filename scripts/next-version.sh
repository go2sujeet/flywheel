#!/usr/bin/env bash
# Compute the next release tag from conventional commits since the last vX.Y.Z tag.
# Usage: scripts/next-version.sh  (run inside a git repo)
# stdout: next tag (e.g. v0.1.1) when a release is warranted, nothing otherwise.
# stderr: one line explaining the decision. Exit 0 either way; nonzero on error.
set -euo pipefail

is_breaking_subject() {
	printf '%s\n' "$1" | grep -Eq '^[A-Za-z0-9_]+(\[[^]]*\])?(\([^)]*\))?!:'
}

is_breaking_body() {
	[ -z "$1" ] && return 1
	printf '%s\n' "$1" | grep -Eq '^BREAKING CHANGE:'
}

is_feat() {
	printf '%s\n' "$1" | grep -Eq '^feat(\[[^]]*\])?(\([^)]*\))?!?:'
}

is_patch_type() {
	printf '%s\n' "$1" | grep -Eq '^(fix|perf|docs|refactor|revert)(\[[^]]*\])?(\([^)]*\))?!?:'
}

base="$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)"

if [ -z "$base" ]; then
	major=0
	minor=0
	patch=0
	rev_list="$(git rev-list HEAD)"
	base_note="no version tag reachable; base v0.0.0"
else
	IFS=. read -r major minor patch <<EOF
${base#v}
EOF
	rev_list="$(git rev-list "$base..HEAD")"
	base_note="base $base"
fi

bump=none

if [ -n "$rev_list" ]; then
	while IFS= read -r commit; do
		msg="$(git log -1 --format=%B "$commit")"
		subject="$(printf '%s\n' "$msg" | sed -n '1p')"
		body="$(printf '%s\n' "$msg" | sed -n '2,$p')"

		if is_breaking_subject "$subject" || is_breaking_body "$body"; then
			bump=major
		elif [ "$bump" != major ] && is_feat "$subject"; then
			bump=minor
		elif [ "$bump" = none ] && is_patch_type "$subject"; then
			bump=patch
		fi
	done <<< "$rev_list"
fi

case "$bump" in
major)
	if [ "$major" -eq 0 ]; then
		minor=$((minor + 1))
		patch=0
	else
		major=$((major + 1))
		minor=0
		patch=0
	fi
	;;
minor)
	minor=$((minor + 1))
	patch=0
	;;
patch)
	patch=$((patch + 1))
	;;
none)
	echo "no release: $base_note, no qualifying commits" >&2
	exit 0
	;;
esac

echo "release: v$major.$minor.$patch ($base_note, bump: $bump)" >&2
echo "v$major.$minor.$patch"