> Copied from the analysis generated on 12 November 2026 at 04:50 UTC by `harness/bench/analysis/analyze.py`. On 15 November
> 2026 the model names were replaced with neutral labels (main model, second model); nothing else changed.
> Section references to `BENCHMARK.md` (for example "4.6") are to IsoKron's internal benchmark record; the public
> write-up is [`../BENCHMARK.md`](../BENCHMARK.md). `scripts/analyze.py` is [`harness/bench/analysis/analyze.py`](../../harness/bench/analysis/analyze.py).

# Kubernetes MCP benchmark: baseline v0.1.2 vs research build

Series `isokron-k8s-mcp-2026-11-bench2` · freeze `548f70273924f5838a344b350a8952a1a052d7dc7747a34e1fb0e236019e918b` (anchor `01a1cd5e-1883-7410-a422-63d56ef7af59`, commit `126753f`) · generated 2026-11-12T04:50Z
Builds: baseline `931b5f5a4a3947f9a4ab329e4731434dd8a9722aee681526f84fafb85130e889`, research `167a5a650592d314ffbb24ddbe3e2b81c45fb49c13062acd53c02052d58ddb82` · toolkit mcp-audit `0c118956`
Models: a frontier model, the main model (sets A and B), and a second, lower-priced model (set A) · 244 measured runs, all completed (204 pass, 40 fail; none inconclusive or abandoned) · spend 18.88 USD measured + 3.06 USD dry run = 21.94 USD of 75 USD.

Correctness is the mechanical checker's verdict on the cluster, recorded on the evidence chain; nothing here is judged by a model. **Set A (pre-registered) and set B (friction-derived, favours the research build by construction) are reported separately and never added together.** Every number is baseline → research with the % change (research − baseline, over baseline). Tokens = input + output + cache creation + cache read, as `mcp-audit usability bench` counts them; cost = the provider-reported USD. Expected tokens (cost) per verified success = all attempts' tokens (cost) ÷ verified successes. Completion intervals are Wilson 95 %. The by-type method is in section 6; what went wrong is in `BENCHMARK.md` section 4.

## 1. Headline (main model)

| metric | set A baseline → research | change | set B baseline → research | change |
|---|---|---|---|---|
| completion (Wilson 95 %) | 1.00 [0.89-1.00] → 1.00 [0.89-1.00] | +0.0 % | 0.50 [0.37-0.63] → 0.70 [0.56-0.81] | +40.0 % |
| verified / attempts | 30/30 → 30/30 |  | 25/50 → 35/50 |  |
| expected tokens per verified success | 66,796 → 64,574 | -3.3 % | 148,584 → 130,781 | -12.0 % |
| expected cost per verified success (USD) | 0.0975 → 0.0614 | -37.0 % | 0.2284 → 0.1250 | -45.3 % |
| tokens per attempt p50 / p95 | 58,441 / 132,094 → 55,907 / 102,947 | -4.3 % | 74,096 / 118,590 → 99,300 / 152,678 | +34.0 % |
| cost per attempt p50 / p95 (USD) | 0.0794 / 0.2339 → 0.0474 / 0.1121 | -40.3 % | 0.0859 / 0.2493 → 0.0821 / 0.1698 | -4.5 % |
| tool calls per attempt (mean) | 9.73 → 3.97 | -59.2 % | 13.16 → 5.30 | -59.7 % |
| model turns per attempt (mean) | 4.20 → 3.33 | -20.6 % | 4.60 → 4.56 | -0.9 % |
| result bytes per attempt (mean) | 9,676 → 3,034 | -68.6 % | 7,834 → 4,163 | -46.9 % |
| wall seconds per attempt (mean) | 23.8 → 19.5 | -17.9 % | 28.6 → 29.2 | +1.8 % |
| failed tool calls (total) | 11 → 0 | -100.0 % | 43 → 22 | -48.8 % |
| false "done" claims (claimed done, checker failed) | 0 → 0 | - | 2 → 2 | +0.0 % |

For the p50 / p95 rows the change column is the p50's change. **Set B with F2 corrected
(post-hoc, labelled; `BENCHMARK.md` 4.6):** completion 0.50 → 0.78 [0.65-0.87], expected tokens per verified success 148,584 → 117,368 (-21.0 %), expected cost 0.2284 → 0.1122 USD (-50.9 %), false "done" claims 2 → 0.

