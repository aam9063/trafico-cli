# Review report: WATCH-01d

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-29
- **Scope reviewed:** `internal/store` schema v3 with the `watches` table, and `internal/app/watchdef.go` (definition, validation, criteria)

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| major | `internal/app/watchdef.go:143-160` | **A route watch notified the same incident twice.** The per-segment mapping is correct for matching, but an incident whose PK range overlaps two segments reached the engine twice and produced **two `incident.new` for one incident** — plus the same doubling on `updated`. `route check` counts duplicates per segment honestly because it reports segments; a notification must not say the same thing twice. | **Applied.** The engine dedupes by `Incident.ID` inside `Check`, so any caller is safe and the invariant lives next to the events: one id produces at most one event of each kind per check. Test: the same incident handed in twice yields exactly one event. |
| major | `internal/app/watchdef.go:176-186` | **`--min-severity low` silently dropped every incident without a published severity.** The floor mapped to `[low, medium, high]`, excluding `unknown` — which the DGT publishes for the large majority of records (`docs/feeds.md` §4.5, ~87 %). The floor a user reads as "any severity" left the watch blind to most of the feed, with no error and no warning. | **Applied.** `low` now means "any published severity": `[unknown, low, medium, high]`. `medium` and `high` keep excluding `unknown`, because "not published" is not "at least medium". The rule is in the doc comment and pinned by a table test. |
| major | `internal/app/watchdef.go:249-269` | **A stored watch with no windows silently became 24/7.** `SpecFromStore` claimed the same validation as `ParseWatchSpec` but omitted the "at least one window" rule: a row with `"windows":[]` or `"windows":"null"` passed, and the engine treats an empty list as "always". A weekday-morning watch would alert around the clock after a hand edit. | **Applied.** `SpecFromStore` rejects zero windows with the same usage error as the parse path, shared through one constant. Both `[]` and `null` tested. |
| minor | `internal/app/watchdef.go:283-291` | The read path skipped two target rules the write path enforces: a `near` target with both a place and coordinates passed, and a `route` target whose route no longer exists passed too, each failing later at run time. | **Applied.** `validateTarget` runs again on the read path. A stale route stays `not_found` (exit 4), which is what ARRANQUE §6.3 assigns to a missing route and what the route commands already do. |
| minor | `internal/app/watchdef.go:130-137` | Zero sinks were accepted, creating a watch whose events are appended and never delivered. | **Kept allowed, documented.** The spec does not mandate `--notify`, and a watch that only records history is a legitimate use. The doc comment now says so and notes that `watch list` must show it. |

**A correction to the reviewer's brief, not to the code:** the review request expected a missing route to be a usage error (exit 2). The reviewer checked `ARRANQUE §6.3` and the implementation is right: a route that does not exist is `not_found`, exit 4. The wrong expectation was the parent's.

Verified by execution, no findings:

- **The v2 → v3 migration against a database built by the previous binary**: state rows and the pending event survived, `watches` was created, and new writes work. Step 2 is additive-only. Two processes opening the same v2 file concurrently both migrate without a duplicate-table or lock failure.
- **`Target` union**: malformed JSON produces a readable per-row error, a kind/field disagreement is an internal error, `Watch(name)` on an absent name is `not_found`, and `Watches()` orders by name.
- **Validation**: every bad input is `errs.NewUsage` — unsafe name, unparsable target, missing route, no windows, bad window, bad severity, unknown sink, interval under two minutes, five `near` shape cases.
- **P-07**: the payload has no field for a name, route, place or coordinate; `Criteria` carries only roads, PK and severities; the database is `0600` in a `0700` directory on every open.

## Veredicto

**Approve after one correction round.** Two of the three majors would have made a watch silently useless rather than visibly broken — a blind spot on 87 % of the feed, and an always-on watch where the user set a window — which is exactly the class this project has been chasing all ola. The third duplicated notifications. All are fixed with tests that fail against the previous code.

### Not verified

- POSIX `0600`/`0700` at runtime (Windows host; the tests skip and run on the Linux and macOS CI legs).
- End-to-end `watch add` / `watch run`: the CLI layer is the next task, and it is the first real caller of `Criteria`.
- Live network paths, forbidden by rule.

### Handed to the next task

`Criteria` returns a **slice** of criteria, one per declared route segment, because a PK range requires exactly one road and collapsing a multi-segment route to one criterion would over-match whole roads. With the engine dedupe in place the caller no longer has to worry about duplicates.
