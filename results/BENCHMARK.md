# Benchmark: kubernetes-mcp-server v0.1.2 against the research build

Series `isokron-k8s-mcp-2026-11-bench2`, 12 November 2026 (01:27–04:45 UTC). Before/after only: the baseline (v0.1.2,
unmodified) against the research build (the 16 bug fixes plus the 19 usability changes); **no fixed-only arm was run**,
so the bug fixes' own effect is not measured. Data: [`2026-11-12-bench2/`](2026-11-12-bench2/); the analysis as generated:
[`2026-11-12-bench2/ANALYSIS.md`](2026-11-12-bench2/ANALYSIS.md); the task set: [`../harness/`](../harness/README.md).

## Method

- **Builds:** baseline `931b5f5a…e889`, research `167a5a65…b82` (records in [`builds/`](builds/); see EXPERIMENT.md on
  the hashes).
- **Target:** a local kind cluster (kind v0.33.0, `kindest/node:v1.37.0`), single node, on macOS arm64 Docker Desktop;
  KubeVirt v1.9.0 CRDs, Tekton Pipelines v1.16.0, local Helm charts. The server ran over stdio as a dedicated
  ServiceAccount that can write only in the run's own namespace and cannot read Secrets (task-specific extra roles are
  in the seeds). Toolsets `core` and `config`, plus `helm` for task F3.
- **Agent:** one agent per run through the Claude Agent SDK 0.3.319 (Claude Code CLI 2.1.319), with the MCP server as
  its only tool source, the system prompt in [`harness/bench/system.md`](../harness/bench/system.md), no effort or
  thinking setting, at most 40 turns, a 1.00 USD cap per run. Models: `claude-opus-5-5` on sets A and B,
  `claude-sonnet-5-5` on set A.
- **Runs:** 5 per task and build (Opus A and B: 160; Sonnet A: 60), plus an observer-effect arm (Opus, set A, wire
  capture off, 2 per task and build: 24). 244 measured runs, strictly sequential, in a seeded order
  (`random.Random(20261112)`, [`harness/bench/order.json`](../harness/bench/order.json)), each in a fresh namespace
  seeded the same way. 300 runs were planned; the 56 spares were not needed.
- **Correctness:** a mechanical checker reads the cluster as the admin identity after the agent stops (and for up to
  120 s for controllers), never the agent's answer. Every mutating API request in the run's namespace was matched to the
  run by identity through the API server's audit log.
- **Measures:** tokens = input + output + cache creation + cache read, as the provider reports them; cost = the
  provider-reported USD. Expected tokens (cost) per verified success = all attempts' tokens (cost) ÷ verified
  successes. Completion intervals are Wilson 95%.
- **Freeze:** the harness (65 files in the freeze manifest, git commit `126753f`) was hashed and frozen at 01:27:38 UTC, before the first
  measured run, fingerprint `548f70273924f5838a344b350a8952a1a052d7dc7747a34e1fb0e236019e918b`, nonce
  `8aa7204901a88b6fe8c6201769f533f81761b3d7b66410c9979617dcad6e3783`, RFC 3161 timestamps from Sectigo and DigiCert,
  anchored at the freeze. The sha256 of the frozen harness files is in
  [`2026-11-12-bench2/FROZEN-FILES.sha256`](2026-11-12-bench2/FROZEN-FILES.sha256). An earlier freeze of the same
  series (00:57 UTC) was superseded after the 32-run dry run with no measured run (an audit-log rotation rule was
  added). The series' records are not yet closed into an Evidence Pack.
- **Spend:** 18.88 USD measured, plus 3.06 USD for the dry run (32 runs, not measured).
- **Pre-check:** a scripted pre-check with no model (32/32) before the freeze: every task's untouched state fails its
  checker and a correct end state passes.

**The two task sets are reported separately. No number here spans both sets.**

## Set A: pre-registered tasks (K1–K6; the fair test)

Six ordinary jobs, with their prompts committed to git on 11 November 2026 at 00:06 UTC, before the first change list
(01:48 UTC). Integrity note: they were pre-registered by git commit only, not by an evidence freeze before the change
list; they were frozen on the chain only with this series. K3's manifest was written to the draft's description of its
shape.

Opus 5.5, baseline → research:

| Measure | baseline | research | change |
|---|---|---|---|
| Verified / attempts | 30/30 | 30/30 | |
| Completion (Wilson 95%) | 1.00 [0.89–1.00] | 1.00 [0.89–1.00] | +0.0% |
| Expected tokens per verified success | 66,796 | 64,574 | −3.3% |
| Expected cost per verified success (USD) | 0.0975 | 0.0614 | −37.0% |
| Tokens per attempt p50 / p95 | 58,441 / 132,094 | 55,907 / 102,947 | −4.3% / −22.1% |
| Cost per attempt p50 / p95 (USD) | 0.0794 / 0.2339 | 0.0474 / 0.1121 | −40.3% / −52.1% |
| Tool calls per attempt (mean) | 9.73 | 3.97 | −59.2% |
| Model turns per attempt (mean) | 4.20 | 3.33 | −20.6% |
| Result bytes per attempt (mean) | 9,676 | 3,034 | −68.6% |
| Wall seconds per attempt (mean) | 23.8 | 19.5 | −17.9% |
| Failed tool calls (total) | 11 | 0 | −100.0% |
| False "done" claims | 0 | 0 | — |

Per task (Opus); per-run median [min–max] from `runs.csv`:

| Task | Verified | Expected tokens per success | Change | Tool calls, median [min–max] | Cost per run, median [min–max] (USD) |
|---|---|---|---|---|---|
| K1 scale and verify | 5/5 → 5/5 | 37,698 → 54,157 | +43.7% | 2 [2–3] → 2 [2–2] | 0.0340 [0.0330–0.0788] → 0.0352 [0.0351–0.0365] |
| K2 one ConfigMap key | 5/5 → 5/5 | 37,932 → 54,633 | +44.0% | 2 [2–2] → 2 [2–2] | 0.0395 [0.0388–0.0416] → 0.0415 [0.0407–0.0418] |
| K3 apply a manifest | 5/5 → 5/5 | 53,318 → 37,285 | −30.1% | 5 [4–5] → 1 [1–1] | 0.0606 [0.0549–0.0623] → 0.0385 [0.0377–0.0388] |
| K4 roll back a bad image | 5/5 → 5/5 | 71,580 → 87,388 | +22.1% | 5 [4–6] → 4 [4–5] | 0.0953 [0.0800–0.1026] → 0.0720 [0.0667–0.1386] |
| K5 label six of nine | 5/5 → 5/5 | 70,042 → 60,696 | −13.3% | 7 [7–8] → 7 [3–7] | 0.1106 [0.1095–0.1220] → 0.0637 [0.0530–0.0649] |
| K6 selective clean-up | 5/5 → 5/5 | 130,206 → 93,286 | −28.4% | 36 [34–42] → 8 [8–9] | 0.2224 [0.2021–0.2982] → 0.1093 [0.1027–0.1123] |

Set A totals per build: baseline 292 tool calls, 2.9250 USD; research 119 calls, 1.8424 USD.

**Sonnet 5.5, set A:** 30/30 → 30/30; expected tokens per success 53,651 → 56,030 (+4.4%); expected cost 0.0387 →
0.0274 USD (−29.3%); tokens p50 43,034 → 36,703 (−14.7%); cost p50 0.0330 → 0.0182 (−44.8%); tool calls 7.40 → 3.70
(−50.0%); turns 3.63 → 2.87 (−21.1%); result bytes 8,562 → 2,441 (−71.5%); wall 15.4 → 15.0 s (−2.6%); failed calls
0 → 0. On K6 the research build used 53.8% more tokens per success while its cost fell (0.0771 → 0.0631); not
investigated.

## Set B: friction-derived tasks (F1a–F9; favours the research build by construction)