What the headline says, in short:
- **Set A (the fair test): no completion difference (30/30 on both builds), cost per success −37 %, tokens −3 %.** The
  research build needs 59 % fewer tool calls and 21 % fewer model turns and returns 69 % fewer result bytes, and every
  failed call is gone (11 → 0). Tokens barely move because the research catalog adds about 5.7 k provider tokens to every
  turn (first-turn context 11.8 k → 17.5 k), re-read from the prompt cache; cache reads are the cheapest token class
  (main model: 0.20 USD per million against 5.00 for a cache write and 20.00 for output), so the cost falls while the token
  count does not.
- **Set B (favours research by construction): completion 50 % → 70 % (78 % with F2 corrected), cost per success −45 %
  (−51 %).** The gains are F3 (Helm into a namespace, 0/5 → 5/5, the K8S-20 fix) and F9 (concurrent edit, 0/5 → 5/5, no
  lost update), and post-hoc F2 (0/5 → 4/5). F4 and F8 fail on both builds.
- Per attempt (not per success), set B's research runs use more tokens at the median (+34 %): the bigger catalog again,
  and research agents finishing tasks that baseline agents gave up on.

## 2. By type of improvement (main model)

The change in expected tokens and cost per verified success, split by the type of change it comes from (section 6).
Positive = more for the research build (a cost); the catalog row is the +54 % tools/list on the other side. "Share" is
the type's change as a % of the baseline value, so the shares add up to the total % change.

### Set A, tokens

baseline 66,796 -> research 64,574: -2,222 (-3.3 %); completion part 0, token part -2,222

| type | change per verified success | share of the baseline value |
|---|---|---|
| Bug fixes (completion) | 0 | +0.0 % |
| Smaller results | 783 | +1.2 % |
| Batch / multi-get | -1,361 | -2.0 % |
| Fewer edit round-trips | -3,820 | -5.7 % |
| Waits (no polling) | -902 | -1.3 % |
| Truthful results | -2,999 | -4.5 % |
| Catalog cost (+54 % tools/list) | 19,148 | +28.7 % |
| Unattributed | -13,072 | -19.6 % |
| **total** | -2,222 | -3.3 % |

### Set A, cost (USD)

baseline 0.0975 -> research 0.0614: -0.0361 (-37.0 %); completion part 0.0000, token part -0.0361

| type | change per verified success | share of the baseline value |
|---|---|---|
| Bug fixes (completion) | 0.0000 | +0.0 % |
| Smaller results | 0.0017 | +1.7 % |
| Batch / multi-get | -0.0089 | -9.2 % |
| Fewer edit round-trips | -0.0041 | -4.2 % |
| Waits (no polling) | -0.0054 | -5.5 % |
| Truthful results | -0.0046 | -4.7 % |
| Catalog cost (+54 % tools/list) | 0.0038 | +3.9 % |
| Unattributed | -0.0186 | -19.1 % |
| **total** | -0.0361 | -37.0 % |

### Set B, tokens

baseline 148,584 -> research 130,781: -17,803 (-12.0 %); completion part -42,453, token part 24,650

| type | change per verified success | share of the baseline value |
|---|---|---|
| Bug fixes (completion) | -21,226 | -14.3 % |
| Smaller results | 1,416 | +1.0 % |
| Batch / multi-get | 3,688 | +2.5 % |
| Fewer edit round-trips | -11 | -0.0 % |
| Waits (no polling) | -4,636 | -3.1 % |
| Truthful results | -30,423 | -20.5 % |
| Catalog cost (+54 % tools/list) | 37,787 | +25.4 % |
| Unattributed | -4,396 | -3.0 % |
| **total** | -17,803 | -12.0 % |

### Set B, cost (USD)

baseline 0.2284 -> research 0.1250: -0.1034 (-45.3 %); completion part -0.0652, token part -0.0381

| type | change per verified success | share of the baseline value |
|---|---|---|
| Bug fixes (completion) | -0.0326 | -14.3 % |
| Smaller results | 0.0039 | +1.7 % |
| Batch / multi-get | -0.0293 | -12.8 % |
| Fewer edit round-trips | -0.0006 | -0.2 % |
| Waits (no polling) | -0.0115 | -5.1 % |
| Truthful results | -0.0518 | -22.7 % |
| Catalog cost (+54 % tools/list) | 0.0076 | +3.3 % |
| Unattributed | 0.0110 | +4.8 % |
| **total** | -0.1034 | -45.3 % |

### Set B with F2 corrected (post-hoc), tokens

baseline 148,584 -> research 117,368: -31,216 (-21.0 %); completion part -53,338, token part 22,122

