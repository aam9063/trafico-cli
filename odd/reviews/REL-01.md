# Review report: REL-01

- **Reviewer:** `gentle-ai-worker` implementation pass, verified by execution
- **Date:** 2026-11-02
- **Scope reviewed:** the one-line installer and its offline test harness, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| risk | `install.sh` | **An installer that downloads a binary and runs it without verifying it turns a compromised asset into code execution.** The project publishes `checksums.txt` with every release and, until now, nothing consumed it. | **Applied.** The SHA-256 is verified **before unpacking**. The test does not merely check that the comparison works: it truncates the fixture to 5 bytes, asserts `verify_checksum` returns 1, and asserts the message says nothing was installed. |
| minor | `install.sh` | Windows is one of the six published assets, and a POSIX `sh` installer would fail there in a confusing way. | **Applied.** Windows is rejected up front with a message pointing at the `.zip`, and any unsupported OS/arch gets the releases page instead of a guess. |
| minor | `scripts/test-install.sh` | A shell test that needs the network cannot run in this repository, and one that skips silently is worse than none. | **Applied.** 31 assertions, zero network: the network calls are stubbed and the decision logic, checksum paths and cleanup are exercised directly. It is standalone (`sh scripts/test-install.sh`), **not** wired into `check.sh`, because that gate is Go-only and the `Makefile` was outside the allowed surfaces. Stated rather than hidden. |

Verified by execution, no findings:

- **Temp directory cleanup is tested on the failure path**, not just asserted in a comment: `download` is stubbed to fail and the test proves both exit 1 and that the `mktemp` directory is gone.
- **Re-running is safe**: an existing binary is replaced and reported.
- **`sh -n` passes on both scripts**, and `bash scripts/check.sh --strict` is green.

## Veredicto

**Approve.** The installer closes the gap between "binaries are published" and "this installs", and it does so with the two properties that matter: it verifies what it downloads, and it fails closed.

### Not verified

- **The live network path**: the GitHub API call and the real asset download are untestable offline by design. Only the decision logic and the failure/cleanup paths are covered. The first real `curl | sh` is its integration test.
- The harness is standalone, so nothing forces it to run before a release. Wiring it into `check.sh` or the release job is a separate decision.
