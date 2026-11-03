# Review report: FORMAT-03

- **Reviewer:** `gentle-ai-worker` implementation pass, verified by execution
- **Date:** 2026-11-02
- **Scope reviewed:** the text renderers for `watch check`, `watch run` and `watch service`, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/cli/watch.go` | Three commands still printed indented JSON after FORMAT-01 and FORMAT-02, contradicting the default the user asked for. FORMAT-02 left them out on the grounds that they are reports rather than lists; that was a reason to render them differently, not a reason to skip them. | **Applied.** Each gets a renderer shaped by where its output is read: `watch run` writes to a scheduler log, so it is one timestamped line per watch with no repeated heading; `watch check` is read by a person, so the empty case is a sentence; `watch service` is read once, so it stays terse. |
| minor | `internal/cli/watch.go` | The renderers need data the JSON envelope does not carry (the event list, the service verb). Adding exported fields would change the contract — S3 of FORMAT-01 forbids it. | **Applied, and worth recording as a pattern.** The response structs gained **unexported** fields, which `encoding/json` never marshals, so `--json` stays byte-identical without a `schema_version` bump. The cost is that renderer-only state now lives on the transport struct; a cleaner shape would be a separate view type, and that is a refactor for whoever touches this next. |
| minor | `internal/cli/render.go` | `internal/cli` now imports `internal/store` to name `store.Event`. The CLI layer was not previously aware of the storage package. | Accepted: the event kind is what the renderer exists to display, and redefining it locally would duplicate the contract. Recorded because it is a dependency direction worth noticing rather than discovering later. |
| minor | `internal/cli/render.go` | The first pass produced `Resumen: 1 vigilancias` — wrong grammar in the one line a person reads every run. | **Applied.** Singular/plural handled, pinned by `TestWatchRunTextSingularSummary`. Caught between RED and GREEN, which is where the cheap catches happen. |

Verified by execution, no findings:

- **All seven new tests assert the output is not the JSON fallback**, via the shared `assertTextRender` that FORMAT-02 introduced. That assertion is now the house rule.
- **`--json` is byte-identical**: the pre-existing watch tests decode the envelope and pin its keys; they pass unchanged, which is the proof, not a claim.
- **The empty case is a sentence, not an array**: `Vigilancia ruta: sin novedades.` — an empty list in text reads like a failure.
- **`check.sh --strict` is green**, and the real binary demo ran against a local fixture server with no external network.

## Veredicto

**Approve.** This closes FORMAT-01 properly: after it, no command that emits an envelope prints JSON by default.

The interesting part is that "make it readable" has three different answers here — a table, a log line and a confirmation — because those three commands are read in three different places. Rendering them the same would have been easier and wrong.

### Not verified

- `watch service` on a real platform: the Windows seam produces the documented unsupported-platform error, and the systemd/launchd paths are exercised through the existing seam, not on a live host.
- `watch run` under a real scheduler for a full interval cycle: the log shape is pinned, the timing is not.
