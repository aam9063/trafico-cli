#!/usr/bin/env bash
# Trim a DATEX II payload down to a small, deterministic snapshot for testdata.
#
# Keeps whole top-level blocks, never partial elements, so the result is still a
# well-formed document that the real parsers can read. Deterministic by design:
# the same input always yields the same output.
#
# Usage:
#   scripts/trim-fixtures.sh --tag sit:situation --keep 3 \
#       --group-by sit:causeType <in.xml> <out.xml>
#
#   --tag T        qualified tag of the repeated block to keep
#   --keep N       max blocks per group (default 1)
#   --group-by T   qualified tag whose text selects the group; blocks are
#                  interleaved round-robin so every group contributes (optional)
#   --header-note  free text embedded as an XML comment in the output
set -euo pipefail

TAG=""
KEEP=1
GROUP=""
NOTE=""
POSITIONAL=()

while [ $# -gt 0 ]; do
  case "$1" in
    --tag) TAG="${2:?--tag needs a value}"; shift 2 ;;
    --keep) KEEP="${2:?--keep needs a value}"; shift 2 ;;
    --group-by) GROUP="${2:?--group-by needs a value}"; shift 2 ;;
    --header-note) NOTE="${2:?--header-note needs a value}"; shift 2 ;;
    -h|--help) sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) POSITIONAL+=("$1"); shift ;;
  esac
done

if [ -z "$TAG" ] || [ "${#POSITIONAL[@]}" -ne 2 ]; then
  echo "usage: $0 --tag <tag> [--keep N] [--group-by <tag>] <in.xml> <out.xml>" >&2
  exit 2
fi

IN="${POSITIONAL[0]}"
OUT="${POSITIONAL[1]}"

awk -v tag="$TAG" -v keep="$KEEP" -v group="$GROUP" -v note="$NOTE" '
function isOpen(s,   re) {
  if (index(s, "<" tag) == 0) return 0
  re = "^[ \t]*<" tag "[ >]"
  return s ~ re
}
function isClose(s,   re) {
  if (index(s, "</" tag) == 0) return 0
  re = "^[ \t]*</" tag ">"
  return s ~ re
}
function pickGroup(buf,   tmp, s) {
  if (group == "") return ""
  tmp = buf
  if (!match(tmp, "<" group ">[^<]*</" group ">")) return ""
  s = substr(tmp, RSTART, RLENGTH)
  sub("<" group ">", "", s)
  sub("</" group ">", "", s)
  return s
}
BEGIN { open = 0; buf = ""; nblk = 0; ngroups = 0; done_header = 0; depth = 0 }

{
  if (!done_header) {
    if (isOpen($0)) done_header = 1
    else { header = header $0 "\n"; next }
  }

  if (!open) {
    if (isOpen($0)) { open = 1; depth = 1; buf = $0 "\n"; next }
    footer = footer $0 "\n"
    next
  }

  buf = buf $0 "\n"
  if (isOpen($0)) depth++
  if (isClose($0)) depth--
  if (depth == 0) {
    open = 0
    nblk++
    g = pickGroup(buf)
    if (!(g in count)) { order[++ngroups] = g; count[g] = 0 }
    blk[g SUBSEP count[g]] = buf
    count[g]++
  }
}

END {
  printf "%s", header
  if (note != "") printf "<!-- %s -->\n", note
  emitted = 0
  for (n = 0; n < keep; n++) {
    for (i = 1; i <= ngroups; i++) {
      g = order[i]
      if (n < count[g]) { printf "%s", blk[g SUBSEP n]; emitted++ }
    }
  }
  printf "%s", footer
  printf "trimmed: kept %d of %d blocks in %d group(s)\n", emitted, nblk, ngroups > "/dev/stderr"
}
' "$IN" > "$OUT"