F1a and F2–F9 are native usability jobs 01, 07, 08, 10, 13, 14, 15, 16 and 17, with their prompt text and done-checks
unchanged; F1b is new (K3's twin with `--- # comment` separators and `replicas: -1`). Two criteria were added before
the freeze and are labelled `bench_fix` in the results: F2 "the server created a Pod" and F5 "the server attempted a
write", both read from the run's API audit log.

Opus 5.5, as recorded:

| Measure | baseline | research | change |
|---|---|---|---|
| Verified / attempts | 25/50 | 35/50 | |
| Completion (Wilson 95%) | 0.50 [0.37–0.63] | 0.70 [0.56–0.81] | +40.0% |
| Expected tokens per verified success | 148,584 | 130,781 | −12.0% |
| Expected cost per verified success (USD) | 0.2284 | 0.1250 | −45.3% |
| Tokens per attempt p50 / p95 | 74,096 / 118,590 | 99,300 / 152,678 | +34.0% (p50) |
| Cost per attempt p50 / p95 (USD) | 0.0859 / 0.2493 | 0.0821 / 0.1698 | −4.5% (p50) |
| Tool calls per attempt (mean) | 13.16 | 5.30 | −59.7% |
| Model turns per attempt (mean) | 4.60 | 4.56 | −0.9% |
| Result bytes per attempt (mean) | 7,834 | 4,163 | −46.9% |
| Wall seconds per attempt (mean) | 28.6 | 29.2 | +1.8% |
| Failed tool calls (total) | 43 | 22 | −48.8% |
| False "done" claims | 2 | 2 | +0.0% |

Per task (Opus):

| Task | Native job | Verified | Expected tokens per success | Tool calls, median [min–max] | Cost per run, median [min–max] (USD) | Failed calls |
|---|---|---|---|---|---|---|
| F1a multi-document deploy | 01 | 5/5 → 5/5 | 64,689 → 101,279 (+56.6%) | 6 [6–8] → 5 [5–5] | 0.0727 [0.0726–0.0832] → 0.0837 [0.0768–0.0849] | 0 → 0 |
| F1b separator twin | new | 5/5 → 5/5 | 44,996 → 54,512 (+21.1%) | 5 [5–5] → 2 [2–2] | 0.0663 [0.0659–0.0664] → 0.0559 [0.0531–0.0678] | 5 → 5 |
| F2 debug pod | 07 | 0/5 → 0/5 (post-hoc 0/5 → 4/5) | — | 7 [6–8] → 7 [6–11] | 0.0782 [0.0614–0.0823] → 0.0879 [0.0751–0.1249] | 0 → 2 |
| F3 Helm into a namespace | 08 | 0/5 → 5/5 | — → 117,942 | 7 [6–9] → 7 [6–8] | 0.1280 [0.1098–0.1348] → 0.1603 [0.1583–0.1711] | 15 → 0 |
| F4 clean-up with a finalizer | 10 | 0/5 → 0/5 | — | 45 [41–52] → 9 [8–23] | 0.2528 [0.2348–0.3152] → 0.1063 [0.1003–0.2057] | 18 → 0 |
| F5 partial write | 13 | 5/5 → 5/5 | 72,792 → 107,652 (+47.9%) | 9 [8–9] → 4 [3–6] | 0.0869 [0.0836–0.0896] → 0.1022 [0.0866–0.1203] | 5 → 5 |
| F6 bulk, 31 objects | 14 | 5/5 → 5/5 | 88,823 → 57,375 (−35.4%) | 35 [35–35] → 4 [4–4] | 0.2279 [0.2255–0.2296] → 0.0559 [0.0558–0.1281] | 0 → 0 |
| F7 multi-read | 15 | 5/5 → 5/5 | 47,838 → 37,403 (−21.8%) | 9 [9–9] → 1 [1–1] | 0.0974 [0.0966–0.0980] → 0.0401 [0.0381–0.0418] | 0 → 0 |
| F8 collision | 16 | 0/5 → 0/5 | — | 1 [1–1] → 2 [2–2] | 0.0317 [0.0294–0.0767] → 0.0429 [0.0423–0.0433] | 0 → 5 |
| F9 concurrent edit | 17 | 0/5 → 5/5 | — → 125,929 | 6 [6–6] → 6 [5–8] | 0.0854 [0.0819–0.0886] → 0.0797 [0.0677–0.0962] | 0 → 5 |

Set B totals per build: baseline 658 tool calls, 5.7088 USD; research 265 calls, 4.3742 USD.

- F3: every baseline run failed honestly on K8S-20 (the chart objects went to the kubeconfig namespace, where the server
  identity gets a 403). F9: on the baseline the forced apply lost the concurrent change 5/5; the research agents used
  `resources_patch`. F4 fails on both builds (no agent removed the test finalizer; all reported it honestly). F8 fails
  on both (agents asked instead of merging; research agents tried `mode: create`, which was refused). F4 and F8 are
  questions about the task design, not server defects.
- False "done": baseline `ob-f2-2`, `ob-f8-3`; research `or-f2-1`, `or-f2-4` (both F2, which the corrected criterion
  passes).
- **F2 checker defect (ours).** The added criterion counted only a `create` of a Pod; the server creates Pods with a
  server-side apply PATCH (201), and the pre-check had used `kubectl run`. Every F2 run is recorded as failed, and the
  recorded verdicts are the headline. **Labelled post-hoc** re-evaluation from each run's own audit-log extract (other
  criteria as recorded): F2 0/5 → 4/5 (`or-f2-3` failed native criteria too). Set B post-hoc: 25/50 → 39/50, completion
  0.50 → 0.78 [0.65–0.87], expected tokens per success 148,584 → 117,368 (−21.0%), expected cost 0.2284 → 0.1122 USD
  (−50.9%), false "done" 2 → 0.

## Where the change comes from (Opus; an accounting model, not an ablation)

Each tool call gets one role from its transcript (recovery after a failed call, wait or poll, batch, multi-get, edit,
other); a type's share is its change per verified success as a percentage of the baseline value, so each column adds
up to the total. Method: [`2026-11-12-bench2/ANALYSIS.md`](2026-11-12-bench2/ANALYSIS.md) section 6.

