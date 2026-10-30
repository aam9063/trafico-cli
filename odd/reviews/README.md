# Review reports (`odd/reviews/`)

A review report is the record of the independent review pass that closes a
task (docs/ARRANQUE.md §10.5). It exists so that the findings — and the
verdict — survive the conversation in which the review happened, and so that
CI can mechanically check that a task-bearing PR carries its report.

## The gate

CI runs `scripts/require_review.sh` on every pull request
(`.github/workflows/ci.yml`, job `review-report`):

- If the PR title names **no** task ID (`<PREFIX>-<number>`, prefixes `H`,
  `SPIKE`, `OPS`, `CORE`, `FEED`, `MODEL`, `CMD`, `ROUTE`, `WATCH`, `AGENT`,
  `QA`, `REL`, `REF`, `CHORE` — e.g. `H-01`, `SPIKE-01`, `FEED-02`, `OPS-02`),
  the gate passes: not every PR maps to a backlog task.
- If it names a task ID, `odd/reviews/<ID>.md` must exist, must contain the
  exact headings `## Hallazgos` and `## Veredicto`, and must carry at least
  200 bytes of content beyond those headings. An empty report satisfying the
  gate is worse than no gate.

The gate is mechanical on purpose: it checks structure, never quality.

## Required structure (docs/ARRANQUE.md §10.5, in this order)

1. **Correctness.** Does the change do what the task says, correctly?
2. **P-04 (fidelity to the source), P-05 (attribution), P-07 (location
   privacy), P-08 (visible freshness)** — in that order.
3. **JSON contract and exit codes** (section 6.3): no invented fields, `null`
   for absent feed data, `attribution` and `meta.published_at` present.
4. **Time zones.**
5. **Simplicity and duplication.**
6. **Tests:** what they cover, what they miss.
7. **Parser performance, with benchmark evidence** (`make bench` output or
   equivalent numbers in the report itself — a claim without evidence does
   not count).

## Findings: severity scale

| Severity | Meaning |
|---|---|
| blocker  | Must be fixed before merge; the change is wrong, unsafe, or violates a hard rule (§6.3, P-04/05/07/08, no-network). |
| major    | Real defect or contract deviation that will bite later; fix now or open a tracked task. |
| minor    | Small defect, naming, or clarity issue; fix in the correction round. |
| nit      | Taste or polish; note it, do not block on it. |

## Rules

- The report lives at `odd/reviews/<ID>.md`, one per task, named by the task
  ID (`OPS-02` → `odd/reviews/OPS-02.md`). Never invent a report for a task
  that was not actually reviewed.
- There is **one** correction round. After it, the verdict is final: either
  the findings are addressed or they are explicitly deferred.
- Large refactors proposed during review do not expand the task's scope:
  they go to the backlog as `REF-<n>`.
- Start from `odd/reviews/_template.md`; keep the required headings exactly
  as they are, or the gate will fail.

## Known limitations of the gate

The gate is mechanical, and these are the holes it knowingly leaves. They are
documented so nobody mistakes passing it for being reviewed.

- **A heading inside a fenced code block satisfies the gate.** A report whose
  `## Hallazgos` and `## Veredicto` live inside a ``` block passes, as long as
  it also carries 200 bytes of text. Ripgrep-free, dependency-free parsing of
  Markdown is not worth the complexity here: the reviewer signs the report and
  a human reads it.
- **Symlinks are followed.** `odd/reviews/<ID>.md` pointing at another task's
  report passes. The link is visible in the diff, so the cost of the hole is
  low and the alternative (rejecting symlinks) would break legitimate ones.
- **A title quoting an ID it does not belong to gates the wrong task.** The
  gate reads the title; it cannot know which task the change actually is.
- **The 200-byte minimum counts newlines**, so one 199-character line passes.
  The boundary is `< 200` fails, `== 200` passes.
- **Zero-padded IDs gate literally**: a title saying `OPS-2` demands
  `odd/reviews/OPS-2.md`, not `OPS-02.md`. It fails in the safe direction.
- **Nothing here judges quality.** A complete, honest, well-argued report and a
  complete, dishonest one look identical to the script.
