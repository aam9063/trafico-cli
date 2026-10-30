# Review report: CMD-01a

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-28
- **Scope reviewed:** `internal/filter` and `internal/geo` — the filtering and distance layer of CMD-01, before its commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

This review covered the first half of CMD-01: the pure filtering logic and the
distance maths, split out so the commands could be reviewed separately. The
reviewer ran the suite, then wrote its own probes in a scratch copy to attack
each rule in §5.2 rather than read the tests and agree with them.

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/geo/geo.go:91` | `ResolveTarget("", nil, nil, -5, places)` returned `(nil, nil)`: an explicitly passed negative `--radius` was silently ignored, the caller proceeded as "no geo filter" and the process exited 0. The package doc already stated the radius must be strictly positive. | **Applied.** `radiusKm < 0` returns `errs.NewUsage` unconditionally, before the no-target branch. `radiusKm == 0` with no target keeps meaning "no geo filter", now stated explicitly. |
| major | `internal/filter/filter.go:226` | An incident the feed marks `direction: "both"` was **excluded** by `--direction positive`. The real feed carries 78 such records (`docs/feeds.md` §4.4): `both` means the incident blocks both directions. Asking "what is happening in the positive direction of the AP-7" would miss a full closure — the wrong answer to the question actually asked, not a cosmetic gap. | **Applied.** An incident with direction `both` matches both `positive` and `negative` filters. A `null` incident direction still matches only when no direction filter is set, and a `both` filter still means "no filter". Rule documented in `doc.go`. |
| minor | `internal/filter/filter.go:237` | An incident with `pk{from: null, to: null}` matched **every** `--pk` range, while `pk: null` never matched — claiming knowledge of bounds the feed did not provide. | **Applied.** Both bounds nil → no match, like a nil PK. A point location with one bound keeps working. |
| minor | `internal/filter/filter.go:143` | `Unlocated` was counted **before** the type/severity/status/window checks, so it included rows those filters had already discarded, and it scoped differently from geo's contribution to the same `meta.unlocated` bucket. | **Applied.** A row counts as unlocated only after passing every positive-match filter. The composition contract is documented: the CLI applies the radius first, then `Apply`, and adds both counts. |
| minor | `internal/filter/filter.go:175` | A negative `--limit` was silently treated as "no limit", while inverted PK and date ranges were rejected. | **Applied.** `Limit < 0` → `errs.NewUsage`; `0` keeps meaning no limit. |
| nit | `internal/filter/filter.go:314` | `severityRank` was consulted without an `ok` check, so an empty or unmapped severity ranked 0 — tied with `high` — breaking the documented "unknown last" guarantee for any input the model does not produce. | **Applied.** Unmapped severities rank 3 (last). |
| advisory | `internal/filter/filter.go:275` | `--from`/`--to` are instants while §5.2 shows dates. An incident starting `2026-10-31T08:00` is excluded by `--to 2026-10-31`, so a CLI that parses the flag as midnight drops everything after the first instant of the last day. | **Documented, not changed.** The overlap semantics are coherent; the caller owns date→instant parsing and must pass an inclusive end-of-day. Recorded in `filter/doc.go` before the CLI task can get it wrong. |

Checks that passed, by execution:

- **PK is overlap, not containment**: the test fails if it were containment (incident 95–105 against `--pk 100-140`), boundaries are inclusive, a one-bound point matches at its own PK, and `--pk` with zero or two roads is an `*errs.Error` with exit code 2.
- **Province folding**: `Lugo`/`LUGO` and `alicante`/`Alacant/Alicante`; a row without province is counted instead of vanishing.
- **Status default `active`**, an ended incident excluded, an unknown status rejected with exit 2.
- **`--limit` applies after sorting**, provable because the test fails if it ran before.
- **`--severity unknown` is a real matchable value**, not a synonym for "no filter" (P-04).
- **Determinism**: all sorts are `sort.SliceStable`, `pk: null` last, `unknown` severity last; two identical `Apply` calls produce identical results including `Unlocated`.
- No duplicated haversine or road logic between the two packages.

## Veredicto

**Approve after one correction round.** The blocker and the major are both real and both fixed with regression tests that fail against the previous code; the four smaller findings went with them because each was a one-line change guarded by its own test. Per §10.5 the round is spent: nothing else changed.

Verified after the fixes: build, vet, staticcheck and gofmt clean; 51 tests pass in the two packages; and `go test ./... -race` inside `golang:1.26.10` on Linux is green across all nine packages.

### Not verified

- Composition with the real CLI: `internal/app` and `internal/cli` for `incidents` do not exist yet, so `meta.unlocated` merging and the date-parsing contract remain untestable end to end. They are T2b's responsibility and are documented in both package docs.
- Whether `pk{from:null,to:null}` or unparsable timestamps actually occur in the live feed: no snapshot was exercised here.
- Filtering performance over a full feed: no benchmark was run for this layer.
- Live feed behaviour, by rule: no test in this repository touches the network.
