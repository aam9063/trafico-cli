# Review report: RLS-02

- **Reviewer:** parent orchestration pass, answering the two advisory findings of the native review of RLS-01
- **Date:** 2026-11-02
- **Scope reviewed:** the pinning of every action in the release workflow to a full commit SHA

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| risk | `.github/workflows/release.yml:20,26,33` | **Native review RLS-01 left two warnings (`R1-001`, `R1-002`) and they were the same defect twice.** The actions were referenced by tag — `actions/checkout@v4`, `actions/setup-go@v5`, `goreleaser/goreleaser-action@v6` — so a rewritten tag upstream would run someone else's code inside a job that holds `contents: write` and publishes our release artefacts. | **Applied.** All three are pinned to full commit SHAs, with the tag kept as a trailing comment so a human can still read the file. This is the whole set: `grep uses:` returns exactly those three. |

Verified by execution, no findings:

- **The SHAs resolve to the intended tags**: each was read from the GitHub API (`repos/<owner>/<repo>/commits/<tag>`), not copied from a blog post.
- **The YAML still parses**, and the workflow keeps its shape: the suite still runs before GoReleaser, and the licence files still travel in the archives.
- **`check.sh --strict` is green.**

## Veredicto

**Approve.** Two advisory findings from the native review, one root cause, closed before the first tag rather than after. Publishing a `v0.1.0` from a pipeline with an open supply-chain door would have been the wrong order.

Worth stating for the record: the native review found this, not the tests, and not me. A workflow file has no runtime behaviour to test, so the only thing that catches a tag-pinned action is someone looking.

### Not verified

- The pinned SHAs against a live run: the workflow has never executed. The first tag remains the integration test for the whole file.
- Whether the upstream projects will keep those commits reachable: they are permanent, but dependabot-style bumps will become manual work now.
