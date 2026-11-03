#!/bin/sh
# Offline tests for install.sh, the one-line installer.
#
# Every test sources the installer with TRAFICO_INSTALLER_SKIP_MAIN=1 and stubs
# the network-facing functions (download, fetch_latest_version) and uname, so
# nothing here ever opens a socket: the repository-wide no-network test rule is
# absolute and applies to the installer tests too.
#
# Covered offline: the OS/arch detection mapping (including the Windows
# rejection and unsupported architectures), the release asset name derivation,
# the latest-release tag parsing, the checksum verification pass and fail paths
# with local fixtures, the PATH hint, and temp-directory cleanup on the failure
# exit path.
#
# Usage: sh scripts/test-install.sh

set -u

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
INSTALLER="$SCRIPT_DIR/../install.sh"

if [ ! -f "$INSTALLER" ]; then
  echo "test-install.sh: cannot find install.sh next to scripts/" >&2
  exit 1
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/trafico-install-test.XXXXXX") || WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

tests_run=0
failures=0

pass() {
  tests_run=$((tests_run + 1))
  echo "ok $tests_run - $1"
}

fail_test() {
  tests_run=$((tests_run + 1))
  failures=$((failures + 1))
  echo "not ok $tests_run - $1"
}

# expect_eq "description" "want" "got"
expect_eq() {
  desc="$1"
  want="$2"
  got="$3"
  if [ "$want" = "$got" ]; then
    pass "$desc"
  else
    fail_test "$desc (want '$want', got '$got')"
  fi
}

# expect_status "description" "want_rc" rc
expect_status() {
  desc="$1"
  want="$2"
  rc="$3"
  if [ "$want" = "$rc" ]; then
    pass "$desc"
  else
    fail_test "$desc (want exit $want, got exit $rc)"
  fi
}

# run_in_subshell runs a command with all given arguments in a subshell so a
# die/exit inside it cannot end the whole test script, and reports both its
# exit code and its combined output. Usage:
#   eval "$(run_in_subshell detect_os)"           -> sets OUT and RC
#   eval "$(run_in_subshell fetch_latest_version "$WORK")"
run_in_subshell() {
  out=$("$@" 2>&1) && rc=0 || rc=$?
  printf 'OUT=%s RC=%s' "$(printf "'%s'" "$(printf "%s" "$out" | sed "s/'/'\\\\''/g")")" "$rc"
}

# Source the installer without running main.
TRAFICO_INSTALLER_SKIP_MAIN=1 . "$INSTALLER"

# --- OS detection -----------------------------------------------------------

uname() { echo "Linux"; }
eval "$(run_in_subshell detect_os)"
expect_eq "detect_os: Linux maps to linux" "linux" "$OUT"
expect_status "detect_os: Linux exits 0" "0" "$RC"
unset -f uname

uname() { echo "Darwin"; }
eval "$(run_in_subshell detect_os)"
expect_eq "detect_os: Darwin maps to darwin" "darwin" "$OUT"
unset -f uname

uname() { echo "MINGW64_NT-10.0-19045"; }
eval "$(run_in_subshell detect_os)"
expect_status "detect_os: Git Bash (MINGW) is rejected" "1" "$RC"
case "$OUT" in
  *"Descarga el .zip"*) pass "detect_os: Windows message points at the .zip assets" ;;
  *) fail_test "detect_os: Windows message points at the .zip assets (got '$OUT')" ;;
esac
unset -f uname

uname() { echo "CYGWIN_NT-10.0"; }
eval "$(run_in_subshell detect_os)"
expect_status "detect_os: Cygwin is rejected like Windows" "1" "$RC"
unset -f uname

uname() { echo "SunOS"; }
eval "$(run_in_subshell detect_os)"
expect_status "detect_os: unsupported OS is rejected" "1" "$RC"
case "$OUT" in
  *"no soportado"*) pass "detect_os: unsupported OS message is clear" ;;
  *) fail_test "detect_os: unsupported OS message is clear (got '$OUT')" ;;
