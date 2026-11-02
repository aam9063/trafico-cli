# Review report: RLS-03

- **Reviewer:** parent orchestration pass, from the observed failure of run `41459574510`
- **Date:** 2026-11-02
- **Scope reviewed:** the repair of the release workflow, before its commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `.github/workflows/release.yml` | **The first tag failed, and it failed at the gate, not at GoReleaser.** Run `41459574510`: `FAIL: staticcheck is required with --strict (CI installs it)`. The CI job installs `staticcheck` and `govulncheck` before calling `check.sh`; the release job did not, so `--strict` refused to run and the release stopped. | **Applied.** An `Install linters` step mirrors `ci.yml:24-26` before the check. The gate is unchanged: `--strict` keeping its refusal is the point — a release that silently skipped its linters would be worse than one that stops. |

Verified by execution, no findings:

- **The gate did exactly what it was put there for.** The release never reached GoReleaser, so no half-published release exists and no artefact has to be withdrawn. The failure was loud, early and free.
- **The fix is a copy of the CI job**, not a new invention: same two `go install` lines, same order.
- **The YAML parses** and the rest of the workflow is untouched: the pinned SHAs, the `check.sh` gate and the licence files in the archives all remain.

## Veredicto

**Approve.** A missing tool in a workflow that had never run. Worth noting what this says about the guard: the `check.sh --strict` step added in RLS-01 caught its own pipeline's first defect, before a single binary was published. Had it not been there, GoReleaser would have shipped six artefacts from a job that never ran the linters.

### Not verified

- A second real run: this commit is the fix, and the next tag is its test. GoReleaser itself still has never executed, so the archive and ldflags configuration remains unproven.
- `@latest` for both tools: the same as the CI job. Pinning them would be better and is a separate decision, since a linter upgrade can fail a release that passed yesterday.
