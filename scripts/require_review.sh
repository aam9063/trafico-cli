#!/usr/bin/env bash
# require_review.sh — mechanical review gate (docs/ARRANQUE.md §10.5 / §12).
#
# A pull request whose title names a backlog task ID must carry its review
# report at odd/reviews/<ID>.md. The gate is deliberately mechanical: it can
# only check that the report exists, has the required headings, and is not
# empty. It cannot judge the quality of the review, and it does not pretend to.
#
# Usage: require_review.sh "<pr title>"
#
# Task ID pattern (one or more digits). The title is uppercased before matching,
# so `ops-99` is gated too: a gate that can be bypassed by typing the ID in
# lowercase is not a gate. Reports always use the uppercase form.
#   (H|SPIKE|OPS|CORE|FEED|MODEL|CMD|ROUTE|WATCH|AGENT|QA|REL|REF|CHORE)-[0-9]+
# Examples: H-01, SPIKE-01, FEED-02, OPS-99, REF-3.
# The ID must start at a word boundary, so "XOPS-01" does not match. The
# digit run is greedy: "OPS-013x" extracts "OPS-013", and a nonexistent ID
# fails the gate rather than silently passing.
#
# Exit codes:
#   0  no task ID in the title (nothing to gate), or every named task has a
#      complete review report
#   1  a named task's report is missing, lacks a required heading, or is too
#      short to be a real review
#   2  usage error
#
# Portability: bash 3.2 compatible (macOS runners); no GNU-only flags, no
# `grep -P`, no `realpath`, no associative arrays.
set -euo pipefail

# Task ID pattern. The leading (^|[^A-Za-z0-9]) is a portable word boundary:
# the ID must start at a word boundary (line start or a non-alphanumeric char).
readonly ID_PATTERN='(^|[^A-Za-z0-9])((H|SPIKE|OPS|CORE|FEED|MODEL|CMD|ROUTE|WATCH|AGENT|QA|REL|REF|CHORE)-[0-9]+)'

# Minimum meaningful content (bytes) a report must carry beyond its headings.
# An empty report satisfying the gate is worse than no gate.
readonly MIN_CONTENT_BYTES=200

# Required headings, matched as exact heading lines (not substrings).
readonly REQUIRED_HEADINGS='^## (Hallazgos|Veredicto)[[:space:]]*$'

usage() {
  echo "usage: require_review.sh \"<pr title>\"" >&2
}

main() {
  if [ "$#" -ne 1 ]; then
    usage
    exit 2
  fi
  local title="$1"

  # Match case-insensitively by uppercasing the title first.
  local upper
  upper="$(printf '%s' "$title" | tr '[:lower:]' '[:upper:]')"

  # grep -oE prints the leading boundary character (if any) together with the
  # match; strip it, then deduplicate. `|| true` keeps "no match" non-fatal
  # under pipefail.
  local ids
  ids="$(
    printf '%s\n' "$upper" \
      | grep -oE "$ID_PATTERN" \
      | sed -E 's/^[^A-Za-z0-9]//' \
      | sort -u || true
  )"

  if [ -z "$ids" ]; then
    echo "require_review: PR title names no task ID (<PREFIX>-<number>, e.g. OPS-02); nothing to gate."
    exit 0
  fi

  local status=0
  local id report missing content_bytes
  while IFS= read -r id; do
    report="odd/reviews/${id}.md"

    if [ ! -f "$report" ]; then
      echo "require_review: PR names task ${id} but its review report is missing." >&2
      echo "require_review: create ${report} (see odd/reviews/README.md and odd/reviews/_template.md)." >&2
      status=1
      continue
    fi

    missing=""
    if ! grep -qE '^## Hallazgos[[:space:]]*$' "$report"; then
      missing="'## Hallazgos'"
    fi
    if ! grep -qE '^## Veredicto[[:space:]]*$' "$report"; then
      if [ -n "$missing" ]; then
        missing="${missing}, '## Veredicto'"
      else
        missing="'## Veredicto'"
      fi
    fi
    if [ -n "$missing" ]; then
      echo "require_review: ${report} is missing required heading(s): ${missing}." >&2
      status=1
      continue
    fi

    # Content beyond the required headings. `|| true` guards pipefail when the
    # file consists only of headings (grep -v outputs nothing, exit 1).
    content_bytes="$(
      { grep -vE "$REQUIRED_HEADINGS" "$report" || true; } | wc -c | tr -d ' '
    )"
    if [ "$content_bytes" -lt "$MIN_CONTENT_BYTES" ]; then
      echo "require_review: ${report} has only ${content_bytes} bytes of content beyond the required headings (minimum: ${MIN_CONTENT_BYTES})." >&2
      echo "require_review: an empty report satisfying the gate is worse than no gate; write the findings." >&2
      status=1
      continue
    fi

    echo "require_review: ${report} present and structurally complete for task ${id}."
  done <<EOF
$ids
EOF

  exit "$status"
}

main "$@"