esac
unset -f uname

# --- Architecture detection -------------------------------------------------

uname() { echo "x86_64"; }
expect_eq "detect_arch: x86_64 maps to amd64" "amd64" "$(detect_arch)"
uname() { echo "aarch64"; }
expect_eq "detect_arch: aarch64 maps to arm64" "arm64" "$(detect_arch)"
uname() { echo "arm64"; }
expect_eq "detect_arch: arm64 maps to arm64" "arm64" "$(detect_arch)"
uname() { echo "amd64"; }
expect_eq "detect_arch: amd64 maps to amd64" "amd64" "$(detect_arch)"
uname() { echo "i686"; }
eval "$(run_in_subshell detect_arch)"
expect_status "detect_arch: unsupported architecture is rejected" "1" "$RC"
unset -f uname

# --- Asset name derivation --------------------------------------------------

expect_eq "archive_for: v-prefixed version strips the leading v" \
  "trafico_0.1.0_linux_amd64.tar.gz" \
  "$(archive_for v0.1.0 linux amd64)"
expect_eq "archive_for: bare version is used as is" \
  "trafico_0.1.0_linux_amd64.tar.gz" \
  "$(archive_for 0.1.0 linux amd64)"
expect_eq "archive_for: darwin arm64 tar.gz" \
  "trafico_0.1.0_darwin_arm64.tar.gz" \
  "$(archive_for v0.1.0 darwin arm64)"
eval "$(run_in_subshell archive_for v0.1.0 windows amd64)"
expect_status "archive_for: windows is not a tar.gz target here" "1" "$RC"

# --- Latest-release tag parsing (network stubbed) ---------------------------

download() { printf '%s' '{"url":"x","tag_name": "v0.1.0", "name":"v0.1.0"}' >"$2"; }
expect_eq "fetch_latest_version: parses tag_name from the GitHub API JSON" \
  "v0.1.0" \
  "$(fetch_latest_version "$WORK")"
unset -f download

download() { printf '%s' '{"message":"Not Found"}' >"$2"; }
eval "$(run_in_subshell fetch_latest_version "$WORK")"
expect_status "fetch_latest_version: missing tag_name fails clearly" "1" "$RC"
case "$OUT" in
  *TRAFICO_VERSION*) pass "fetch_latest_version: failure suggests TRAFICO_VERSION" ;;
  *) fail_test "fetch_latest_version: failure suggests TRAFICO_VERSION (got '$OUT')" ;;
esac
unset -f download

download() { return 7; }
eval "$(run_in_subshell fetch_latest_version "$WORK")"
expect_status "fetch_latest_version: download failure aborts" "1" "$RC"
unset -f download

# --- Checksum verification (local fixtures, no downloads) -------------------

FIXTURE_ASSET="trafico_0.1.0_linux_amd64.tar.gz"
GOOD_HASH="3f79bb7b435b05321651daefd374cdc681dc06faa65e374e38337b88ca046dea"
GOOD_BODY="payload that hashes to nothing in particular"

if command -v sha256sum >/dev/null 2>&1; then
  sha_tool() { sha256sum "$1"; }
else
  sha_tool() { shasum -a 256 "$1"; }
fi

# Fixture whose real hash we compute, so the pass case exercises the actual
# comparison and not a hardcoded pair.
printf '%s\n' "$GOOD_BODY" >"$WORK/archive-body"
REAL_HASH=$(sha_tool "$WORK/archive-body" | awk '{print $1}')

printf '%s  %s\n' "$REAL_HASH" "$FIXTURE_ASSET" >"$WORK/checksums.txt"
printf '%s\n' "$GOOD_BODY" >"$WORK/$FIXTURE_ASSET"
if verify_checksum "$WORK/checksums.txt" "$FIXTURE_ASSET" "$WORK/$FIXTURE_ASSET"; then
  pass "verify_checksum: matching fixture passes"