| type | change per verified success | share of the baseline value |
|---|---|---|
| Bug fixes (completion) | -19,049 | -12.8 % |
| Smaller results | 1,270 | +0.9 % |
| Batch / multi-get | 3,310 | +2.2 % |
| Fewer edit round-trips | -10 | -0.0 % |
| Waits (no polling) | -4,160 | -2.8 % |
| Truthful results | -42,542 | -28.6 % |
| Catalog cost (+54 % tools/list) | 33,911 | +22.8 % |
| Unattributed | -3,945 | -2.7 % |
| **total** | -31,216 | -21.0 % |

### Set B with F2 corrected (post-hoc), cost (USD)

baseline 0.2284 -> research 0.1122: -0.1162 (-50.9 %); completion part -0.0820, token part -0.0342

| type | change per verified success | share of the baseline value |
|---|---|---|
| Bug fixes (completion) | -0.0293 | -12.8 % |
| Smaller results | 0.0035 | +1.5 % |
| Batch / multi-get | -0.0263 | -11.5 % |
| Fewer edit round-trips | -0.0005 | -0.2 % |
| Waits (no polling) | -0.0103 | -4.5 % |
| Truthful results | -0.0699 | -30.6 % |
| Catalog cost (+54 % tools/list) | 0.0068 | +3.0 % |
| Unattributed | 0.0098 | +4.3 % |
| **total** | -0.1162 | -50.9 % |

Reading the split:
- **Bug fixes** and **truthful results** carry set B: the completion gained on F3 (K8S-20) is credited to bug fixes,
  F9's (lost update prevented by patch without force) and post-hoc F2's (delete reports gone) to truthful results; the
  truthful-results row also holds the recovery calls after failed calls that the research build no longer needs.
- **Batch / multi-get** is the biggest per-call saving in cost on both sets (K6, F6, F7: one selector or multi-kind call
  instead of 30-45 per-item calls), but in tokens it is near zero or positive: the per-item calls were issued in
  parallel within a few turns, so removing them saves output and cache writes (expensive) more than turns (cache reads).
- **Smaller results** comes out slightly positive: for the same tool, research results are on average a little larger
  per call (per-item result lists of the batch-capable tools, the scale receipt), while the big echo reduction shows up as
  fewer calls, which this method books under batch and edit.
- **Catalog**: +28.7 % (set A) and +25.4 % (set B) of the baseline tokens per success, but only +3.9 % / +3.3 % of the
  cost, since it is read from the cache.
- **Unattributed** is large in set A tokens (−19.6 %) and set A cost (−19.1 %): mostly the model's own output and the
  cache writes of the final answer turns and of turns without tool calls (research runs end in fewer, shorter turns), plus
  the cross-run prompt-cache state. The method does not spread it over the types.

### From the task tags (section 5a, source 2)

Each task carries the change types it exercises; mean of the per-task % changes over the tasks of each type (tasks with
no success on one build left out and counted as n/a).


#### Main model, set A

| type | tasks | mean % change, tokens / success | mean % change, cost / success | n/a | successes baseline -> research |
|---|---|---|---|---|---|
| smaller_results | K1, K2, K3, K5 | +11.1 % | -24.2 % | 0 | 20 -> 20 |
| batch | K5, K6 | -20.8 % | -49.9 % | 0 | 10 -> 10 |
| multi_get | K6 | -28.4 % | -54.5 % | 0 | 5 -> 5 |
| edit_round_trips | K2, K4 | +33.1 % | -2.1 % | 0 | 10 -> 10 |
| waits | K1, K3, K4, K6 | +1.8 % | -29.5 % | 0 | 20 -> 20 |
| truthful_results | K2, K6 | +7.8 % | -25.3 % | 0 | 10 -> 10 |

#### Main model, set B

| type | tasks | mean % change, tokens / success | mean % change, cost / success | n/a | successes baseline -> research |
|---|---|---|---|---|---|
| bug_fixes | F1b, F2, F3 | +21.1 % | -9.6 % | 2 | 5 -> 10 |
| smaller_results | F1a, F7 | +17.4 % | -25.8 % | 0 | 10 -> 10 |
| batch | F4, F6 | -35.4 % | -69.1 % | 1 | 5 -> 5 |
| multi_get | F4, F7 | -21.8 % | -59.2 % | 1 | 5 -> 5 |
| edit_round_trips | F9 | - | - | 1 | 0 -> 5 |
| waits | F1a, F2, F3 | +56.6 % | +7.5 % | 2 | 5 -> 10 |
| truthful_results | F1b, F2, F3, F4, F5, F8, F9 | +34.5 % | +4.7 % | 5 | 10 -> 20 |


