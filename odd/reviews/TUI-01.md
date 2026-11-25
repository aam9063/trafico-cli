# Review report: TUI-01

- **Reviewer:** parent orchestration pass, verified by running it
- **Date:** 2026-11-03
- **Scope reviewed:** the terminal interface in `internal/tui` and its entry point

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| design | `cmd/trafico/main.go` | **A TUI that opens in a pipe is worse than no TUI.** Escape codes in a log file, or in an agent's stdout, break the contract the whole project is built on. | **Applied.** `trafico` opens the interface only with no arguments **and** a character device on both stdin and stdout. Verified: `trafico < /dev/null` prints the help and exits 0. |
| minor | `internal/tui/views.go` | The frame broke open at the right edge: a 40-rune description pushed the border out of the terminal. | **Applied.** The last column takes the remaining width and is truncated, and every other column is measured from its widest cell capped at 26. On a narrow terminal the free-text column is dropped instead of clipping. |
| minor | `internal/tui/tui.go` | `tea.WithAltScreen()` blanks the terminal history on exit, and it also made the interface uncapturable for visual review. | **Applied.** Removed. The window scrolls with the session instead of replacing it. |

Verified by execution:

- **The user ran it and confirmed it draws**: rounded frame, block-letter banner, aligned table, selected row in the accent colour, high severity in the warm tone, shortcut bar at the bottom.
- **`check.sh --strict` is green**, `CGO_ENABLED=0` builds, and the binary is installed for a real-terminal test.
- **Four views** — incidents, panels, radars, sections — on keys `1`–`4`, each calling the same `internal/app` function its command calls. Switching back to a loaded view does not refetch.

## Veredicto

**Approve for the four views, pending one visual confirmation.**

### Not verified

- **The three new views have not been seen.** Only the incidents view was confirmed by a human. The table code is shared and the data path is the same, but "should look the same" is not the same as "looks the same".
- **VHS cannot capture this interface**: frames come out blank whether or not alt screen is used. Visual review of a TUI in this environment needs a real terminal, which is why the check is a human one.
- Routes are not a view yet: `app` exposes them through the CLI layer, and mirroring that in the TUI would have meant a fourth data shape on the last turn. Recorded for the next session.
