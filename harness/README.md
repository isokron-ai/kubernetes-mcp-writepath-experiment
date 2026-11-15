# Harness: build script and benchmark task set

IsoKron's files for the measurement in [`../results/BENCHMARK.md`](../results/BENCHMARK.md): the build script for the three
builds, and the benchmark's task set (prompts, system prompt, seeds, mechanical checkers) with the cluster and server
configuration it ran against. Build names: `baseline` (tag `upstream-v0.1.2`), `fixed` (`fixed-v0.1.2`, plus
`../experiment-patches/bugfix/`) and `research` (`research-v0.1.2`, plus `../experiment-patches/usability/`). The
benchmark compared `baseline` and `research`.

The task-set files are published exactly as they were frozen for the measured series (sha256 in
[`../results/2026-11-12-bench2/FROZEN-FILES.sha256`](../results/2026-11-12-bench2/FROZEN-FILES.sha256)), so their comments
still name files of the full harness that are not published (`bench/drive.mjs`, `lib/target.mjs`, `PLAN.md`, the
ledger). They are not packaged as a general tool.

## Layout

    build/PIN, build/build.sh    the pin and the standalone build: build.sh baseline|fixed|research -> build/out/<name>/
    lib/kc.sh                    kubectl as the cluster admin (REPLACEMENT, see below)
    cluster/                     PIN (kind v0.33.0, kindest/node v1.37.0 by digest), kind-config.yaml (API server on
                                 127.0.0.1:16443, audit log on), audit-policy.yaml, server-rbac.yaml (the server's
                                 ServiceAccount mcp-audit-system/kms: cluster-wide read without Secrets, writes bound per
                                 namespace by the seeds), addons.yaml and addons/PIN (Tekton, KubeVirt CRDs, roles)
    server/kms.toml              the server configuration measured (toolsets core, config; stdio); variants/helm.toml for F3
    charts/ux-web/               the local Helm chart of task F3
    bench/system.md              the agent's system prompt
    bench/tasks/A/K1..K6.md      set A prompts (pre-registered); only the text between the PROMPT markers reaches the agent
    bench/tasks/B/F1a..F9.md     set B prompts (from the native usability jobs)
    bench/images.txt             images the seeds use, by digest, preloaded on the node
    bench/order.json             the seeded run order of the measured series
    bench/seeds/                 seed_a.sh K1..K6; seed_b.sh TASK NS (through native-seed.sh, native2-seed.sh)
    bench/checkers/              check_a.py (set A), check_b.py (set B), finalizer_ctl.py (K6's seed-side controller)
    usability/native*/           the native sessions' done-checks, which check_b.py imports unchanged
    bench/runctl.py              per run: fresh namespace, seed, settle, pre-state, clean-up
    bench/precheck.py            the scripted pre-check: an untouched state must fail, a correct end state must pass
    bench/analysis/analyze.py    the analysis that produced ../results/2026-11-12-bench2/ (reads the unpublished run store)

## What differs from the frozen harness

43 frozen files are published byte-identical. Changed or added for publication:

| File | Change |
| --- | --- |
| `build/build.sh`, `build/PIN` | Rewritten to build from this repository alone (the frozen versions built from a local upstream clone inside the harness); same flags plus `-buildvcs=false`, see [`../EXPERIMENT.md`](../EXPERIMENT.md) |
| `lib/kc.sh` | Replacement. The frozen one ran a target guard (`lib/target.mjs`, reading the harness's `config.env`) before calling kubectl; this one only names the admin kubeconfig and the kind context on every call and refuses identity-changing flags |
| `bench/analysis/analyze.py` | Not in the freeze: written after the runs, at the harness commit that produced the published analysis |
| `README.md`, `.gitignore` | New |

Not published, and why:

- `config.env`, `lib/target.mjs`, `tests/target.test.mjs`, `probe/target.json`, `toolkit/PIN`: the harness's local
  configuration and its guard. They hold local paths and the locations of credential files.
- `bench/drive.mjs`, `bench/make-config.py`, `bench/configs/agent.*.json`: the driver and the agent configurations.
  They run IsoKron's internal audit toolkit (not public), carry local paths, and name credential files by path. Their
  settings are in [`../results/BENCHMARK.md`](../results/BENCHMARK.md) (model, turns, caps, toolsets, order).
- `bench/precheck-2026-11-12.json`: the pre-check record (local paths); its result, 32/32, is in BENCHMARK.md.
- `usability/native*/seed.sh`: the native sessions' seeds; `bench/seeds/native-seed.sh` and `native2-seed.sh` are the
  copies the benchmark used (they differ in two lines: the namespace check and the run label).
- The built binaries and their records (see [`../results/builds/`](../results/builds/)), and everything written at
  run time: run records, transcripts, wire captures, API audit logs, cluster snapshots, the evidence chains.

## Using the task set

Everything here runs against a disposable local kind cluster only.

1. Create the cluster from `cluster/kind-config.yaml` with the pinned kind and node image (`cluster/PIN`), apply
   `cluster/server-rbac.yaml` and `cluster/addons.yaml` (and the add-ons in `cluster/addons/PIN`), and preload the images
   in `bench/images.txt` on the node.
2. Set `AUDIT_KUBECONFIG` to the cluster's admin kubeconfig (outside this repository) for `lib/kc.sh`, and give the MCP
   server a kubeconfig for the `mcp-audit-system/kms` ServiceAccount.
3. Per run: `bench/seeds/seed_a.sh K3` (set A, namespace `bench-k3`) or `bench/seeds/seed_b.sh F7 mcp-<run id>` (set B),
   then `bench/checkers/check_a.py pre ...`, run your agent with `bench/system.md` as the system prompt and the text
   between the PROMPT markers of the task file as the user message, then `bench/checkers/check_a.py post ...` (or
   `check_b.py`). The checkers read state as the admin identity and never read the agent's answer. `bench/runctl.py`
   and the checkers keep their state under `state/bench/` inside this directory.

The measured runs used an agent SDK (0.3.319) with the MCP server as the only tool source, at most 40 turns and
a 1.00 USD cap per run; see [`../results/BENCHMARK.md`](../results/BENCHMARK.md) for the rest of the method.
