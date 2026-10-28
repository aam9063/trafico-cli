# Review report: OPS-02

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-28
- **Scope reviewed:** `scripts/require_review.sh`, the `review-report` job in `.github/workflows/ci.yml`, `odd/reviews/README.md`, `odd/reviews/_template.md`, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `.github/workflows/ci.yml:44` | `${{ github.event.pull_request.title }}` is substituted into the `run:` script **before** the shell parses it, so a PR title containing `$(...)` or a `"` executes in the runner. A PR title is attacker-controlled text on any fork. The reviewer reproduced the substitution mechanism locally; the end-to-end path in Actions was not executed. | **Applied.** The title now travels through `env: PR_TITLE` and is referenced as `"$PR_TITLE"`, so it is data, never code. The vulnerable form also existed in `docs/ARRANQUE.md` §12, copied from the charter; corrected there too. |
| minor | `scripts/require_review.sh:88-93` | Headings inside a fenced code block satisfy the gate: ``` + `## Hallazgos` + `## Veredicto` + 210 bytes passes. | **Documented, not fixed.** Writing a Markdown-aware gate without dependencies costs more than it protects. Listed under "Known limitations". |
| minor | `scripts/require_review.sh:73` | `[ ! -f "$report" ]` follows symlinks, so a report can point at another task's file. | **Documented, not fixed.** The link is visible in the diff, so the cost of the hole is low. |
| nit | `scripts/require_review.sh:101-105` | The 200-byte minimum counts newlines, so one 199-character line passes. | **Documented.** The boundary `< 200` fails / `== 200` passes is intentional and stated. |
| nit | `scripts/require_review.sh` (ID pattern) | `OPS-2` and `OPS-002` gate literally, so a title using a non-canonical form demands a differently named report. | **Accepted.** It fails in the safe direction (blocks rather than passes). |

Two further defects were found by the parent during verification, before the
independent review ran, and are recorded here so the report is not flattering:

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| major | `scripts/require_review.sh` (ID pattern) | The pattern was case-sensitive, so a title saying `ops-01` slipped through the gate entirely: a gate bypassable by typing lowercase is not a gate. | **Applied.** The title is uppercased before matching; reports keep the canonical uppercase name. |
| minor | `scripts/*.sh` in the git index | `check.sh` and `trim-fixtures.sh` were stored as `100644`, so `./scripts/check.sh` fails with "Permission denied" on a fresh clone. CI did not notice because it invokes them through `bash`. | **Applied.** All three scripts are `100755` in the index. |

Verified working, by execution rather than by reading: two IDs in one title both
gated; `XOPS-01` and `HYPEROPS-01` do not match; `(ops-02)`, `[OPS-02]`, line
start and `-OPS-02` all gate; `OPS-013x` extracts `OPS-013`; duplicates
deduplicated; no ID exits 0; 0, 1 and 2 arguments exit 2; `###` and indented
headings fail; a headings-only report fails; exactly 200 bytes passes; a CRLF
report matches and counts consistently; `pipefail` survives a non-matching
`grep` through the explicit `|| true`; `bash -n` clean; no GNU-only flags.

## Veredicto

**Approve after one correction round.** The one blocker was the shell injection
and it is fixed with exactly the `env:` indirection the reviewer specified; the
remaining findings are explicitly documented limitations of a deliberately
mechanical gate. Per §10.5 the correction round is spent, so anything else goes
to the backlog instead of another round.

### Not verified

- Execution on the macOS and Linux runners themselves: the checks ran on Git
  Bash under Windows, and portability was assessed by inspection of the shell
  constructs used.
- The injection exploit end to end inside GitHub Actions: the substitution
  mechanism was demonstrated locally, not against a live workflow run.
- Whether `review-report` is wired as a required check in branch protection.
- Quality of future reports: the gate checks structure only, by design.
