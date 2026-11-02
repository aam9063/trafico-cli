# Review report: FEED-05

- **Reviewer:** `gentle-ai-worker` implementation pass plus a parent correction round
- **Date:** 2026-11-02
- **Scope reviewed:** the INVIVE and TEFIVA section parsers, the `Section` model and the `sections` command, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `docs/feeds.md` §8 → `internal/model/section.go` | **The documentation was wrong and it induced a wrong decision.** §8 stated that TEFIVA carries no PK elements, so FEED-05 was told to derive the kilometre point from the identifier. The feed *does* publish it: `loc:linearWithinLinearElement/loc:fromPoint|toPoint/loc:distanceAlong`, in metres, six elements for the three fixture sections. The derived values matched exactly, which is why a count check would never have caught it. | **Applied.** `SectionFromTefiva` reads the published elements first and reports `pk_source: "reference"`; the identifier is only a fallback (`pk_source: "identifier"`) for a payload without a location block. §8 now documents the real structure and says why the two agreed. |
| minor | `internal/datex2/sections.go` | The TEFIVA identity guard was the v3 `PredefinedLocationsPublication` payload type, and the `tramosriesgomotos` feed has the identical shape. Its body would have parsed as zero sections and reached the caller as an empty list with exit 0 instead of `feed_changed`. | **Applied.** The guard now also requires at least one `predefinedLocationReference` id in the documented `PROVINCIA_CARRETERA_METROS` form — structural identity, the same criterion used for v1.0. **Not verified against the motos feed**, which has not been measured; stated in the code comment and in §8. |
| minor | `docs/feeds.md` §7 | It said the INVIVE province travels in `name/descriptor/value` typed `tpegDescriptorType`. In the real payload the area descriptor is typed `other`; the province is in `_0:administrativeArea` on the reference point. | **Applied.** §7 corrected and the parser reads `administrativeArea`, as published (`ALBACETE`). |

Verified by execution, no findings:

- **The two identity guards, both directions**: an INVIVE body into the TEFIVA parser and a TEFIVA body into the INVIVE parser each return an error, never an empty list. The radars body, which is the same v1.0 dialect, is rejected by the INVIVE set-id check.
- **The PK derivation**: three of three fixture ids match the documented pattern; the unmatched branch is pinned synthetically (`HUELVA_N-433_sinpk` → both bounds `null`, `pk_source: null`).
- **Two real defects were caught between RED and GREEN**: a nil dereference decoding a radars body into the INVIVE parser (`referencePointLinear` is absent on point radars), and `71.4 + 1.7 = 73.09999999999999` reaching JSON before the millimetre rounding.
- **No guessing**: the TEFIVA province is `null` because the feed publishes no INE code. The province prefix in the id is never lifted into the field.

## Veredicto

**Approve.** The blocker here is not in the code — it is that a wrong line in the measurement document produced a wrong instruction, which produced correct code for the wrong premise. The feed corrected the documentation, which is the order this project wants. The three findings are fixed, `check.sh --strict` is green, and the golden files were regenerated for the two kinds.

Worth stating plainly: the id-derived values matched the published ones exactly, so no test that checks values would have found the error. Only reading the payload element by element did.

### Not verified

- The full production feeds: only the three-section snapshots exist locally (INVIVE publishes 1331, TEFIVA 150).
- The `tramosriesgomotos` payload: not measured, so the new guard is inferred from its documented shape rather than observed.
- INVIVE's `administrativeArea` for areas that are not provinces: read verbatim, per P-04.
