# Review report: SEC-01

- **Reviewer:** parent orchestration pass, forced by a red gate
- **Date:** 2026-11-03
- **Scope reviewed:** the Go toolchain directive in `go.mod`

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `go.mod:5` | **`check.sh --strict` went red with exit 3 on code that had not changed.** `govulncheck` reported `GO-2026-6859` in `net/http`, fixed in `go1.26.11`; the pinned toolchain was `go1.26.10`. The vulnerability is reachable from this code — `feeds.Cache.Store`, `output.Diag` and the HTTP response handling all appear in the trace. | **Applied.** `toolchain go1.26.11`. The gate is green again with no source change. |

Verified by execution, no findings:

- **The failure was not caused by the TUI work.** It appeared because `govulncheck` refreshes its database on every run: the same tree that passed yesterday now reports a vulnerability published since. That is the gate doing its job on a moving target, not a regression in the change under review.
- **Go downloads the pinned toolchain** (`downloading go1.26.11`), so every machine and CI job builds with the fixed version without anyone installing it by hand.
- **`check.sh --strict` is green**: gofmt, build, vet, staticcheck, govulncheck and the full suite.

## Veredicto

**Approve.** A security fix that was sitting behind a red gate nobody had run recently.

Worth recording: `govulncheck` in `--strict` is worth exactly this. Without it, the project would have shipped `v0.2.5` on a stdlib with a known HTTP vulnerability, and nothing in the test suite would ever have mentioned it.

### Not verified

- Whether the vulnerable path is reachable on the three release targets from outside: `govulncheck` reports reachability from this code, not exploitability. Fixing the toolchain makes the question moot.
- The `go 1.26.0` language directive is untouched: only the toolchain moved.
