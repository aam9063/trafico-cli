# Review report: FORMAT-01

- **Reviewer:** `gentle-ai-worker` implementation pass over a parent-measured scope, verified by execution
- **Date:** 2026-11-02
- **Scope reviewed:** the inversion of the default output format, before its commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| design | `AGENTS.md` | **This change deliberately breaks a hard rule of the project.** `stdout solo JSON` existed because the CLI serves agents; the default is now human-readable text and an agent must pass `--json`. | Recorded as a decision, not a defect: the user chose it explicitly over terminal auto-detection, and the reasoning is in `odd/tasks/format-01-salida-legible.md` (L2). The rule, `README.md` and `docs/ARRANQUE.md` now state the new contract in ten places, so no document still promises the old default. |
| blocker | `internal/cli/incidents.go`, `route.go` | **Three commands emit an envelope and never registered the flag**: `incidents show`, `route show`, `route check`. With the default inverted they would have kept printing raw JSON while every other command printed text — the one inconsistency a user would hit immediately and blame on the tool. | **Applied.** `addPrettyFlag` registered on all three; every `output.Write` envelope site was cross-checked against the registrations, and `version` keeps rejecting the flag on purpose (CMD-02). |
| minor | `internal/cli/*`, docs | A first implementation pass reached 33 of 37 tests with the tree in red, because the three missing registrations read as test failures. | **Applied.** That pass was reverted rather than committed, and the scope was measured into the feature document before the second attempt. The four remaining failures were confirmed to be the missing flag and nothing else — no flag-lifecycle bug existed, so `--json` after a positional argument always parsed correctly. |

Verified by execution, no findings:

- **The JSON contract did not move** (S3): no `output.Write`, no serialiser and no golden changed. `--json` produces the same bytes as before the inversion; the full suite is green across all 16 packages.
- **The `--pretty` path still works**: three existing tests pin it, including `version --pretty` being rejected with exit 2.
- **Diagnostics stay on stderr** (P-01): untouched by this change and still asserted.
- **65 tests pass in `internal/cli`**, including the new `--json` coverage for the three commands that had none.

## Veredicto

**Approve.** The stated goal — "que no salga en formato json" — is met, and the part that made it risky was not the default itself but the audit behind it: three commands would have been left behind, and each one would have failed silently in the wrong direction.

The rule change is the significant part and it is documented, not smuggled: ten places across three files no longer promise JSON by default.

### Not verified

- The pretty renderers themselves under the new default: they were already exercised by their own tests through `--pretty`, and this change only alters which path is taken, not what either path emits.
- The release does not exist yet. It is deliberately blocked on this change so the first published contract is the one that stays.
