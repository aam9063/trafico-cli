# Review report: FORMAT-01b

- **Reviewer:** `gentle-ai-worker` implementation pass, verified by execution
- **Date:** 2026-11-02
- **Scope reviewed:** the text renderers for `skills` and `schema`, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/cli/*` (whole tree) | **FORMAT-01 was verified against a false premise of mine.** I wrote, and reported, that `incidents`, `signs`, `radars` and `sections` "have their own Spanish renderer". They do not. `grep PrettyFunc` returns only `skills.go` and `schema.go` — every data command falls through to `output.writePretty`, which is **indented JSON plus an attribution line**. So the user asked for output that is not JSON, FORMAT-01 shipped, and eight of ten commands still print JSON. | **Recorded, not fixed here.** This commit gives `skills` and `schema` real renderers because they were the ones in front of us. The rest is FORMAT-02 and it is the real remainder of the work. |
| minor | `internal/cli/schema.go` | A JSON Schema is a document, not a table. Tabulating it would have been the wrong shape. | **Applied.** `schema <name>` gets a heading, title and description, then the document as formatted JSON from the same embedded bytes. `schema` (list) is tabulated because a list is a list. |
| minor | `internal/cli/schema.go` (`shortDescription`) | The list truncates descriptions at 72 runes with `…`. Presentation only — `--json` and `schema <name>` keep the full text. | Accepted as proposed. The descriptions are the schemas' own English text, quoted verbatim and not translated: nothing invented. |

Verified by execution, no findings:

- **`--json` is byte-identical to before**: the existing tests that compare the served schema against the published file still pass, and the output is still one single line.
- **The four new tests assert the output is not the JSON fallback**, not merely that it contains expected strings — that is what would have caught this defect in the first place.
- **RED observed**: all four failed against the unmodified code with the fallback JSON in the message.
- **`check.sh --strict` is green.**

## Veredicto

**Approve.** Two commands now read like the tool they belong to.

The finding that matters is the one this commit does **not** fix, and it is mine: FORMAT-01 changed which path runs by default, and I checked that the flag existed rather than what the flag rendered. The default was already the fallback; inverting it changed almost nothing, and the user's request is still unfulfilled for the commands they actually use.

### Not verified

- The visual format of the new renderers against a house style: there was no existing per-command renderer to copy, so the shape is this pass's proposal.
- Eight commands remain on the fallback. Their renderers are FORMAT-02.
