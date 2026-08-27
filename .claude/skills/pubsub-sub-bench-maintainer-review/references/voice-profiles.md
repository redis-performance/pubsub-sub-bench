# Voice profiles — real pubsub-sub-bench people

Mined from actual GitHub history on `redis-performance/pubsub-sub-bench`
(`gh pr list --state all --limit 300`, `gh api .../pulls/<n>/reviews`, `/pulls/<n>/comments`, and
`/issues/<n>/comments`, plus `gh api .../issues/<n>` for PR/issue bodies), covering all 44 PRs and
6 issues on record as of this mining (2023–2026). Read this alongside `nitpick-taxonomy.md` before
writing anything.

**Be honest about what this repo's history actually is, up front:** review culture here is thinner
than redisbench-admin's, which is itself already thin. Every human review found across all 44 PRs is
a bare `APPROVED` with an **empty body** — zero substantive review comments, zero inline diff
comments, zero `CHANGES_REQUESTED`, anywhere in the mined history. What *is* real and substantive is
the technical writing in PR and issue **descriptions**, almost all of it fcostaoliveira's own.

## fcostaoliveira / filipecosta90 — Filipe Oliveira (same person, two GitHub accounts; by far the
dominant author and the only maintainer with any real reviewer/triage presence)

`filipecosta90` is used on PRs #1–#34 (with a gap; both accounts appear active in overlapping
periods), `fcostaoliveira` from PR #36 onward. Both are "Filipe Oliveira (Redis)" / "(Personal)" —
treat them as one person's voice, not two different maintainers.

**Voice in PR/issue descriptions**: unusually rigorous for a project this size — exact line-number
citations, worked numeric examples, small tables comparing expected vs. actual, and an honest
disclosure of what was deliberately *not* done. Real examples:

- Issue #43 (the `TotalSubscriptions` bug): cites the exact three lines involved
  (`subscriber.go:254`, `:373`, `:611`), works a concrete numeric example
  (`-clients 5000 -channel-minimum 1 -channel-maximum 100 -subscribers-per-channel 10` → actual
  5000 vs. reported 1000), and closes with a **numbered menu of three possible fixes**, each with a
  one-line trade-off, plus *"Happy to send a PR for whichever you prefer."*
- PR #44 (the fix): includes a "Changes" section, a results table (case / warning / reported /
  actual), and an explicit, honest scope decision: *"I deliberately did **not** make
  `-subscribers-per-channel` control fan-out (option 1 in #43), since that changes behaviour for
  anyone currently passing it... Happy to do that instead if you'd prefer; this PR takes the option
  that only makes the reporting truthful."*
- PR #42 (TLS): a "Testing" section separating unit vs. integration coverage, and a checked test-plan
  list (`make test`, `make test-integration`, manual smoke test, `make checkfmt`) rather than a bare
  claim that it works.
- Issue #41 (the TLS gap report): lists all 32 existing CLI flags to make the absence of TLS support
  visually obvious, then a table of proposed flag names, then two explicit "details worth getting
  right" call-outs (cluster-path parity, RTT-measurement contamination — see taxonomy items 4–5).

**Voice as a closer/verifier**: one real example, closing issue #41 after PR #42 shipped —
detailed, specific verification rather than a bare "fixed": names the exact benchmark suites re-run
(all four `pubsub-mixed-*` variants, over both TLS and plaintext, confirming no regression), and
flags a real, non-obvious cross-tool gotcha for future readers (this tool's `-tls_insecure_skip_verify`
vs. memtier's `--tls-skip-verify` — different flag-naming conventions that can't share one arg string
in a combined harness). Closes plainly: *"Happy for this to be closed."*

**What this means for the bot's voice**: there is no evidence of fcostaoliveira (or anyone) writing a
*review comment* in this style — this rigor shows up in descriptions, before review happens. When
something in a PR genuinely warrants comment, the authentic register to imitate is this one: concrete,
line/flag-cited, a short table or worked example over abstract description, and — when there's a
real design choice rather than an obvious fix — a short numbered list of options with an offer to
defer, not a unilateral demand.

## paulorsousa, elena-kolevska, ofekshenawa — approvers with no evidenced review voice

All three show up only as `APPROVED` reviews with an empty body:

- **paulorsousa**: approved PRs #32, #36, #37, #38, #39 (all fcostaoliveira's own CI/Docker/docs
  work from 2026) — every one with an empty review body.
- **elena-kolevska**: approved PRs #28 and #29 (htemelski's node-redis benchmark work) — PR #29 shows
  two separate empty-body `APPROVED` reviews from her (likely re-approving after a push), still no
  text either time.
- **ofekshenawa**: one empty-body `APPROVED` on PR #13 (SSUBSCRIBE support).

Be honest that this skill has **no** real voice profile for any of the three beyond "approves,
writes nothing" — do not extrapolate a personality or a set of concerns for them the way a project
with real review comments would support. If you want to gesture at "a second look from a reviewer,"
don't attribute specific concerns to any of these three by name.

## htemelski / htemelski-oss — Hristo Temelski (the one substantive external contributor)

Author of the node-redis and ioredis JS benchmark ports (PRs #28, #29, #30) under `js/`. PR #30's own
title is candid about a real gap in the prior version: "fix ioredis benchmark, **utilize cluster
client fully**." No review comments exist on any of his PRs beyond elena-kolevska's empty approvals;
there's no back-and-forth to characterize. Worth noting only as evidence that the cluster-path-parity
concern (taxonomy item 4) recurs even in the JS ports, not just the Go core.

## Automated tooling — real, but with real limits, and don't mistake it for maintainer voice

- **Docker Build Validation** (`docker-build-pr.yml`) posts a fixed-template comment
  ("🐳 Docker Build Validation... ✅ Docker build successful!") on pushes to a PR, listing the git
  SHA, platforms tested, and a help/version smoke test. It reposts on every push — 7 near-identical
  copies were observed on a single PR (#34) and 6 on another (#42). This is real, load-bearing CI
  signal (a genuine build+smoke-test result), but it is a bot's fixed template, not a human
  reviewer's judgment — don't cite it as if a person said it, and don't re-derive "does this build"
  in your own review since it already covers that.
- **CodeQL** (`codeql-analysis.yml`) runs on push/PR to `master`/`main` plus a weekly schedule, using
  GitHub's generic, unmodified auto-generated template for Go. No alert or discussion of one turned
  up anywhere in the mined PR/issue history. Note that it exists and runs; do not claim it has caught
  anything in this repo, unlike redisbench-admin where it demonstrably has.
- **No Codecov bot** posts on PRs here — there is no automatic coverage-percentage comment to cite,
  unlike redisbench-admin.
