# Cross-cutting nitpick taxonomy — pubsub-sub-bench, real precedent only

Grounded in actual GitHub PR/issue history, actual PR descriptions, and this repo's own
`AGENTS.md`/`CONTRIBUTING.md` on `redis-performance/pubsub-sub-bench` (all 44 PRs and all 6 issues
surveyed, 2023–2026). This project's review history is thinner than redisbench-admin's: several
categories below are evidenced by a self-authored, well-documented bug report/fix rather than a
reviewer's comment, because no reviewer here has ever left a substantive comment in the mined
history. That is being honest about the actual size of the record, not a weakness to paper over.

1. **A reported/derived metric must reflect what actually happened, not just its inputs.** The
   sharpest, best-documented real bug in this project's history: issue #43 / PR #44.
   `TotalSubscriptions` in the JSON output was `total_channels * subscribers_per_channel` — but
   `-subscribers-per-channel` does not drive subscription creation at all; actual fan-out is
   `-clients` × channels-per-connection (from `-min/-max-number-channels-per-subscriber`, both
   defaulting to 1). The two values share no terms, so the reported number could be off by any
   factor in either direction, and the flag's own help text ("number of subscribers per channel")
   actively reinforces the wrong belief. The fix: count subscriptions as they're actually created,
   not compute a count from inputs that might not correspond to what happened. Check any PR that
   adds or touches a reported count/metric for exactly this: is it counted from an actual code path
   that ran, or computed from flags that could silently drift out of sync with reality?

2. **No silent no-op on an unhandled enum/mode/placement value.** Real, self-authored fix in the
   same PR #44: the `-subscribers-placement-per-channel` dense/sparse branch had no `else`, so an
   unrecognized value silently created **zero** subscribers and produced a clean-looking
   zero-message result — which reads as "the server delivered nothing" rather than "we subscribed to
   nothing." The fix makes this fail loudly (exit 1 with a reason) instead. Check that any new
   mode/placement/enum-like flag either exhaustively handles every value or errors clearly on an
   unrecognized one; a "quiet zero" is this project's own evidenced worst case, not a generic Go
   nitpick.

3. **A newly declared flag or constant must actually be wired to a call site.** Issue #41: four TLS
   constants (`tls_ca`, `tls_cert`, `tls_key`, `tls_insecure_skip_verify`) existed in `subscriber.go`
   with exactly one reference each — their own declaration — suggesting a capability (TLS support)
   that did not exist. Nothing else in the codebase read them, and the tool `log.Fatal`ed against any
   TLS-enabled endpoint. When reviewing a PR that adds a flag, constant, or config field, trace it to
   where it's actually read/used; a name and a default value are not evidence the feature works.

