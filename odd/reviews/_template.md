# Review report: <TASK-ID>

- **Reviewer:** <name / model of the read-only reviewer subagent>
- **Date:** <YYYY-MM-DD>
- **Scope reviewed:** <diff range, e.g. `dev..<sha>`>

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| <blocker\|major\|minor\|nit> | <path>:<line> | <what is wrong> | <concrete proposal, or `REF-<n>` if it goes to the backlog> |

## Veredicto

<approve / approve after one correction round / reject — and in one or two
sentences, why.>

### Not verified

<What this review did not check: e.g. live feed behavior, Windows runner,
benchmark on other hardware, anything outside the frozen diff range.>