## 3. Per task (main model)


#### Set A

| task | tags | baseline verified | research verified | baseline exp. tokens/success | research | change | baseline calls (mean) | research | baseline cost/success | research |
|---|---|---|---|---|---|---|---|---|---|---|
| K1 | waits, smaller_results | 5/5 | 5/5 | 37,698 | 54,157 | +43.7 % | 2.2 | 2 | 0.0439 | 0.0355 |
| K2 | edit_round_trips, smaller_results, truthful_results | 5/5 | 5/5 | 37,932 | 54,633 | +44.0 % | 2 | 2 | 0.0398 | 0.0413 |
| K3 | smaller_results, waits | 5/5 | 5/5 | 53,318 | 37,285 | -30.1 % | 4.8 | 1 | 0.0601 | 0.0383 |
| K4 | edit_round_trips, waits | 5/5 | 5/5 | 71,580 | 87,388 | +22.1 % | 5 | 4.4 | 0.0910 | 0.0836 |
| K5 | batch, smaller_results | 5/5 | 5/5 | 70,042 | 60,696 | -13.3 % | 7.2 | 6.2 | 0.1129 | 0.0618 |
| K6 | batch, multi_get, truthful_results, waits | 5/5 | 5/5 | 130,206 | 93,286 | -28.4 % | 37.2 | 8.2 | 0.2373 | 0.1079 |

#### Set B

| task | tags | baseline verified | research verified | baseline exp. tokens/success | research | change | baseline calls (mean) | research | baseline cost/success | research |
|---|---|---|---|---|---|---|---|---|---|---|
| F1a | smaller_results, waits | 5/5 | 5/5 | 64,689 | 101,279 | +56.6 % | 6.8 | 5 | 0.0768 | 0.0826 |
| F1b | bug_fixes, truthful_results | 5/5 | 5/5 | 44,996 | 54,512 | +21.1 % | 5 | 2 | 0.0662 | 0.0599 |
| F2 | truthful_results, waits, bug_fixes | 0/5 | 0/5 | - | - | - | 7.2 | 7.6 | - | - |
| F3 | bug_fixes, truthful_results, waits | 0/5 | 5/5 | - | 117,942 | - | 7.6 | 6.8 | - | 0.1633 |
| F4 | batch, multi_get, truthful_results | 0/5 | 0/5 | - | - | - | 45.4 | 14.2 | - | - |
| F5 | truthful_results | 5/5 | 5/5 | 72,792 | 107,652 | +47.9 % | 8.6 | 4.2 | 0.0866 | 0.1031 |
| F6 | batch | 5/5 | 5/5 | 88,823 | 57,375 | -35.4 % | 35 | 4 | 0.2277 | 0.0703 |
| F7 | multi_get, smaller_results | 5/5 | 5/5 | 47,838 | 37,403 | -21.8 % | 9 | 1 | 0.0972 | 0.0396 |
| F8 | truthful_results | 0/5 | 0/5 | - | - | - | 1 | 2 | - | - |
| F9 | edit_round_trips, truthful_results | 0/5 | 5/5 | - | 125,929 | - | 6 | 6.2 | - | 0.0796 |


F2 with the corrected criterion: baseline 0/5, research 4/5 (or-f2-3 failed the native criteria too). All failed attempts
with the checks that failed: `analysis-output/TABLES.md` ("Every failed attempt").

## 4. Second model, set A


| metric | baseline → research | change |
|---|---|---|
| completion (Wilson 95 %) | 1.00 [0.89-1.00] → 1.00 [0.89-1.00] | +0.0 % |
| verified / attempts | 30/30 → 30/30 |  |
| expected tokens per verified success | 53,651 → 56,030 | +4.4 % |
| expected cost per verified success (USD) | 0.0387 → 0.0274 | -29.3 % |
| tokens per attempt p50 / p95 | 43,034 / 83,931 → 36,703 / 142,012 | -14.7 % |
| cost per attempt p50 / p95 (USD) | 0.0330 / 0.0767 → 0.0182 / 0.0651 | -44.8 % |
| tool calls per attempt (mean) | 7.40 → 3.70 | -50.0 % |
| model turns per attempt (mean) | 3.63 → 2.87 | -21.1 % |
| result bytes per attempt (mean) | 8,562 → 2,441 | -71.5 % |
| wall seconds per attempt (mean) | 15.4 → 15.0 | -2.6 % |
| failed tool calls (total) | 0 → 0 | - |
| false "done" claims (claimed done, checker failed) | 0 → 0 | - |