4. **Standalone/cluster parity: a config or connection change must reach both option structs.**
   Issue #41's own suggested-fix section says this explicitly: "Apply the config to both
   `redis.Options` and `redis.ClusterOptions`, or the cluster mode silently stays plaintext." PR #42
   (the TLS fix) did apply the new TLS config to both. This project's broader history shows the
   cluster path is a real, recurring place where things need separate, deliberate handling: PR #30's
   own title is "fix ioredis benchmark, **utilize cluster client fully**" (a prior version didn't);
   PR #16 is "Ensure mutual exclusion on cluster nodes/slots update when starting benchmark"; PR #17
   and PR #15 are both cluster-slot/connection-routing fixes; issue #18 (still open) is about the OSS
   cluster API setup stage always running. Any PR touching connection setup, auth, timeouts, or TLS
   should be checked for whether it was applied to the cluster path too, not just standalone.

5. **RTT/latency measurement must not silently absorb cost it isn't meant to measure.** Issue #41's
   suggested-fix section, again explicitly: "With `-measure-rtt-latency`, make sure the TLS handshake
   isn't counted in the reported RTT — the connection setup cost is real but it isn't per-message
   latency, and folding it in would quietly inflate the numbers TLS is being evaluated on." This
   project has repeated, dedicated feature investment in RTT precision — PR #23 ("Added the option to
   measure RTT from publishers->subscribers"), PR #26 ("extended summary metrics (p95)"), PR #32
   ("Optimize RTT payload generation to include configurable data size"), PR #34 ("Track RTT in
   nanos"). Treat any change touching the RTT/latency code path with the same care: does it measure
   only what it claims to measure, at the precision it claims?

6. **Downstream benchmark-spec impact is real and has already bitten this project once.** PR #44's
   own description traces it out: the bug in item 1 above is live in
   `redis/redis-benchmarks-specification`'s `...-50K-subscribers-5k-conns.yml` suite, which passes
   exactly the argument combination that triggers the mismatch — so a suite literally named "50K
   subscribers" was generating the same 5000 subscriptions as its 5000-subscriber sibling, silently.
   The PR author filed a separate, independent fix in that other repo rather than trying to paper
   over it here. Worth a one-line check on any PR changing default behavior, a flag's meaning, or a
   reported count: could an existing named benchmark suite in redis-benchmarks-specification now mean
   something different than its name implies?

7. **Cross-tool (memtier_benchmark) flag-naming consistency matters for mixed pub/sub suites, but has
   no enforced convention.** Issue #41 proposed naming new TLS flags to line up with memtier's
   (`-tls-skip-verify` to match `--tls-skip-verify`) specifically because
   `redis-benchmarks-specification`'s `pubsub-mixed-*` suites pair a memtier publisher with a
   pubsub-sub-bench subscriber. What actually shipped in PR #42 used this project's existing
   underscore style (`-tls_insecure_skip_verify`) instead, which the closing verification comment on
   issue #41 flags as a real, documented gotcha: a harness driving both tools can't share one TLS
   argument string between them, since Go's `flag` package hard-fails on an unrecognized flag. Both
   choices are defensible; the point for review purposes is that this project has no settled
   convention here, and a PR introducing a new flag that has a memtier equivalent should at least
   note the naming choice rather than let it happen silently.

8. **Test infrastructure here is genuinely new — most of this project's history shipped with zero
   automated tests.** PR #42 (TLS support, 2026) says so in its own description: "Establishes the
   repo's first test infrastructure: `make test` previously ran against zero `*_test.go` files." That
   means RESP3 support, cluster mode, sharded pub/sub, reconnection logic, rate limiting, the RTT
   measurement feature line, and the node-redis/ioredis JS ports (PRs #1–#41) all merged without a
   single automated test, despite `CONTRIBUTING.md`'s written rule that "All new behaviour must be
   covered by tests." Be accurate: that rule is real and current, but it was not historically
   enforced — cite it as the present, going-forward bar (which PR #42 itself sets a new, real
   precedent for meeting, with 9 unit + 3 integration tests), not as something the whole history
   demonstrates being held to.

## What this taxonomy is honestly thin or silent on

- **No evidence of a substantive human review comment, ever.** All human reviews found in the mined
  history (`paulorsousa`, `elena-kolevska`, `ofekshenawa`) are bare `APPROVED` with an empty body.
  There is no equivalent here to redisbench-admin's kei-nan PR#541 review — no multi-point,
  line-traced, "here's the bug and here's the arithmetic" example exists in this project's real
  history. Don't manufacture one.
- **No CodeQL catch on record.** `codeql-analysis.yml` exists and runs (Go, GitHub's generic
  auto-generated template, on push/PR to master/main plus a weekly schedule), but the mined PR/issue
  history shows no discussion of a CodeQL alert being raised or fixed on this repo. Note that it runs;
  don't claim it has caught anything real here.
- **No Codecov integration, no automatic coverage percentage on PRs.** Unlike redisbench-admin, there
  is no coverage bot commenting on PRs in this repo's history — `CONTRIBUTING.md`'s coverage language
  ("Coverage should not decrease") is written doctrine with no CI-visible enforcement mechanism found.
- **No example of a maintainer requesting changes or rejecting a design.** Every merged PR in the
  survey was approved as submitted or self-merged; the two closed-without-merge PRs found (#39, a
  Docker registry migration; and older ones) show no recorded review disagreement in their comment
  history — they simply weren't merged, with no visible debate.
- **Buffer sizing / memory-safety nitpicks.** This is a Go codebase using `go-redis`; no equivalent
  to memtier_benchmark's C-string/`snprintf` category applies, and no such issue turned up in this
  repo's history.
