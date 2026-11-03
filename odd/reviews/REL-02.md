# Review report: REL-02

- **Reviewer:** parent orchestration pass, verified by running the installer on the affected platform
- **Date:** 2026-11-03
- **Scope reviewed:** Windows support in `install.sh` and the harness that assumed its absence

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `install.sh` (`detect_os`) | **The installer refused the platform the project's own author uses.** It rejected `MINGW*`/`MSYS*`/`CYGWIN*` with a message pointing at the `.zip` — a good message, and a good failure, but it left an entire platform without the one-line install that is the installer's whole purpose. The release publishes `windows_amd64` and `windows_arm64`; the gap was in the script, not the assets. | **Applied.** Git Bash, MSYS2 and Cygwin map to `windows`, the `.zip` asset is selected, and `unzip` extracts it with a clear error when it is missing. |
| minor | `install.sh` | The installed binary is `trafico.exe`, not `trafico`. A script that copies the wrong name leaves an empty install directory and a confusing `command not found`. | **Applied.** `binary_name_for` decides the archive name and the installed name from one place, so the two cannot drift apart. |
| minor | `scripts/test-install.sh` | Four assertions **asserted the old refusal**: that Git Bash was rejected, that Cygwin was rejected, that the message pointed at the `.zip`, and that `windows` was not a tarball target. They were correct tests of the old behaviour and became wrong the moment the behaviour changed. | **Applied.** They now assert the mapping (`windows`), the asset (`trafico_0.1.0_windows_amd64.zip`) and the binary name (`trafico.exe`), plus a Linux case so the split is pinned from both sides. 31 → 33 assertions. |

Verified by execution, no findings:

- **The installer was run on the platform it now supports**, not just unit-tested: `sh install.sh` on Git Bash downloaded `v0.2.0`, verified the SHA-256, installed `trafico.exe`, and the binary reports `0.2.0 (b787a2e) built 2026-11-03T10:16:05Z`.
- **The checksum gate still runs before extraction**, which is the property the installer exists for and the one a platform branch could have bypassed.
- **`check.sh --strict` is green** and `sh -n` passes on both scripts.

## Veredicto

**Approve.** This is the same defect as FORMAT-01 in a different costume: a feature that was reported as done because its error path was written, not because its happy path was ever exercised. The installer "handled" Windows by refusing it.

The honest note is that it took the author running it to find out.

### Not verified

- `unzip` on a machine that lacks it: the failure path is explicit and untested here because this environment has it.
- `windows_arm64`: the mapping supports it and no Windows ARM machine was available to try.
- The harness still runs in no gate (REF-3). The fact that it caught all four stale assertions today is an argument for wiring it in, not against.