### Second model, set A, by type of improvement, tokens

baseline 53,651 -> research 56,030: 2,379 (+4.4 %); completion part 0, token part 2,379

| type | change per verified success | share of the baseline value |
|---|---|---|
| Bug fixes (completion) | 0 | +0.0 % |
| Smaller results | 1,085 | +2.0 % |
| Batch / multi-get | 3,859 | +7.2 % |
| Fewer edit round-trips | 565 | +1.1 % |
| Waits (no polling) | -1,892 | -3.5 % |
| Truthful results | 0 | +0.0 % |
| Catalog cost (+54 % tools/list) | 16,470 | +30.7 % |
| Unattributed | -17,707 | -33.0 % |
| **total** | 2,379 | +4.4 % |

### Second model, set A, by type of improvement, cost (USD)

baseline 0.0387 -> research 0.0274: -0.0114 (-29.3 %); completion part 0.0000, token part -0.0114

| type | change per verified success | share of the baseline value |
|---|---|---|
| Bug fixes (completion) | 0.0000 | +0.0 % |
| Smaller results | 0.0007 | +1.8 % |
| Batch / multi-get | -0.0002 | -0.6 % |
| Fewer edit round-trips | 0.0000 | +0.1 % |
| Waits (no polling) | -0.0020 | -5.3 % |
| Truthful results | 0.0000 | +0.0 % |
| Catalog cost (+54 % tools/list) | 0.0016 | +4.3 % |
| Unattributed | -0.0115 | -29.6 % |
| **total** | -0.0114 | -29.3 % |

#### Per task

| task | tags | baseline verified | research verified | baseline exp. tokens/success | research | change | baseline calls (mean) | research | baseline cost/success | research |
|---|---|---|---|---|---|---|---|---|---|---|
| K1 | waits, smaller_results | 5/5 | 5/5 | 45,697 | 35,551 | -22.2 % | 2.8 | 1 | 0.0240 | 0.0203 |
| K2 | edit_round_trips, smaller_results, truthful_results | 5/5 | 5/5 | 37,270 | 35,774 | -4.0 % | 2 | 1 | 0.0171 | 0.0147 |
| K3 | smaller_results, waits | 5/5 | 5/5 | 42,261 | 37,085 | -12.2 % | 2.6 | 1 | 0.0262 | 0.0184 |
| K4 | edit_round_trips, waits | 5/5 | 5/5 | 42,999 | 35,670 | -17.0 % | 3 | 1.2 | 0.0339 | 0.0140 |
| K5 | batch, smaller_results | 5/5 | 5/5 | 66,121 | 57,403 | -13.2 % | 7 | 7 | 0.0543 | 0.0338 |
| K6 | batch, multi_get, truthful_results, waits | 5/5 | 5/5 | 87,559 | 134,701 | +53.8 % | 27 | 11 | 0.0771 | 0.0631 |

The second model completes every set A task on both builds too; the research build halves its calls and cuts cost per success
by 29 %. K6 on research costs more tokens per success (+54 %) while its cost still falls (−18 %); the per-run rows are in
`analysis-output/RUNS.md` (not investigated further here).

## 5. Observer-effect control (set A, main model, capture off)


| build | arm | attempts | verified | completion (Wilson 95 %) | cost per attempt (mean) | model turns (mean) | wall s (mean) |
|---|---|---|---|---|---|---|---|
| baseline | capture on (main model) | 30 | 30 | 1.00 [0.89-1.00] | 0.0975 | 4.20 | 23.8 |
| baseline | capture off (observer) | 12 | 12 | 1.00 [0.76-1.00] | 0.0995 | 4.50 | 24.7 |
| research | capture on (main model) | 30 | 30 | 1.00 [0.89-1.00] | 0.0614 | 3.33 | 19.5 |
| research | capture off (observer) | 12 | 12 | 1.00 [0.76-1.00] | 0.0706 | 3.50 | 21.2 |


The same tasks with the MCP server started directly instead of behind the wire-proxy capture (2 runs per task per
build): every run passes on both builds, as with the capture on. Mean cost per attempt, capture on vs off: baseline
0.0975 vs 0.0995 USD, research 0.0614 vs 0.0706 USD; model turns 4.20 vs 4.50 and 3.33 vs 3.50. The research build is
cheaper with the capture off as well (0.0995 → 0.0706 USD, −29 %). No observer effect on correctness is visible at this
size; no significance test is claimed for the cost differences. Only verdict, cost, turns and wall time
are compared (there is no capture, so no call-level metrics).

