# Review report: REF-4

Covers the three cheap REFs QA-01 proposed for the next release. REF-2 (split `render.go`) and REF-3 (wire the installer harness into the release job) stay in the backlog.

- **Reviewer:** parent orchestration pass, verified by execution
- **Date:** 2026-11-03
- **Scope reviewed:** the dead output surface, the `sections` heading and the `watch service` verb dispatch

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| minor | `internal/output/output.go` | **`WriteLines` and `IsTTY` had no callers anywhere but their own tests.** `WriteLines` is documented "for commands that stream results" and no command streams JSONL; TTY detection is decided by `prettyFor`, never by `IsTTY`. Dead exported surface inside an `internal/` package is still surface someone will eventually call because it exists. | **Applied.** Both deleted with their tests, and the `os` import they were the only user of. |
| minor | `internal/cli/render.go:393` | `renderSectionsText` read the dataset kind from the first matching section, so an empty result printed `Tramos  (0):` — double space, no dataset name. It is the case a user hits precisely when their filter matches nothing. | **Applied.** `SectionsResponse` carries the requested `Kind` as a non-serialised field, the same way it already carries `Warning`; the render reads it from there. Verified with the binary: `Tramos animals (0):`. |
| minor | `internal/cli/render.go:955` | The `default:` branch of `renderWatchServiceText` rendered **any** unknown verb as a status report. Only three verbs are passed today, so nothing was wrong — but a fourth would have rendered as status and exited 0 instead of failing. | **Applied.** `status` became an explicit case and `default` returns an internal error naming the verb, matching the type-assertion guard directly above it. |

Verified by execution, no findings:

- **`--json` is untouched** by all three: two fields are `json:"-"` and the third is a dispatch change on a path that never produces output.
- **`check.sh --strict` is green** after the deletions, which is the proof nothing needed the removed surface.
- The `sections` heading reads the requested kind, so a filter that matches nothing still names the dataset it looked in.

## Veredicto

**Approve.** Three small closures, and the third is the one worth naming: a `default:` that renders something plausible is how a future bug exits 0.

### Not verified

- Nothing here has behaviour that a fixture exercises beyond what the suite already covers.
