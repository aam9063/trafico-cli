# Review report: LOCK-01

- **Reviewer:** parent orchestration pass, prompted by a real CI failure
- **Date:** 2026-11-03
- **Scope reviewed:** `TestWithWatchLockStaleTakeoverIsExclusive`

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/app/watchrun_test.go` | **The test measured the wrong guarantee.** It collected each cycle's start and end into `intervals`, then checked `len(intervals) != 1`. A lock does not promise that only one caller ever runs: it promises that two never run **at the same time**. A worker that arrives after the winner has released acquires the lock legitimately and runs its own cycle — correct behaviour, reported as a failure. The macOS runner is slow enough for that to happen; `round 21: 2 concurrent cycles executed the critical section` was not concurrency. | **Applied.** The test now checks what it was already collecting: intervals are sorted and compared, and any overlap is a failure naming both spans. `len == 0` is also a failure — the lock must not lock everyone out. |
| minor | `internal/app/watchrun_test.go` | The data proved the point and was thrown away: `start` and `end` were recorded per cycle and never read. | Folded into the fix above: the fix is made of the field that was already there. |

Verified by execution — **the test has teeth**:

- With `LOCKFILE_EXCLUSIVE_LOCK` removed from the Windows lock, the test fails: `round 0: two cycles overlapped: [12:56:47.2143881 → .2250559] and [12:56:47.214905 → .2250559]`.
- With the lock restored, it passes three consecutive runs.
- A first attempt to disable the lock in `lock_unix.go` proved nothing: that file is `//go:build !windows`, so on this machine the change had no effect and the test passed for the wrong reason. Caught before it became a false claim.

## Veredicto

**Approve.** The lock was never broken. The test was, and it had been failing on macOS for a reason it could not articulate.

Worth recording how it surfaced: I merged PR #59 after `gh pr checks --watch` exited non-zero and I continued anyway. **That was my error, not the tooling's** — a red watch means look, not proceed. The failure it hid was a test asserting a guarantee nobody had promised.

### Not verified

- The macOS runner itself: the fix removes the timing dependence, but only the next CI run on that platform proves it.
- Whether other timing-sensitive tests in the suite share this shape: not audited.
