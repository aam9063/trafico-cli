# Review report: FORMAT-02

- **Reviewer:** `gentle-ai-worker` implementation pass over a parent-measured defect, verified by execution
- **Date:** 2026-11-02
- **Scope reviewed:** the text renderers for the data commands, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/cli/*` | **FORMAT-01 shipped without rendering anything.** It inverted which path runs by default, and I verified the flag existed rather than what it produced. `grep PrettyFunc` returned two files; eight commands printed indented JSON behind a text-default flag. The user's request — output that is not JSON — was unmet for every command they would actually use. | **Applied.** Eleven renderers in `internal/cli/render.go`, wired into each command's `output.Options`. `incidents --limit 5` is now five aligned rows instead of ~200 lines of JSON. |
| blocker | `internal/cli/render_test.go` | **The test that should have caught FORMAT-01 did not exist.** The old tests asserted that `--json` returned the contract; none asserted that the default path did something else. A suite that only pins one branch cannot notice the other one is empty. | **Applied.** All 14 new tests assert the first non-empty line is **not** `{`, that no envelope key leaks into the text, and that the attribution line is last. That is the assertion whose absence allowed the original mistake. |
| minor | `internal/cli/render.go` | Column choice per list, truncation width, `-` for absent values, and English contract values left untranslated all had no precedent in the codebase. | Accepted as proposed. Truncation reuses `schema.go`'s 72-rune width so the CLI has one rule. Contract values stay in English because translating them would break the "preserve the feed's original text" principle; inventing a translation table is exactly what P-04 forbids. |
| minor | `internal/cli/render.go` | `route add/remove` and `watch add/remove` inherit the new renderers through their shared response types — a side effect, not a decision. | Accepted: it is consistent with the goal, and a command that confirms an action in text rather than JSON is strictly better for the user. Recorded because it was not asked for. |

Verified by execution, no findings:

- **`--json` is byte-identical**: the contract tests for incidents, feeds and route check pass by name, and the renderers only touch the `PrettyFunc` branch.
- **A real run against the live feed** produced a readable table with the publication line and the attribution, and two identical-looking `LZ-10` rows that are genuinely two situation records — the renderer reports the feed, it does not deduplicate it away.
- **`check.sh --strict` is green**; no existing test changed.

## Veredicto

**Approve.** This is the work FORMAT-01 claimed to have done.

### Not verified

- `watch check`, `watch run` and `watch service` still fall through to the JSON fallback. They are per-command reports rather than lists or details, were not in the measured defect list, and are a follow-up.
- The column widths against real long values: the truncation rule is pinned, but the visual balance on a 40-column terminal is a judgement call with no house style to match.
- The live network run used isolated XDG directories, so no local state was touched.
