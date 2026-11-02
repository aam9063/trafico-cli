# Review report: ROUTE-02

- **Reviewer:** parent orchestration pass, verified by execution
- **Date:** 2026-11-02
- **Scope reviewed:** the `route check` extension that reports panels and radars per segment, before its commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| design | `internal/app/routes.go` (`RouteCheckOptions`) | **The extra feeds are opt-in, and that is a decision, not an oversight.** `route check` currently makes exactly one fetch — the ROUTE-01 constraint — because the situation feed is ~7 MB. Panels cost two more feeds and radars one more: loading them always would turn the fastest command into the slowest for every caller, including the ones that only want incidents. | `--signs` and `--radars` are flags. The keys `signs` and `radars` travel either way and serialise as `null` when not requested, so a consumer sees a stable shape — the same reason `speed_limit` exists as `null` on a radar. |
| minor | `internal/app/routes.go` (`radarOnSegment`) | A segment radar publishes its range **unordered** (`docs/feeds.md` §6: ini 2650 m, fin 1400 m). Comparing the endpoints directly against the segment bounds would have made every reversed radar invisible on its own segment — a silent miss, not an error. | The interval is sorted before the overlap test, and `TestRadarOnSegmentMatchesInvertedPK` pins it. |
| minor | `internal/app/routes.go` (`signOnSegment`) | A panel publishes a single kilometre point, not a range, so it needs a different test from the radar's interval overlap. Using the same function would have widened a point into a zero-length interval and dropped panels sitting exactly on a segment bound. | Separate predicate, inclusive on both ends, pinned by `TestSignOnSegmentUsesASinglePoint`. |

Verified by execution, no findings:

- **One fetch per requested feed**: both optional feeds are loaded once, outside the segment loop, for the same reason the situation feed is — a per-segment fetch would cost one full download per declared segment on a route with five segments.
- **The golden was regenerated and reviewed**: `route-check.golden.json` changed only by the two new keys per segment (`signs: null`, `radars: null`) and the two new summary counters. No incident serialisation moved.
- **The stderr channel is unchanged**: diagnostics still go to stderr, stdout stays one JSON value.

## Veredicto

**Approve.** The contract change is additive and every pre-existing field serialises byte for byte as before, which the golden diff confirms. The two matching predicates are the part with real failure modes and both are pinned: a reversed radar would have disappeared silently, and a panel exactly on a bound would have been dropped.

### Not verified

- The optional feeds against the real endpoint during a route check: the fixtures drive the tests, and no test touches the network.
- The `--signs` path end to end: `Signs` loads two feeds internally and its own envelope is discarded here; only the sign list is used. A future task could surface its orphan-controller warning through `route check` instead of dropping it.