else
  fail_test "verify_checksum: matching fixture passes"
fi

printf '%s  %s\n' "$GOOD_HASH" other_0.1.0_linux_amd64.tar.gz >>"$WORK/checksums.txt"
if verify_checksum "$WORK/checksums.txt" "$FIXTURE_ASSET" "$WORK/$FIXTURE_ASSET"; then
  pass "verify_checksum: picks the right entry among several assets"
else
  fail_test "verify_checksum: picks the right entry among several assets"
fi

printf '%s\n' "$GOOD_BODY" | head -c 5 >"$WORK/$FIXTURE_ASSET"
rc=0
verify_checksum "$WORK/checksums.txt" "$FIXTURE_ASSET" "$WORK/$FIXTURE_ASSET" 2>"$WORK/fail.err" || rc=$?
expect_status "verify_checksum: corrupted archive fails" "1" "$rc"
case "$(cat "$WORK/fail.err")" in
  *"No se ha instalado nada"*)
    pass "verify_checksum: failure message says nothing was installed"
    ;;
  *)
    fail_test "verify_checksum: failure message says nothing was installed (got '$(cat "$WORK/fail.err")')"
    ;;
esac

printf '%s  %s\n' "$GOOD_HASH" some_other_asset.tar.gz >"$WORK/empty.txt"
rc=0
verify_checksum "$WORK/empty.txt" "$FIXTURE_ASSET" "$WORK/$FIXTURE_ASSET" 2>"$WORK/missing.err" || rc=$?
expect_status "verify_checksum: asset missing from checksums.txt fails" "1" "$rc"
case "$(cat "$WORK/missing.err")" in
  *"no contiene ninguna entrada"*) pass "verify_checksum: missing entry message is clear" ;;
  *) fail_test "verify_checksum: missing entry message is clear (got '$(cat "$WORK/missing.err")')" ;;
esac

# --- PATH hint --------------------------------------------------------------

saved_path=$PATH
PATH="/usr/bin:/bin"
hint=$(path_hint "$WORK/not-on-path")
case "$hint" in
  *"export PATH=\"$WORK/not-on-path:\$PATH\""*) pass "path_hint: prints the export line for a missing directory" ;;
  *) fail_test "path_hint: prints the export line for a missing directory (got '$hint')" ;;
esac

PATH="$saved_path:$WORK/on-path"
hint=$(path_hint "$WORK/on-path")
expect_eq "path_hint: silent when the directory is already on PATH" "" "$hint"
PATH=$saved_path

# --- Temp directory cleanup on the failure path -----------------------------

# Stub mktemp so the installer's temp directory lands inside $WORK, and record
# every directory it creates; then make the first download fail and check that
# main aborts and removes the directory anyway.
created="$WORK/created"
: >"$created"
mktemp() {
  dir=$(command mktemp -d "$WORK/installer.XXXXXX") || return 1
  echo "$dir" >>"$created"
  echo "$dir"
}
download() { return 9; }

cleanup_out=$(TRAFICO_VERSION=v0.1.0 main 2>&1) && cleanup_rc=0 || cleanup_rc=$?
expect_status "main: failed download aborts the install" "1" "$cleanup_rc"

leftovers=0
while IFS= read -r dir; do
  [ -n "$dir" ] || continue
  if [ -e "$dir" ]; then
    leftovers=$((leftovers + 1))
  fi
done <"$created"
expect_eq "main: failed download leaves no temp directory behind" "0" "$leftovers"
unset -f mktemp download

# --- Summary ----------------------------------------------------------------

echo "1..$tests_run"
if [ "$failures" -eq 0 ]; then
  echo "test-install: OK ($tests_run tests)"
  exit 0
fi
echo "test-install: FAIL ($failures of $tests_run tests failed)" >&2
exit 1
