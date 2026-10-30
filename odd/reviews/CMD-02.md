# Review report: CMD-02

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-28
- **Scope reviewed:** `internal/app/feeds.go`, `internal/app/doctor.go`, `internal/cli/feeds.go`, `internal/cli/doctor.go`, their four test files, and the registration plus global `--pretty` flag in `internal/cli/root.go`

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

The reviewer did not read the code to agree with it: it built the binary, ran it
against isolated `XDG_CONFIG_HOME`/`XDG_CACHE_HOME` directories, stood up a local
origin server and counted requests. Every finding below is backed by a probe.

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/cli/feeds.go:39-52,61-74`, `internal/cli/doctor.go` | The cache-fallback warning never reached stderr. It was computed, carried all the way to `FeedStatus.Warning`, and dropped: stdout showed `from_cache`, `stale` and the warning, stderr was 0 bytes. §5.6 requires the warning on stderr; §6.1 puts diagnostics there. | **Applied.** Both RunE functions call `output.Diag(cmd.ErrOrStderr(), …)` for each status with a warning, before writing JSON. stdout stays pure. |
| blocker | `internal/app/feeds.go:150-162` | The `parseErr` branch discarded `FromCache`, `Stale` and `Warning`, and its message said the feed "respondió" — three falsehoods when the body came from the cache after a network failure. That is precisely the "DGT changed the format and there is no network" case of §13. | **Applied.** The branch propagates the three fields and words a cache serve differently from a live 200. |
| major | `internal/app/feeds.go:59-60,167-183` | `feeds[].fetched_at` serialised in local time (`+01:00`) while `meta.fetched_at` in the same document ended in `Z`. §6.1 requires UTC. | **Applied.** A `utcTimePtr` helper normalises every `*time.Time` stored on `FeedStatus`. `internal/output` untouched. |
| major | `internal/cli/root.go:35,78-87` | The new global `--pretty` was silently ignored by `trafico version`: the flag was accepted, plain text was printed and the exit code was 0. | **Applied.** The flag is no longer persistent on the root; `addPrettyFlag` registers it only on the commands that render an envelope. `trafico version --pretty` now exits 2 as an unknown flag, which is honest instead of silent. |
| minor | `internal/cli/feeds.go:29-33` | Bare `trafico feeds` printed cobra help on stdout, breaking P-01. | **Applied.** It returns `errs.NewUsage("indica un subcomando: list o status")`: envelope on stderr, exit 2. The root command's own help is out of scope. |
| minor | `internal/app/feeds.go:63-69` | `feeds list` reported `status: "ok"` for feeds it never checked, which reads as verified health. | **Applied.** `list` reports `enabled`/`disabled`; `ok` and `error` are reserved for `feeds status`. The constant and the doc comment state the two vocabularies. |
| minor | `internal/cli/feeds.go:88-95` | `meta.stale` and `meta.from_cache` asserted `false` while a served entry was stale and cached, so the document read as fresh. | **Applied.** Both are derived from the per-feed statuses (true if any feed carries them). `feeds list`, which checks nothing, keeps them false with a comment saying why. |
| nit | `internal/cli/doctor.go:32`, `internal/app/doctor.go:71` | The config was loaded twice in the doctor path. | Not fixed: harmless, and touching it would move the error reporting of a diagnostic command. |
| nit | `internal/app/feeds.go:167-183` | The cache entry is read twice per `feeds status`; for the situations feed that streams 5.8 MB again only to close it. | **Deferred to `REF-1`** (`odd/backlog.md`): fixing it means exposing `Entry` from `Result` in `internal/feeds`, which is the surface of a closed task. |

One further defect was found by the parent **after** the reviewer's pass, and is
recorded here rather than quietly fixed:

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| minor | `internal/app/doctor.go:120` | The same `meta` aggregation gap the reviewer flagged for `feeds status` also applied to `doctor`, which the reviewer's Minor 7 did not name: `doctor` reported `meta.from_cache: false` while its own feed entry said `from_cache: true`, and its `meta.published_at` was `null` with a known publication time. | **Applied.** The aggregation moved to `app.DeriveMeta`, used by both commands so they cannot drift again, and `meta.age_seconds` is now derived from the publication time (P-08) instead of being left `null`. Regression test: `TestDoctorMetaReflectsCacheFallback`. |

Verified by execution after the fixes: build, vet, staticcheck and gofmt clean;
`TestDoctorMetaReflectsCacheFallback` and the ten `feeds`/`doctor` tests pass;
the real binary returns `version --pretty` → exit 2, bare `feeds` → exit 2, and
`feeds[].fetched_at` ends in `Z`.

A note on the test that caught the last one: the first version of
`TestDoctorMetaReflectsCacheFallback` failed on Windows with
`The process cannot access the file because it is being used by another
process` because the seed `Result` was never closed. That is the risk FEED-01
documented in its own report — a `Result` held open blocks the rename and the
delete — showing up in a test rather than in production.

## Veredicto

**Approve after one correction round.** Both blockers were real and both are
fixed with regression tests that fail against the previous code; the two majors
touched the contract (UTC dates, a silently ignored flag) and are closed. Per
§10.5 the correction round is spent, so the remaining nits went to `odd/backlog.md`
as `REF-1` instead of another round.

### Not verified

- `trafico doctor` with **no config file** through the binary: the compiled
  defaults enable `dgt-situations`, so that path would hit the real DGT and the
  review did not touch the network. Only the app-level variant is covered.
- 429/5xx and connection-drop fallbacks through the real binary: covered by unit
  tests with `httptest`, not re-probed end to end.
- Interruption (exit 130), Windows ACL specifics, and the `watch`/`sink`
  interactions, all out of scope for CMD-02.
- Live feed behaviour: no test in this repository touches the network by rule.