| Type | Set A tokens | Set A cost | Set B tokens | Set B cost | Set B post-hoc tokens | Set B post-hoc cost | Sonnet A tokens | Sonnet A cost |
|---|---|---|---|---|---|---|---|---|
| Bug fixes (completion) | +0.0% | +0.0% | −14.3% | −14.3% | −12.8% | −12.8% | +0.0% | +0.0% |
| Smaller results | +1.2% | +1.7% | +1.0% | +1.7% | +0.9% | +1.5% | +2.0% | +1.8% |
| Batch and multi-get | −2.0% | −9.2% | +2.5% | −12.8% | +2.2% | −11.5% | +7.2% | −0.6% |
| Fewer edit round trips | −5.7% | −4.2% | −0.0% | −0.2% | −0.0% | −0.2% | +1.1% | +0.1% |
| Waits instead of polling | −1.3% | −5.5% | −3.1% | −5.1% | −2.8% | −4.5% | −3.5% | −5.3% |
| Results that say what happened | −4.5% | −4.7% | −20.5% | −22.7% | −28.6% | −30.6% | +0.0% | +0.0% |
| Larger catalog (+54% tool list) | +28.7% | +3.9% | +25.4% | +3.3% | +22.8% | +3.0% | +30.7% | +4.3% |
| Not attributed | −19.6% | −19.1% | −3.0% | +4.8% | −2.7% | +4.3% | −33.0% | −29.6% |
| **Total** | **−3.3%** | **−37.0%** | **−12.0%** | **−45.3%** | **−21.0%** | **−50.9%** | **+4.4%** | **−29.3%** |

On set A the bug-fix row is 0 (every task already passed on the baseline), so the whole saving there comes from the
usability changes. On set B the bug-fix row is F3 (K8S-20); F9's gain (the lost update prevented) and post-hoc F2's are
credited to "results that say what happened", that is, to usability changes, not to bug fixes. Prices fitted exactly
from the reported costs: Opus input 4.00, output 20.00, cache write 5.00, cache read 0.20 USD per million tokens;
Sonnet 2.00, 10.00, 2.50, 0.10. Why tokens are flat while cost falls: the larger tool list adds about 5.7 k tokens to
every turn's context (first-turn context 11.8 k → 17.5 k), re-read from the prompt cache at the cheapest rate.

## Other measures

- **Observer-effect control** (set A, Opus, wire capture off, 2 per task and build): 12/12 and 12/12 passed; mean cost
  per attempt with capture on vs off: baseline 0.0975 vs 0.0995, research 0.0614 vs 0.0706 USD; turns 4.20 vs 4.50 and
  3.33 vs 3.50; wall 23.8 vs 24.7 s and 19.5 vs 21.2 s. The research build is still 29% cheaper with capture off. No
  observer effect on correctness was visible; no significance test.
- **Compaction:** not run (the agent runner had no way to force a mid-task compaction).
- **Every attempt:** [`2026-11-12-bench2/RUNS.md`](2026-11-12-bench2/RUNS.md) and `runs.csv`; every failed attempt with
  its failed checks in [`TABLES.md`](2026-11-12-bench2/TABLES.md); every tool call with its role, error flag and result
  size in `calls.jsonl`.

## What is published, and what is not

Published: per-run metrics (`runs.csv`, `RUNS.md`), the generated tables and analysis (`TABLES.md`, `analysis.json`,
`ANALYSIS.md`), each run's sequence of tool calls (`calls.jsonl`: tool, role, error flag, result size; the call
arguments are removed), the sha256 of every frozen harness file, the task set, seeds and checkers (byte-identical to
the frozen files, see [`../harness/README.md`](../harness/README.md)), and the records of the measured builds. Not
published: raw agent transcripts, MCP wire captures, API audit logs, cluster snapshots, the evidence chains and their
timestamp tokens, the agent configurations and the driver (they carry local paths, cluster credentials by path and
identifiers of IsoKron's internal tooling). The summaries above are computed from them.

## Limitations

- Five runs per task and build (two in the observer arm): spread, not a distribution; Wilson intervals are wide and no
  significance test is claimed.
- Set A was pre-registered by git commit, not by an evidence freeze before the change list. Set B favours the research
  build by construction. F2's recorded verdicts carry our checker defect (post-hoc view labelled).
- The by-type split is an accounting model over the transcripts: per-turn output is estimated, result tokens are
  characters / 3.0, and a call belongs to one role; the unattributed remainder is shown, never spread.
- Provider prompt caching carries across runs (5-minute TTL); the run order was shuffled and seeded so both builds are
  equally exposed, but per-run cache-write cost depends on what ran just before.
- One client, agents running as the operator user on macOS (not in a Linux sandbox), one cluster (kind, Kubernetes
  1.37.0), toolsets `core` and `config` (plus `helm` for F3). No compaction arm, no fixed-only arm.
