#!/usr/bin/env bash
# demo.sh builds the flywheel CLI, scaffolds a throwaway project, and prints a
# short deterministic transcript of real command output to stdout. The temp
# project path is masked as ~/demo so screenshots never leak a local path.
#
#   bash scripts/demo.sh              # full transcript
#   bash scripts/demo.sh --part init  # only the version + init blocks
#   bash scripts/demo.sh --part log-state  # only the log + state blocks
set -euo pipefail

part="all"
if [ "${1:-}" = "--part" ]; then
  part="${2:-all}"
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

build_dir="$(mktemp -d)"
proj="$(mktemp -d)"
trap 'rm -rf "$build_dir" "$proj"' EXIT

# Build the CLI from this tree, stamping the real version (git describe) into
# it. Build chatter goes to stderr, never into the transcript.
go build -o "$build_dir/flywheel" -ldflags "-X main.version=$(git describe --tags --always --dirty)" ./cmd/flywheel 1>&2
if [ -f "$build_dir/flywheel.exe" ]; then
  fw="$build_dir/flywheel.exe"
else
  fw="$build_dir/flywheel"
fi

# A fresh project. The CLI prints the project's absolute path (with Windows
# separators under Git Bash); mask it to ~/demo before anything is printed.
if command -v cygpath >/dev/null 2>&1; then
  proj_abs="$(cygpath -w "$proj")"
else
  proj_abs="$proj"
fi
mask_target="$(printf '%s\\demo' "$proj_abs" | tr '\\' '/')"
mask_repl='~/demo'

# mask rewrites one transcript line: normalize path separators, then swap the
# temp project path for ~/demo (literal substring, no regex surprises).
mask() {
  while IFS= read -r line; do
    line="$(printf '%s' "$line" | tr '\\' '/')"
    printf '%s\n' "${line//$mask_target/$mask_repl}"
  done
}

say() { printf '$ %s\n' "$*"; }

# log_event prints one flywheel log invocation (command plus the JSON event fed
# on stdin) and then runs it, so the transcript shows exactly what was logged.
log_event() {
  say "flywheel log --dir demo --json - <<'EOF'"
  printf '> %s\n' "$1"
  printf '> %s\n' 'EOF'
  printf '%s\n' "$1" | "$fw" log --dir demo --json -
}

run_version() {
  say "flywheel version"
  "$fw" version
}

run_init() {
  say "flywheel init --dir demo"
  "$fw" init --dir demo
}

run_log_state() {
  log_event '{"ts":"2026-09-12T09:00:00.000Z","task":"T1","kind":"planned","brief":".flywheel/briefs/T1.txt","needs":["T0"],"owns":["docs/assets/"]}'
  log_event '{"ts":"2026-09-12T09:10:00.000Z","task":"T1","kind":"dispatched","session":"sess-01H2J3K4L5M6N7P","model":"openrouter/deepseek/deepseek-v4-flash-0731","attempt":"r1"}'
  log_event '{"ts":"2026-09-12T09:40:00.000Z","task":"T1","kind":"finished","rc":0,"session":"sess-01H2J3K4L5M6N7P","attempt":"r1","reason":"done"}'
  say "flywheel state --dir demo"
  "$fw" state --dir demo
}

{
  cd "$proj"
  case "$part" in
    all|init) run_version; run_init ;;
  esac
  case "$part" in
    all|log-state) run_log_state ;;
  esac
} | mask