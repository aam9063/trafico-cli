# Review report: WATCH-03

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date**: 2026-10-29
- **Scope reviewed:** the three debts the Ola 3 reviews identified — `--emit-initial`, `chat_id` for telegram sinks, and the undocumented run fields — across `internal/watch`, `internal/store` (schema v5), `internal/app`, `internal/cli`, `internal/config` and `docs/ARRANQUE.md`

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

These are three loose ends rather than one feature, so the review question was
narrow: does each one actually close, without leaving the rule it touches
duplicated or half-wired?

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| minor | `internal/config/load.go` (`rawSink`, `decode`) | `config.Save` began writing `chat_id` while the strict loader's `rawSink` mirror did not know the field, so a config the tool itself wrote failed to load back: `strict mode: fields in the document are missing in the target struct`. A field that cannot be read back is worse than no field. | **Applied.** `rawSink` gains a tagged `ChatID *string` and the decode merges it only when the file set it, with the same semantics as `url`, `topic` and `template`. `TestLoadRoundTripsTelegramChatID` fails if the merge line is removed. |
| — | `internal/watch/watch.go` | The baseline rule could have been duplicated: a CLI flag that made the command layer decide what the first check emits would be a second definition of the same rule. | **No finding.** The flag only fills `Definition.EmitInitial`; the sentinel, the dedupe and the append-before-replace ordering all stay in the engine, and `watch run` needs no flag because the choice is stored with the watch. |
| — | `internal/store/store.go` (migration 4→5) | Existing rows must default to the silent baseline, not to announcing. | **No finding.** The column defaults to `0` and `TestOpenMigratesV4Database` pins that a v4 database migrates with the flag off, so nobody who created a watch before this change starts receiving baseline announcements. |
| nit | `docs/ARRANQUE.md` §5.5, §7 | The document that describes the contract did not mention `skipped_interval`, `last_check_at`, `--emit-initial` or the telegram `chat_id`. | **Applied.** Both sections updated, including a `[sinks.telegram]` example with `chat_id`. |

Verified by execution:

- **`--emit-initial` real behaviour**: a watch created with the flag announces every incident present at baseline, once; re-running does not repeat them; a twin watch without the flag stays silent on the same data. Both observed through the public interface, not by reading.
- **Telegram sink end to end**: a sink with `chat_id` builds and delivers a `chat_id` + `text` body against a local server; without it, `watch add` exits 2 with a message naming `chat_id` — an unusable sink is rejected where the user can fix it, not silently dropped at delivery.
- **Round trip**: `Save` then `Load` preserves `chat_id`, and a config without it loads with the field empty.
- **No regression in the baseline**: the pre-existing silent-baseline tests pass unchanged, which is the evidence that the flag is an opt-in and not a default flip.

## Veredicto

**Approve.** No blockers and no majors. One minor was real and fixed with a test that fails without it; the rest are either verified as non-issues or documentation that now matches the code.

### Not verified

- Real `help`/`systemctl`/`launchctl` behaviour and POSIX permission bits at runtime: the CI covers the Linux and macOS legs; this review ran on Windows.
- The five native RDD reviews remain blocked by the harness configuration described in the Ola 3 closing document; this report and the six before it come from independent read-only reviewers, which is the coverage the project actually had during the ola.
