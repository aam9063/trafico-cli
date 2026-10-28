#!/usr/bin/env bash
# check.sh is the single source of truth for the "green" gate of trafico-cli.
# The Makefile and CI both delegate here; the linter list is defined only in
# this file.
#
# Usage: scripts/check.sh [--strict]
#   --strict  treat a missing optional tool (staticcheck, govulncheck) as a
#             failure instead of a skip.
#
# Runs on Linux, macOS and Git Bash on Windows. Does not require make.
set -euo pipefail

strict=false
for arg in "$@"; do
  case "$arg" in
    --strict) strict=true ;;
    *)
      echo "check.sh: unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

# Always run from the repository root, whatever the caller's cwd is.
cd "$(dirname "$0")/.."

step() { echo "==> $*"; }

skip_or_fail() {
  local tool="$1"
  if [ "$strict" = true ]; then
    echo "FAIL: $tool is required with --strict (CI installs it)" >&2
    exit 1
  fi
  echo "SKIP: $tool not installed (CI installs it)"
}

step "gofmt"
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt found unformatted files:" >&2
  echo "$unformatted" >&2
  exit 1
fi

step "go build ./..."
go build ./...

step "go vet ./..."
go vet ./...

if command -v staticcheck >/dev/null 2>&1; then
  step "staticcheck ./..."
  staticcheck ./...
else
  skip_or_fail staticcheck
fi

if command -v govulncheck >/dev/null 2>&1; then
  step "govulncheck ./..."
  govulncheck ./...
else
  skip_or_fail govulncheck
fi

step "go test ./..."
go test ./...

echo "==> check: OK"
