# Review report: FEED-04

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-11-02
- **Scope reviewed:** the DATEX II v1.0 radar parser, the 52-entry INE province table, the `Radar` wire shape and the `radars` command, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/datex2/radars.go` (`ParseRadars`) | **The identity guard could not tell radars from INVIVE.** It accepted any v1.0 `d2LogicalModel` carrying a `PredefinedLocationsPublication` — which is exactly what the sections feed is. Pointing `feeds.dgt-radars.url` at the INVIVE URL produced **twelve phantom radars with exit 0**: `GUID_Tramo_Invive_*`, `direction: both`, `province: null`, plausible PKs. Silent wrong data behind a success code, which is the failure class this guard exists to prevent. | **Applied.** The discriminator was already in the payload: `predefinedLocationSet/@id`. The parser now requires at least one set id from a named allowlist (`GUID_Inventario_CinemometrosVelocidadMedia`, `GUID_Inventario_CabinasCinemometro`) after the root and payload-type guards. The code comment states this is payload identity, not field derivation — the earlier decision not to infer a radar `type` from an id prefix stands. Regression test RED before the fix. |
| minor | `internal/model/radar.go` | `CVM_163929` serialises `pk: {from: 2.65, to: 1.4}` because its fin reference point is 1400 m while its ini is 2650 m. Preserving it unordered is the faithful reading, but nothing said that this feed admits `from > to` while the incident contract assumes the opposite. | **Applied.** The rule is documented on `RadarFromParsed` and in `docs/feeds.md` §6, and pinned by `TestRadarFromParsedPreservesReversedPK`. |
| minor | `internal/model/radar.go` | A destination label that exists only on the fin point is dropped: `CVM_164860` carries `directionNamed: GUARDO` only on the secondary, so the output has `direction_label: null`. | **Applied.** Stated in the `direction_label` doc comment and pinned by a test. Deliberately **not** merged from the secondary: taking the primary as sole authority is what makes the output deterministic, and merging would contradict it. |

Verified by execution, no findings:

- **The guard against the other three dialects**: the v3.7 incident payload, the v3 tefiva body and the v3.7 signs payload all produce `feed_changed` (exit 7) with empty stdout, at both parser and CLI level.
- **The INE table**: all 52 codes resolve, single-digit codes are zero-padded (`4` → Almería, and `" 4 "` too), unknown codes return `false` so the caller emits `province: null` while keeping `province_code`. The names match the forms the incident feed publishes, so `--province alicante` filters the same across commands.
- **`type` and `speed_limit`** are present as `null` because the feed genuinely lacks them, pinned by test and visible in the golden.
- **`meta.published_at: null`** is deliberate: the feed carries a `publicationTime` **frozen at 2025-12-18**, byte-identical across the three snapshots. The parser never reads it and says why. `docs/feeds.md` §3 and §9 were corrected: the field exists, it is just not usable as freshness.
- **Tests with teeth**: dropping the INE table fails three tests; accepting a foreign dialect fails two.

## Veredicto

**Approve after one correction round.** The blocker was the most realistic failure yet — a user copying the wrong URL into their config — and it was invisible: the phantom radars looked entirely plausible. It is fixed with a test that fails against the previous code. The two minors were data facts the measurement surfaced and are now stated where a consumer will read them.

The golden did not need regenerating: the changes are guards, comments and documentation, so no serialised output moved.

### Not verified

- The full 737-radar production feed: only the trimmed 12-radar snapshot exists locally.
- The frozen timestamp across the whole feed: verified on the three trimmed snapshots only.
- Official INE spelling for the provinces absent from the incident snapshot (`Araba/Álava`, `Gipuzkoa`, `Bizkaia`): standard INE names, not cross-checked against a feed.
- FEED-05 and `route check` integration: later tasks.

### One pre-existing test line was changed

The synthetic `minimalRadarsBody` fixture in `radars_test.go` used `GUID_Test` as its set id; it now uses a real radar set id so its intent — "a genuine radar-dialect body" — survives the allowlist. Reported explicitly because it is the only existing test content this round touched.
