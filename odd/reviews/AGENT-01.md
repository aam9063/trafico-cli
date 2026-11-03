# Review report: AGENT-01

- **Reviewer:** `gentle-ai-worker` implementation pass over a parent-authored spec, verified by execution
- **Date:** 2026-11-02
- **Scope reviewed:** the `skills` and `schema` commands, the embedded recipes and the embedded schemas, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| risk | `internal/cli/schema.go` | **Reading the schemas from disk would have shipped a command that works only for us.** `os.ReadFile("schemas/...")` succeeds in the repository and fails for every user of the released binary, which does not carry the source tree. | **Applied.** `schemas/embed.go` embeds the directory with `go:embed`; `schema.go` reads only `schemas.FS`. It is not a stylistic choice — it is the difference between a command and a broken command, and the failure would have surfaced only after a download. |
| risk | `internal/cli/skills_test.go` | **A recipe that names a flag we later rename is worse than no recipe.** An agent follows it, gets exit 2, and has no way to know the error is ours. | **Applied.** `TestRecipesMatchCommandTree` builds the live cobra tree, routes each recipe's argv through `root.Find`, descends the subcommand chain and looks up every flag on the leaf `FlagSet`, consuming non-bool values so `<sáb>`-style placeholders are not mistaken for flags. Rename `--pk` and this fails before an agent does. |
| minor | `docs/ARRANQUE.md` §6.2 | It cited `trafico schema incidents`, but the published names are `incident` and `incidents-response`. Stale since before this change. | **Applied.** Corrected here, outside the worker's allowed surfaces, together with the note that the names come from `trafico schema`. |

Verified by execution, no findings:

- **S3 is proven, not asserted**: `recipes_run_test.go` runs each of the four §6.4 commands through the CLI harness against fixtures and requires exit 0 with a non-empty result. An unknown recipe or schema name is exit 2 and names the alternatives — never an empty list with exit 0.
- **The embedded document is byte-for-byte the published file**, pinned by test; `--json` compacts it into a single `json.RawMessage` so stdout stays one JSON value.
- **FORMAT-01 is respected**: both commands register the flags and render text by default.
- **`check.sh --strict` is green**; no existing test changed and no existing command was touched beyond two `AddCommand` lines.

## Veredicto

**Approve.** Both commands serve data the binary already had, and both failure modes were closed before they could reach a user: the disk read that breaks only outside the repo, and the recipe that rots silently when a flag moves.

The second is the one worth keeping: it makes the documentation testable. A guide that cannot be checked against the program it describes is a guide that is wrong within a month.

### Not verified

- The recipes against a live feed: the tests use fixtures, and no test touches the network.
- The complete schema set: `schemas/` publishes two of the contracts while the rest live as structs in `internal/model`. `trafico schema` serves what is published, which is the honest scope.
- `docs/cli-spec.md` (AGENT-02) and the install path (REL-01) remain open in the wave.
