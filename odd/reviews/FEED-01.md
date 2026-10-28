# Review report: FEED-01

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-28
- **Scope reviewed:** `internal/feeds/` — `client.go`, `cache.go`, `feed.go` and their three test files

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `feed.go:199-200` | A connection dropping **mid-body** surfaced as `internal` / exit 1. The cache fallback only covered `Client.Get` errors and 429/5xx headers; a failure inside `io.Copy` during `Store` became an internal error. On a 7 MB download over a bad link this is the realistic failure, and it broke both §5.6 (fallback) and §6.3 (exit 1 means "unexpected"). Probed with a server that promises `Content-Length: 1000`, writes 10 bytes and drops: with an expired cache the result was exit 1 instead of the fallback; with no cache it should have been exit 6. | **Applied.** `Store` now wraps the response body in a reader that records read-side failures and returns the `ErrBodyRead` sentinel; a local write failure still returns `internal`. `Fetch` routes `ErrBodyRead` through `fallbackAfterFailure(..., errs.NewNetwork(...))`. Two regression tests: `TestFetchMidBodyFailureFallsBackToCache`, `TestFetchMidBodyFailureWithNoCacheIsNetworkError`. |
| blocker | `feed.go:196-205` + `cache.go:97-146` | An HTTP 200 with **0 bytes poisoned the cache**: `Store` renamed the empty entry into place *before* `Fetch` rejected it, so the next call inside the TTL answered `FromCache: true`, `Stale: false`, exit 0 with an empty body. A failure silently turned into a success. | **Applied.** `Store` rejects a zero-byte body with `ErrEmptyBody` before the rename, so the poison never lands; `Fetch` maps it to `feed_changed`. Regression test: `TestFetchEmptyBodyDoesNotPoisonCache`, which asserts the **second** call goes back to the server (`requests == 2`) and fails the same way. |
| minor | `cache.go:70-78`, `feed.go:157` | A backwards clock jump made `Age` negative, so `Fresh` stayed true far beyond the TTL and the fallback warning could print `hace -5s`. | **Applied, differently than proposed.** `Age` clamps at zero so nothing prints a negative age, but `Fresh` treats a future-dated entry as **not fresh** and refetches. Clamping alone would have made an entry look fresh forever while the clock stayed behind. Test: `TestCacheAgeClampsNegativeClock`. |
| nit | `feed.go:241` vs `feed.go:157` | `FetchedAt` was captured before the download started, so a slow (up to 30 s) transfer made the entry look fresher than it was, shaving the effective TTL. | **Applied.** The timestamp is taken once the response headers are in, and `Result.FetchedAt` now uses the value stored with the entry. |

Checks that passed and produced no findings, verified by running them rather
than by reading:

- **P-06, one request for two fetches**: `TestFetchFirstMissThenCacheHit` asserts `requests == 1`, and the fresh-hit path returns before any HTTP work.
- **Streaming**: no `io.ReadAll` on the fetch path; the body is handed over and copied to disk. `ReadAll` appears only in tests.
- **No dead conditional headers**: none are sent, matching `docs/feeds.md` §2, and a test asserts it.
- **P-08 honesty**: every fallback result sets `Stale: true` unconditionally and carries a `Warning`. Apart from the two blockers above, no path served stale data after a failure without saying so.
- **Exit codes**: 429 → 5, 503 → 5, 404 → 7, network with no cache → 6, unknown feed → 2, all asserted.
- **Atomicity**: temp file in the same directory, fsync, chmod, rename; a failed copy leaves the previous entry intact.

## Veredicto

**Approve after one correction round.** Both blockers were real and both are
fixed with regression tests that fail against the previous code: the mid-body
failure now falls back or exits 6, and an empty body can no longer be served as
a successful download. Per §10.5 the correction round is spent, so nothing else
changed beyond the two advisories above, which were cheap and are covered by
tests.

Verification of the corrected code: `go test ./internal/feeds/ -race` inside
`golang:1.26.6` on Linux — **24 tests pass**, including the `0600` permission
test that skips on Windows.

### Not verified

- Behaviour against the real DGT feeds: no test in this repository touches the
  network, by rule, and the nightly job is the one that would notice a change.
- Concurrent `Store`/`Load` across processes (the review probed a single
  process), and Windows rename-over-an-open-file behaviour.
- `internal/datex2` interaction: out of scope for this task.
