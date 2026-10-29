# Review report: WATCH-02

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date**: 2026-10-29
- **Scope reviewed:** `internal/service` (new) and the `service` subcommand group in `internal/cli/watch.go`, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| major | `internal/service/service.go:349` | **On macOS, uninstall could get stuck.** An unload of a job launchd does not consider loaded exits non-zero — which is exactly what happens when the load at install time failed. `Uninstall` treated that as fatal and aborted **before removing any file**, so the plist stayed on disk forever, `status` reported `installed: true, active: false`, and the only escape was deleting the file by hand. That violates S9's "no leftovers" on that path. The Linux path was already robust because `systemctl --user disable` is idempotent. | **Applied.** On darwin the unload failure is accumulated, the files are removed anyway and the failure is reported alongside them; Linux still aborts, because a failing disable there is a real problem. Regression test with a runner that fails only on `unload`, asserting the files are removed and `status` no longer reports installed. |
| minor | `internal/service/service.go:372-380` | `removeEmptyDirChain` walks up while `os.Remove` succeeds, so uninstall also removes pre-existing **empty** directories it did not create (an empty `~/.config`). | **Accepted.** It never removes a file or a non-empty directory (the removal fails), and an empty ancestor carries no information. Documented in the code. |
| minor | `internal/cli/watch.go:396-403` | Service envelopes carry `attribution` but not `meta.published_at`. | **Accepted as pre-existing.** Every non-feed command in the baseline does the same; the value would be a timestamp for data that was never fetched. |
| minor | `docs/ARRANQUE.md:173` | The table said the service schedules `watch check`; the generated units run `watch run`, which is the correct choice since `check` takes a name and writes nothing. | **Applied.** The line now says `watch run`. |
| nit | `internal/cli/watch.go:149` | The package-level `serviceContext` seam is acceptable (tests restore it with `t.Cleanup`, nothing uses `t.Parallel`). | **Accepted.** |

Verified by execution, no findings:

- **No leftovers**: the reviewer compared the whole home tree before install and after uninstall on both platforms, with foreign files planted in every directory the install touches — identical trees, foreign files byte-identical, empty home fully cleaned.
- **Generated content correct by reading**: systemd is a `Type=oneshot` service plus a timer with `OnBootSec`/`OnUnitActiveSec` and `WantedBy=timers.target`; launchd is a plist with `StartInterval` in whole seconds, `RunAtLoad` false and a `Label` matching the filename. Both invoke exactly `trafico watch run`.
- **Uninstall ordering** (deactivate → remove → reload → directory cleanup) is right, and `status` after a midway failure tells the truth: installed means every file is present.
- **Windows** fails with a typed error, a usage envelope and exit 2, writing nothing.
- **`status` when not installed** exits 0 with a report, not an error.
- **Tests with teeth**: `TestUninstallLeavesNoLeftovers` fails on any forgotten file **or created directory**; the interval is pinned exactly at the service layer.

## Veredicto

**Approve after one correction round.** No blockers; the one major made uninstall unrecoverable on macOS in a specific sequence, and it is fixed with a test that fails against the previous code. The minors are either applied or explicitly accepted as harmless.

### Not verified

- Real `systemctl` and `launchctl` behaviour: no test in this repository may invoke them, so the actual firing of the timer and the plist is judged by reading.
- POSIX permission bits at runtime (Windows host; the CI covers the Linux and macOS legs).
