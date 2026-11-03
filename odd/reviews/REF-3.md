# Review report: REF-3

- **Reviewer:** parent orchestration pass, verified by execution
- **Date:** 2026-11-03
- **Scope reviewed:** wiring the installer harness into the release workflow

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `.github/workflows/release.yml` | **The installer harness ran in no gate, so a broken installer could ship.** `check.sh` is the Go gate: gofmt, build, vet, staticcheck, govulncheck, `go test`. It cannot see `install.sh`, which downloads a binary, verifies a checksum and writes to a PATH. QA-01 called the harness's isolation real debt, and REL-02 proved it: four assertions had pinned the old Windows refusal and went stale without anything noticing until a human read them. | **Applied.** The release job runs `sh scripts/test-install.sh` after the Go suite and before GoReleaser. A regression in the installer now fails the release instead of shipping. |

Verified by execution, no findings:

- **The harness is offline by design** — network calls stubbed — so the step costs seconds, not minutes.
- **33 assertions pass**, and the step runs *before* GoReleaser, so it gates publication rather than reporting after it.
- **The YAML parses** and the existing steps are untouched: pinned SHAs, the linter install, the strict suite.

## Veredicto

**Approve.** This closes the loop the installer opened: it verifies what it downloads, and now something verifies the installer.

### Not verified

- The step in a real release run: the next tag is its first execution. It is a shell call with a known green exit code, which is as far as a local check can go.
- `check.sh` itself remains Go-only by design; putting the shell harness inside it would make the Go gate depend on `sh`, `unzip` and `mktemp` on every platform.
