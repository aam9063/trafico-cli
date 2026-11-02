# Review report: FEED-03

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-11-02
- **Scope reviewed:** the VMS parser (`internal/datex2/signs.go`), the wire model (`internal/model/sign.go`), the joined use case (`internal/app/signs.go`) and the `signs` command, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5). Keep the two headings below exactly as written: the CI gate requires them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| major | `internal/datex2/signs.go` (`ParseVmsDevices`, `ParseVmsMessages`) | **A foreign dialect parsed as a silent empty result.** Fed the v3.7 situations payload, a v1.0 radar body or a v3 tefiva body, both parsers returned zero results and **no error**, so `trafico signs` exited 0 with `signs: []` instead of `feed_changed` (exit 7). An agent reading that concludes there are no panels when the feed actually changed. The incident parser already guards against this by requiring `com:publicationTime`. | **Applied.** Both parsers validate the payload identity before returning: the root `xsi:type` must be the expected one **and** the body must carry `com:publicationTime`. The `xsi:type` is what distinguishes the sign feeds from the incident payload, which shares the `levelC` namespace family with them; the timestamp alone rejects the `datex2.eu` dialects. Three regression tests, all RED before the fix. |
| minor | `internal/model/sign.go` (`isOffMessage`) | A message carrying a real pictogram URL but no `customPictogramCode` was classified as an off panel and hidden, because the code was dropped before the rule saw it. | **Applied.** A pictogram URL is content; only the blank glyph (code `"0"`, or the recorded blank URL when the code is absent) marks a panel as off. |
| nit | `internal/app/signs.go` | Duplicate `vmsControllerReference` ids kept only the last controller's messages, silently. | **Applied.** `BuildSigns` reports duplicates and the command writes them to stderr through the same channel and helper as the orphan warning; last-wins stays, pinned by a test. |

Verified by execution, no findings:

- **The join** works on the real fixture: 5 devices, 3 controllers, 3 matched, 2 devices with `messages: []`. Controllers without a device — the real-world case of two feeds captured a minute apart — are reported on stderr, never dropped.
- **Namespaces by URI**: no prefix matching anywhere; a v1.0 or v3 body cannot parse as garbage.
- **Off-panel `[]`** is the honest encoding and is documented where a caller can find it.
- **P-04 fidelity**: `direction_label` from `roadDestination`, `province` kept uppercase and matched case-insensitively by `--province lugo`, absent fields `null`.
- **`meta.published_at`** comes from the messages feed, the volatile half of the pair; a missing value stays `null`.
- **The direction filter** mirrors `internal/filter` exactly: a `both` sign matches a one-way filter, a null direction matches only when no filter is set. This was probe-only before; it now has a test.
- **Tests**: the golden would fail if the join were dropped (it asserts a specific controller carries its message), and orphans are pinned at both app and unit level.

## Veredicto

**Approve after one correction round.** The major was the dangerous kind: a failure indistinguishable from a legitimate empty answer, in a tool whose exit codes exist precisely to separate those two. It is fixed with three tests that fail against the previous code. The two smaller findings came with it because each was a silent edge in the same join.

The golden file did **not** need regeneration: the real fixtures carry the expected `xsi:type` and `publicationTime`, every fixture message carries a pictogram code, and no fixture has a duplicate controller id. That is itself evidence the fixes changed failure handling without changing what a healthy feed produces.

### Not verified

- `make check` as a whole and benchmarks: the reviewer ran the four affected packages.
- Orphan/duplicate summaries beyond ten ids: exercised by reading, not by a probe.
- Live network behaviour, forbidden by rule.

### Documentation defect found and fixed

`docs/feeds.md` §1 placed the VMS locations feed in the **v3** standard dialect; the fixture uses `levelC/schema/3`, the same family as the incident feed. The parser would have been written against the wrong namespaces had the review not caught it. The table is corrected: `levelC` covers incidents and both panel feeds; `datex2.eu/schema/3` covers tefiva and the motorcycle sections.