## 6. Method of the by-type split

```
Per-call roles (one per tool call, from the transcript's tool_use input and tool_result; first match wins):
  recovery   any call after a failed call (isError), until the next successful write              -> truthful results
  wait       resources_rollout action=status; a read that repeats an earlier identical read after a write (polling)
             (a write with a `wait` argument keeps its write role: what it saves is the polls)    -> waits
  batch      resources_label, resources_delete/scale with labelSelector (one call for many objects); or any write
             of a (tool, kind) that the run applies to two or more different objects (what a batch replaces) -> batch/multi-get
  multi_get  resources_get with `names`, resources_list with `kinds`/`all_kinds`; or any read whose tool the run
             uses on two or more different targets (what a multi-get replaces)                    -> batch/multi-get
  edit       resources_patch, resources_rollout undo/restart; or a get immediately followed by a full-object
             create_or_update (both calls of the read-modify-write pair)                          -> fewer edit round-trips
  other      everything else (discovery, single reads and writes)                                 -> unattributed
Per task (same task, research vs baseline, per-attempt means). Accounting identity: a run's tokens are the sum over its
model turns of (context + output); the context of a turn is the fixed base (system prompt, catalog, user prompt: the
first turn's context) plus everything said since. So T = turns x base + for every call (its tool_use input once as output
and again in every later turn, its result in every later turn) + the model's own text/thinking and its re-reads.
  catalog         = (median first-turn context, research - baseline) x research model turns   [a cost: positive tokens]
  smaller_results = for each tool present in both builds: (research mean result tokens per call - baseline mean) x
                    research calls of that tool x their mean re-reads (later model turns that carry the result)
  call roles      = (research - baseline) turn shares of the role x the baseline base context (a call's turn share =
                    1 / the number of calls its model turn issued), plus (research - baseline) calls of the role x the
                    baseline mean footprint of such a call (its turn's output share x (1 + re-reads) + result tokens x re-reads;
                    a turn's output, reasoning included, is shared among the calls it issued by argument size)
  unattributed    = the rest of the per-attempt change: the final answer turn and other turns without calls, calls of role `other`,
                    the cross-run prompt-cache state (cache writes vs reads), and anything the roles above do not capture
Completion: expected per success E = T / S. dE = [T_B/S_R - T_B/S_B] (completion, at baseline tokens) + [(T_R - T_B)/S_R]
(tokens, at research completion). The completion part goes to the task's primary type (bug fixes or truthful results,
by the cause of the baseline failures; PRIMARY below); the token part is split as above, divided by S_R.
Result tokens of a call ~ result characters / 3.0. A turn's output ~ the growth of the context to the next API call less
the results it received (the transcript carries each call's context but not its final output count).
```

Prices fitted from the reported costs (exact, residual < 1e-15 USD): main model input 4.00, output 20.00, cache write 5.00,
cache read 0.20 USD per million tokens; second model 2.00, 10.00, 2.50, 0.10. The cost split prices each part by the token
class it moves (re-reads at the cache-read price, new content once at the cache-write price, the issuing turn's output at
the output price); the remainder is unattributed.

## 7. Limitations

- Five runs per task per build (two for the observer arm): spread, not a distribution. Wilson intervals are wide (set A
  completion 1.00 [0.89-1.00] on both builds); no significance test is claimed.
- The by-type split is an accounting model over the transcripts, not an ablation: per-turn output is estimated (the
  transcript has each API call's context, not its final output count), result tokens are characters / 3.0, and a call
  belongs to one role. The unattributed remainder is shown, never spread.
- Provider prompt caching carries across runs (5-minute TTL); run order was shuffled and seeded so both builds are
  equally exposed, but per-run cache-write cost depends on what ran just before.
- Set A was pre-registered by git commit, not by an evidence freeze before the change list (integrity note);
  `mcp-audit usability bench` therefore refuses it, and these numbers come from `scripts/analyze.py` (cross-checked
  against the toolkit's per-task figures on relabelled copies).
- F2's recorded verdicts carry our checker defect; the post-hoc view is labelled and shown next to them.
- One client (an agent SDK through mcp-audit agent), isolation dev (operator user on macOS), one cluster (kind,
  Kubernetes 1.37.0), toolsets core + config (+ helm for F3). No compaction arm.
