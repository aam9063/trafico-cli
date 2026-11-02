# Review report: RLS-01

- **Reviewer:** parent orchestration pass, verified by execution
- **Date:** 2026-11-02
- **Scope reviewed:** the GoReleaser configuration, the release workflow and the injected version variables, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| risk | `.github/workflows/release.yml` | **A tag publishes, so the workflow must not be able to ship an untested binary.** GoReleaser would happily build and upload whatever the tag points at. | **Applied.** The job runs `scripts/check.sh --strict` before GoReleaser, so a red suite fails the release instead of publishing past it. |
| minor | `.goreleaser.yaml` (`archives.files`) | The data is CC BY. A released archive that carries the binary but not the licence or the attribution note lets whoever redistributes it drop the obligation without noticing. | **Applied.** `LICENSE`, `README.md` and `docs/feeds.md` travel inside every archive. |
| minor | `internal/cli/root.go` | `version` was a single string. After a release, a bug report needs to say exactly which build it came from — a bare `0.0.4` cannot distinguish a tag from a local build masquerading as one. | **Applied.** `version`, `commit` and `buildDate` are separate injected variables; a plain `go build` leaves them at `0.0.0-dev` with no suffix, so a binary never claims a release it is not. |

Verified by execution, no findings:

- **The injection works**: `go build -ldflags "-X ...version=v9.9.9 -X ...commit=abc1234 -X ...buildDate=2026-11-02"` prints `v9.9.9 (abc1234) built 2026-11-02`, and a plain build prints `0.0.0-dev`.
- **Both YAML files parse** and the workflow uses `fetch-depth: 0`, without which the generated changelog is empty.
- **The suite is green** with the change: `check.sh --strict` passes.
- **The tag is not pushed here**: `main` and the release number are human decisions. This commit only makes the release possible.

## Veredicto

**Approve.** The configuration is small and the parts that matter are the two guards: the suite runs before publishing, and the licence travels with the binary. Neither is GoReleaser's default, and both are the kind of omission that is invisible until someone redistributes the data without attribution.

### Not verified

- A real GoReleaser run: the tool is not installed locally and the workflow is the first place it will execute. The YAML parses and the ldflags are proven, but the first tagged release is the true integration test.
- Windows archives on a Windows runner: cross-compilation is covered by `CGO_ENABLED=0` and the CI matrix, not by this file.
