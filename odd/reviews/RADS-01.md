# Review report: RADS-01

- **Reviewer:** parent orchestration pass, closing a quirk REF-2 found and preserved on purpose
- **Date:** 2026-11-03
- **Scope reviewed:** the truncation note in `renderRadarsText`

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| minor | `internal/cli/render_static.go` | **The renderer counted a truncation it never performed.** A manual counter incremented whenever the road kind exceeded the column width, and the row then printed that kind in full. Only the destination label is the free-text column, and only `textTable` cuts it. So the closing note could say *"1 descripción truncada"* while every field was intact — a note that describes an edit nobody made. | **Applied.** The manual counter is gone; the count now comes from what the table actually cuts. |
| minor | `internal/cli/render_test.go` | **Nothing pinned it**, because no fixture has a kind longer than 72 runes. A quirk with no fixture is a quirk nobody will ever see, which is how it survived three render passes. | **Applied.** Two tests: one with a 160-rune kind that must appear whole and must not produce a note; one with two 210-rune destination labels that must be counted. The second exists because the first would pass on a renderer that simply never truncates. |

Verified by execution — **both tests have teeth**:

- Inflating the counter by one makes the first test fail with `the note claims a truncation that did not happen`.
- The second test fails if `textTable` stops cutting the free column, which is the failure mode the first test alone could not see.
- `check.sh --strict` is green.

REF-2 found this quirk and preserved it byte for byte rather than "fixing" it inside a pure move. That was the right call twice over: the move stayed provably behaviour-preserving, and the fix arrived with a test instead of riding along with a refactor.

## Veredicto

**Approve.** A note that lies is worse than no note: it tells the reader their view is complete when it is not.

### Not verified

- Any other renderer with a hand-written counter beside a table that counts its own cuts: not audited. This one was found by a differential harness, not by looking.
