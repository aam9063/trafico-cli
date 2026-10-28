# Review report: CMD-01b

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-28
- **Scope reviewed:** `internal/app/incidents.go`, `internal/cli/incidents.go`, their tests, the seven golden files under `internal/app/testdata/`, the registration line in `internal/cli/root.go`, and the `time/tzdata` import in `cmd/trafico/main.go`

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

The reviewer ran the suite, then worked in a scratch copy: it probed the exit
code matrix, the `coverage_note` matrix, the date boundaries including both DST
transitions, the `query` echo, a malformed feed, a warm-cache fallback, and it
**mutated the code** to see which tests would notice.

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/cli/incidents.go:205`, `internal/geo/geo.go:142` | **Non-finite flag values were accepted.** `--lat NaN` exits **0** with `total: 0`: NaN passes every range comparison, the haversine against a NaN centre is never within the radius, so every row is dropped and the response reads as an honest "no incidents". `--radius NaN` and `--radius inf` **fetch the feed first** and then fail with `internal` / exit 1 — a spent request and the wrong code. | **Applied.** `parseRadiusKm` rejects `NaN`/`±Inf`; `validateRange` rejects non-finite (and out-of-range) latitude and longitude. Both are usage errors before any network call, with a test asserting **zero HTTP requests**. |
| minor | `internal/app/incidents.go:154` | No test failed if the radius were applied **after** `Apply`. The reviewer proved it by flipping the order: every existing test stayed green, including the one named `TestIncidentsAppliesRadiusBeforeFilters`. The implementation was right; the test did not defend it. | **Applied.** The reviewer's probe is committed as `TestIncidentsRadiusRunsBeforeApply`: three synthetic records, `unlocated == 2` without the radius and `== 1` with it. It fails if the order flips. |
| minor | `internal/app/incidents.go:297,323` | `query` echoed `near` and `radius` but not `lat`/`lon`, so a `--lat/--lon` query could not be verified from its own response, while a comment claimed the geo inputs were echoed. | **Applied.** Optional `lat`/`lon` are echoed only when the geo filter ran on those flags; with `--near` they stay absent (P-04). |
| minor | `internal/app/incidents_test.go:437` | The focused schema validator ignored `$ref`, and the response schema declares `items: {"$ref": "incident.schema.json"}`. Every incident inside the goldens validated as "anything": the envelope was checked, the incident contract was not. §9 asks for real output validated against the schemas. | **Applied.** The validator resolves local sibling-file references in `schemas/` and fails loudly on any reference it cannot resolve. A probe with a deliberately invalid incident now fails validation. |
| nit | `internal/app/testdata/` | `incidents show` had no golden and no schema validation. | **Applied.** `incident-show.golden.json`, byte-compared and schema-validated, pinning `total: 1`, the one-element array, the absence of `query` and the presence of the note. |
| advisory | `internal/datex2/parse.go` (`coordinatesOf`) | A record whose point has no `<loc:pointCoordinates>` got coordinates `(0,0)` instead of nil — the Gulf of Guinea. A radius query would haversine it against null island and drop it **without counting it as unlocated**, so an existing record became invisible to `--near`. A P-04 violation: the model asserted a position the feed never gave. Marked advisory because it lives outside this diff, but it is inside CMD-01's radius feature. | **Applied.** The coordinate element is now a pointer, so absent and a genuine `(0,0)` are distinguishable; the parser test pins both sides of that line. |

Verified by execution, no findings:

- **Order and validation before fetch**: every invalid combination exits 2 with empty stdout before any request, re-checked with a warm cache and with the server killed; the cache bytes were identical before and after the rejected commands.
- **`coverage_note` matrix**: present unfiltered, for `barcelona`, for a mixed `alicante barcelona` pair, for `alava` (bilingual and accent folding), for `bizkaia`, and on a `--road` filter; absent only for `--province alicante`.
- **Dates**: `--to 2026-11-02` echoes `2026-11-02T23:59:59.999999999+01:00` and includes a 20:00 Madrid start; 23:59:59.999999999 is inside and the next midnight is outside; a bare `--from` is that day's midnight; both DST transition days end on the correct offset; `2026-13-45`, `2026-02-30` and garbage are usage errors.
- **Contract**: the goldens validate against the schema and are byte-compared, so key reordering is caught; `total` is post-filtering (24 unfiltered, 2 by road, 1 for `show`); `query` echoes the normalised road, the applied status default and the parsed instants.
- **Exit codes**: 2 across fourteen distinct invalid inputs, 4 for an absent id with empty stdout, 7 for a malformed feed with the prefilled issue link, 0 with `from_cache: true` when the network is down.
- **`time/tzdata`**: with the import, `Europe/Madrid` resolves even with `ZONEINFO` and `GOROOT` poisoned; without it, it fails. `parseWindowDate` is the only caller.

## Veredicto

**Approve after one correction round.** The blocker was real and is fixed with a test that proves no request is spent on a rejected argument; the five smaller findings came with it because each was cheap and each closed a gap where a test or a contract was claimed but not enforced. The `datex2` advisory was pulled into this round deliberately: it is outside the diff but inside this feature, and shipping `--near` while a record can silently vanish would be shipping a filter that lies.

Verified after the fixes: build, vet, staticcheck and gofmt clean; the whole suite green in `go test ./... -count=1`; and `go test ./... -race` inside `golang:1.26.10` on Linux, eleven packages `ok`.

### Not verified

- Live DGT behaviour: no test in this repository touches the network, by rule.
- `--pretty` Spanish/Madrid rendering beyond the attribution line: no `PrettyFunc` exists in the project yet, so the fallback renderer is what a user gets.
- `trafico schema incidents`, exit 5, exit 130, and the CLI outside this unit.
- Pattern and format keywords inside `incident.schema.json` remain advisory: the focused validator checks structure, not every JSON Schema keyword.
- CI on the three OSes: this review ran the suite on the Windows host; the Linux leg was verified separately with `-race`.
