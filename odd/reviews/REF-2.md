# Review report: REF-2

- **Reviewer:** `gentle-ai-worker` implementation pass over the QA-01 finding
- **Date:** 2026-11-03
- **Scope reviewed:** the split of `internal/cli/render.go`

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| design | `internal/cli/render.go` | **976 lines holding 16 renderers, and the file's own header invited the next append.** QA-01 was precise about the problem: not the quality of any renderer, but the unit of change. Three passes had each appended, and the seam to split along already existed. | **Applied.** `render.go` keeps only the shared vocabulary plus a new `textTable` (257 lines); the renderers moved to `render_incidents.go`, `render_routes.go`, `render_feeds.go`, `render_static.go`, `render_watch.go`. |
| minor | seven list renderers | The same build-columns / measure-widths / truncate-and-count prelude was written seven times, one of them inside `renderDoctorText`. Seven copies of a rule is seven places for it to drift. | **Applied.** One `textTable` with `newTextTable`/`addRow`/`writeTo`. The render code is 1003 lines against 976 before — the helper costs more than the duplication it removes in raw lines, and it is still the right trade: the rule now lives once. |
| minor | `internal/cli/render_watch.go` | `watchRunEntryText` re-implemented singular/plural four lines from `countNoun`, which exists for exactly that. | **Applied.** |
| minor | `internal/cli/render_watch.go` | The `watch run` log mixed stamps: per-watch entries carried RFC3339, the closing `Publicado:`/`Resumen:` lines did not. | **Decided: footer.** A blank line separates the last entry from the tail, and a comment states that a log consumer parses only the `<stamp> <watch>: …` lines. Stamping the tail would have meant touching the shared `publishedNote` or prefixing per renderer. |
| minor | `internal/cli/render_static.go` | **A latent quirk, found by the differential harness and preserved on purpose.** `renderRadarsText` truncates the radar kind into a local copy and counts the cut in the truncation note, but prints the untruncated value. No fixture has a kind longer than the column, so nothing pins it. | **Recorded, not fixed.** Fixing it changes user-visible output for kinds over 72 runes; the code carries a comment, and it wants its own task with a fixture that has a long kind. |

Verified by execution — and the verification is the interesting part:

- **Zero test files were modified**, which is the definition of a move. All 14 `assertTextRender` tests and the `--json` contract tests pass untouched.
- **Byte-identical output, proven rather than asserted**: a scratch differential harness ran verbatim copies of the six pre-split list renderer bodies against the new ones — including truncation-heavy and empty-list inputs — and a dump/comparison of `trafico incidents --limit 5` before and after matched with `cmp`.
- **That harness earned its keep**: it caught two real divergences before they shipped (doctor's padded last column and its suffix separator) and one `textTable` panic on a table with no free column.
- **Scratch files deleted**, verified: `git status` shows only the six intended files.

## Veredicto

**Approve.** The file was not broken; it was the wrong unit of change, and now it is five right ones plus a vocabulary.

Worth naming the two fixes the harness found. A pure move that "obviously" preserves behaviour found three defects in itself — which is the argument for proving a refactor instead of trusting it.

### Not verified

- The radar-kind quirk stays in the code by decision, marked and recommended as a follow-up.
- Line counts as a quality measure: 1003 against 976 going up is the honest number, and the reason to make the change was the unit of change, not the total.
