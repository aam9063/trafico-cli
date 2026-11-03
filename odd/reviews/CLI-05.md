# Review report: CLI-05

- **Reviewer:** parent orchestration pass, on a direct user request
- **Date:** 2026-11-03
- **Scope reviewed:** what `trafico version` prints

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| minor | `internal/cli/root.go` (`newVersionCommand`) | The command printed `0.2.1 (286413d) built 2026-11-03T10:42:59Z`. The commit and the build date were added in RLS-01 so a bug report could name the exact build — a good reason, applied to the wrong audience. Someone checking the version wants a version, and a script comparing it wants one token. | **Applied.** The text form is the version and nothing else. |
| minor | `internal/cli/root.go` | The information RLS-01 added is still worth having: a tag and a local build can share a version string. | **Applied.** It moved to `--json`, which is FORMAT-01's contract for exactly this: text for a person, the structured value for whatever needs it. |
| minor | `docs/cli-spec.md` | Adding a flag makes the generated reference stale. | **Not a finding, the system working.** `TestCLISpecIsCurrent` failed on the first test run after the change, naming the command to regenerate. That is the drift AGENT-02 exists to prevent, caught within one keystroke of creating it. |

Verified by execution, no findings:

- **`trafico version` prints one token**: `0.0.0-dev` from a plain build, `0.2.1` from a released one.
- **`trafico version --json`** returns `{"version":…,"commit":…,"build_date":…}` as one value.
- **Two tests pin both paths**, including an assertion that the text form contains no parenthesis and no `built`.
- **`check.sh --strict` is green** with the regenerated spec.

## Veredicto

**Approve.** A small correction of judgement, not of code: the build detail belonged in the machine-readable form, not in the default read.

### Not verified

- Nothing here has behaviour beyond the two paths the tests pin.
