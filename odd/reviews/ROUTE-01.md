# Review report: ROUTE-01

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-28
- **Scope reviewed:** `internal/config` (the new `[routes]` section), `internal/app/places.go`, `internal/app/routes.go`, `internal/cli/place.go`, `internal/cli/route.go`, their tests and `internal/app/testdata/route-check.golden.json`, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

The reviewer ran the suite in a scratch copy, probed the binary with an
isolated `XDG_CONFIG_HOME`, hashed the config file around every rejected
removal, and **mutated `validatePlace`** to confirm a regression would be
caught.

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| minor | `internal/app/routes.go:95-113`, `internal/app/places.go:51-67` | `route add` and `place add` silently **replace** an existing name, exit 0. Overwrite is the only edit path and the spec is silent, but a re-run of an old `add` command can wipe a multi-segment route with no signal. | **Documented.** Both commands now carry a `Long` that states the replacement in the help text. Discarded the alternative of a `--replace` flag: it would make the normal edit path require a flag the spec never mentions, and the help text is where a user looks before running the command. |
| nit | `internal/app/routes.go:196-200` | Overlapping segments (`CV-86:0-6` and `CV-86:2-3`) count the same incident twice in `summary.incidents` and `meta.total`. | **Accepted as is**, with the code comment making it explicit. Summing per segment is the honest reading: the user asked about two segments and both contain the incident. |
| nit | `internal/cli/place.go:129-133`, `internal/cli/route.go:206-210` | `meta.fetched_at` is `time.Now()` on commands that fetch nothing, while `published_at` is null. | **Accepted**, consistent with the `feeds list` precedent already in `dev`. `fetched_at` describes when the response was built; `published_at` describes the data, and inventing one would be the real defect. |

Verified by execution, no findings:

- **Declaration order** holds structurally (the stored slice is iterated, never sorted) and live: a three-segment route came back in declaration order with the feed publishing only one of the roads. The feed order was deliberately reversed in the test.
- **One fetch per `route check`**: `fetchSituations` is called once before the segment loop and no network call exists inside it; two hit-counting tests pass (three segments → one request, two segments → one request).
- **The `--yes` gate**: `sha256` of the config is **byte-identical** before and after `place remove` and `route remove` without the flag, exit 2, and the gate runs **before any load**, proven with a `0644` config and with a non-TOML one — both returned `confirmation_required` instead of a load error.
- **P-07**: no cache or sink reference in the new files, personal names never reach the fetcher, the cache-fallback warning carries only the feed name and timestamp, and every write goes through the atomic `config.Save` with `0600`.
- **`internal/config` did not regress**: the diff is purely additive. The reviewer removed the longitude check from `validatePlace` in the scratch copy and `TestValidateFailures/place_with_longitude_out_of_range` **failed**, so the worker's reported slip is covered by a test.
- **Contract**: a command that fetched nothing reports `published_at: null` and keeps `schema_version` and `attribution`; `route check` carries the feed's real publication time and `meta.total == summary.incidents`; a missing route exits 4 and makes **zero** requests (existence is checked before the fetch); a hand-edited config with a malformed segment is rejected loudly on load.
- **Names**: `../escape`, `a/b`, `""` and `.oculto` all exit 2; dotted names round-trip through go-toml quoting.

## Veredicto

**Approve.** No blocker and no major: the one minor is closed by documenting the replacement in the help text, and the two nits are behaviours the code already states and the reviewer agreed are honest. Nothing else changed.

### Not verified

- Request counting at the binary level: the reviewer's background counting server was unreachable from the CLI in this Windows/MSYS environment, and the 60 s cache would mask per-segment fetches anyway. The single-fetch guarantee rests on two passing hit-counting tests and on the code path having one fetch call before the loop.
- `0600` on disk empirically: POSIX bits do not exist on this host. Verified in code and by the permission tests that run on Linux.
- The exit-6 path for `route check` specifically: only the cache-fallback path was observed; it shares `fetchSituations` and the root error handling with `incidents`, which are covered.
- Lint and `go vet` were run by the parent, not by this review.
