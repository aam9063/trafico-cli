# Review report: AGENT-02

- **Reviewer:** parent orchestration pass, after the delegated worker stalled without producing anything
- **Date:** 2026-11-03
- **Scope reviewed:** the generated command reference, its staleness test and the README link

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| risk | `docs/cli-spec.md` | **A hand-maintained reference is wrong within a month, and this project already has the receipt.** `docs/ARRANQUE.md` §6.2 cited `trafico schema incidents`, a command that had never existed under that name. Nobody noticed until a worker tried to run it. | **Applied.** The reference is rendered from the live cobra tree by `RenderCLISpec`. Forty commands, their flags with defaults and descriptions, and the exit codes from §6.3. Nothing in the file is typed by a human. |
| blocker | `internal/cli/spec_test.go` | **Generating a file is not enough** — a generated file also goes stale, it just goes stale differently. Without a check, the committed copy freezes while the tree moves, which is the exact failure the document was meant to prevent. | **Applied.** `TestCLISpecIsCurrent` runs the hidden generator and compares the output with the committed file, failing with the command to regenerate. That test is the deliverable; the markdown is a by-product. |
| minor | `internal/cli/root.go` | The generator needs an entry point a maintainer can run without a build tag. | **Applied.** A hidden `trafico docs` command. Hidden because nobody needs it to use the tool; `TestCLISpecOmitsHiddenCommands` pins that it stays out of its own output. |

Verified by execution, no findings:

- **The spec generates 40 commands** and `check.sh --strict` is green with the new test in the suite.
- **The README attribution was already correct** — DGT, `nap.dgt.es`, CC BY, the `Fuente: DGT (CC BY)` closing line and the "not an official tool" note. The only change was linking the generated reference, because adding a second copy of the attribution would have been the drift this task exists to avoid.
- **A first attempt walked the tree without the `trafico` prefix**, and `TestCLISpecOmitsHiddenCommands` caught it before the file was committed.

## Veredicto

**Approve.** The value is not the markdown; it is that the markdown cannot lie without a red test.

### Not verified

- The generator's output for a command added later: it is exercised by construction, which is the point.
- The delegated worker stalled for 30 minutes on a `bash` call and produced nothing; this was implemented inline. Recorded because a stall is a fact about the run, not about the code.
