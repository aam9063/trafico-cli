# Review report: WATCH-01a

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-29
- **Scope reviewed:** `internal/window` — the `--window` parser behind `watch`, before its commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

This is the piece of Ola 3 whose bugs are easiest to miss: a wrong window does
not fail, it silently stops alerting on a day the user commutes, or alerts an
hour after they arrived. The reviewer wrote twelve probes from the spec rather
than from the test table.

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| minor | `window.go:1`, `doc.go:1` | Two package doc comments: `go doc` printed the thinner one first (`doc.go`'s one-liner), so the godoc summary was the less informative of the two. | **Applied.** `doc.go` no longer carries a package comment; the full documentation stays on `window.go`. Verified with `go doc`. |
| minor | `window.go:267-275` (`madridOnce`) | A silent fallback to UTC if `LoadLocation` ever failed: every window would shift an hour with no error path, because `Matches` returns a `bool`. | **Accepted, no change.** `cmd/trafico` imports `time/tzdata`, so the path is unreachable in practice, and `Matches` has no way to report it. Noted for the CLI layer if a startup check is ever wanted. |
| nit | `resolveDay` | NFD-decomposed accents (base letter plus combining mark, common when a day name is pasted from macOS) are rejected. | **Documented.** It fails loudly with a usage error naming the token, so it is not a silent trap, and normalising Unicode would mean a new dependency for a case the user fixes by typing the day. |
| nit | `window.go:134` (`String`) | The zero value rendered `" 00:00-00:00"`: a leading space and a time range for a window with no days, which does not parse back. Unreachable from `Parse`, but `watch list` renders stored windows. | **Applied.** A window with no days renders `(sin días)`, with its own test. A canonical form that cannot be parsed back is a trap, and `watch list` is where a stale store row would surface it. |

Answers to the questions that mattered, all observed rather than inferred:

- **Wrapping ranges**: `vie-lun 07:00-09:00` matches Friday, Saturday, Sunday and Monday, not Tuesday. With times crossing midnight (`vie-lun 22:00-02:00`), **Monday 01:00 matches** — Sunday is declared, so its night runs into Monday — and so does Tuesday 01:00 via declared Monday, which is the same rule applied consistently. The reviewer's own first expectation was wrong here and the code was right.
- **Midnight ownership**: with only `vie` declared, `22:00-02:00` matches Friday 23:00 and Saturday 01:00, and correctly does **not** match Saturday 23:00 or Monday 01:00.
- **End of window**: exclusive at minute granularity — 08:59:59.999999999 matches `07:00-09:00`, 09:00:00.000 does not.
- **UTC input**: a `time.Time` built in UTC that is 08:00 in Madrid matches a 07:00-09:00 window.
- **DST, both transitions**: spring forward, a `02:30-03:30` window covers only the real instants 03:00–03:29:59 CEST and does not panic; fall back, 02:30 matches in **both** occurrences while 02:00–02:29 CET is correctly excluded. The double match is documented and acceptable: the worst case is that alerts resume during the repeated hour, and pinning per occurrence would need instant-based windows, which the rest of the spec does not use.
- **The worker's corrected tests**: verified right, not bent. `vie-lun` really is Monday, Friday, Saturday and Sunday, and Sunday 03:00 under `vie-lun 22:00-02:00` is genuinely outside the window.
- **Robustness**: 22 malformed specs — empty, one-digit hour, `25:00`, `07:60`, `24:00`, `07:00-07:00`, unknown days, `lun--vie`, trailing comma, en-dash, three-part time range, extra fields — all `*errs.Error` with exit 2 and no window returned. No silent default.
- **`String()` round-trip**: `Parse(w.String())` equals `w` for every case built, including wrapping windows and the full week. Only the zero value failed, which is the nit fixed above.
- **`Madrid()`**: cached with `sync.OnceValue`, same pointer across calls.

## Veredicto

**Approve.** No blocker and no major. Two minors and two nits; the two that were worth changing are applied and the other two are documented as the deliberate trade-offs they are.

### Not verified

- The real booksy-cli `--when` format: the spec in `docs/ARRANQUE.md` §5.5 was taken on faith, and the day vocabulary here is self-consistent.
- `make check` and the full suite: the reviewer ran `go test ./internal/window/`, `go build ./...` and `go vet` only; the parent ran the rest.
- Integration with `watch list` and `watch add`: those do not exist yet (T4). The zero-value fix anticipates the one place where a degenerate stored window would have reached a user.

### Local verification note

The parent could not run the suite on Linux with `-race` for this unit: Docker Desktop was not running after a restart and the parent does not start GUI applications. The CI matrix covers `ubuntu-latest`, `macos-latest` and `windows-latest` for this PR, which is where the platform-specific leg happens.
