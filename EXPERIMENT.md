# Kubernetes MCP server write-path experiment

An independent research experiment by [IsoKron](https://isokron.ai). It is based on
[containers/kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server) at tag `v0.1.2` (commit
`66553fed`, released 4 November 2026), in three builds: the unmodified server, a fixed build (16 bug-fix commits) and a
research build (the bug fixes plus 19 usability changes that change how the server handles writes, results and errors),
with the build script, the benchmark task set and the results used to measure them.

> **Not affiliated with, sponsored by, or endorsed by the kubernetes-mcp-server maintainers or Red Hat.** This is not an
> official release and not a supported distribution. It was built to measure what fixing write-path and usability
> friction changes for AI agents, and it is published so others can inspect the changes and reproduce the measurement.
> Use the upstream server for real work. If you run these builds, point them at a disposable local cluster (for example
> kind) with a ServiceAccount that can write only where you expect it to.

Write-up: [Kubernetes MCP server v0.1.2 audit](https://isokron.ai/research/kubernetes-mcp-server-audit). Findings:
[FINDINGS.md](FINDINGS.md). Per-commit details: [CHANGES-EXPERIMENT.md](CHANGES-EXPERIMENT.md).

## What is in this repository

The first commit (tag `upstream-v0.1.2`) imports upstream's tree at `66553fed`, unmodified and without upstream's
history: `git rev-parse upstream-v0.1.2^{tree}` gives `d65794e676f0d74b42649d34d2dc9db48a94600f`, the same tree id as
`git rev-parse 66553fed^{tree}` in upstream's repository. On top of it come the 16 bug-fix commits (tag `fixed-v0.1.2`),
the 19 usability commits (tag `research-v0.1.2`), and then IsoKron's additions on `main`. `main` also removes upstream's
`.github/` automation (release workflows, Dependabot, CODEOWNERS), so that nothing of upstream's runs or requests
reviews here; the server source on `main` is the research build's.

| Path | What it is |
| --- | --- |
| [`EXPERIMENT.md`](EXPERIMENT.md), [`CHANGES-EXPERIMENT.md`](CHANGES-EXPERIMENT.md), [`FINDINGS.md`](FINDINGS.md), [`NOTICE`](NOTICE), [`SECURITY.md`](SECURITY.md), [`SUPPORT.md`](SUPPORT.md) | This description, the change list, the findings register, attribution, reporting |
| [`experiment-patches/`](experiment-patches/) | `bugfix/` (16) and `usability/` (19), each applied in its `SERIES` order to a clean v0.1.2 checkout; `corrections/` (1), made after the measurement, on top of them; `main-13bfb96f/`, the same changes rebased on upstream main |
| [`harness/build/`](harness/build/) | `build.sh baseline\|fixed\|research\|research-r2`: builds each binary from this repository alone |
| [`harness/`](harness/README.md) | The benchmark's task set as it was frozen: prompts, system prompt, seeds, mechanical checkers, cluster and server configuration, run helpers, the analysis script |
| [`results/`](results/) | [`BENCHMARK.md`](results/BENCHMARK.md), the per-run data and analysis of the measured series, the records of the measured builds, the tool-list weights |
| [`.github/workflows/ci.yml`](.github/workflows/ci.yml) | CI: both patch series applied with `git am` onto `upstream-v0.1.2` and checked against the build tags, and the three builds |

## Three builds

| Name | Git tag | What it is | Tree id |
| --- | --- | --- | --- |
| baseline | `upstream-v0.1.2` | upstream v0.1.2 (`66553fed`), unmodified | `d65794e676f0d74b42649d34d2dc9db48a94600f` |
| fixed | `fixed-v0.1.2` | baseline plus the 16 bug-fix commits | `934f9e8f678ffcbc25aadcb2aeaf460115715a3d` |
| research | `research-v0.1.2` | the fixed build plus the 19 usability commits | `766d2c64d818af9f6e022ff768be2f99658d1d15` |
| research-r2 | (tree pinned in `harness/build/PIN`) | the research build plus the corrections made after the measurement ([CHANGES-EXPERIMENT.md](CHANGES-EXPERIMENT.md#corrections-after-the-measurement)); not measured by the benchmark | `db20c4887808b495ebeb0e33a70fbeb3ae83720d` |

Binary sha256 of the builds that were **measured** (darwin/arm64, go1.26.11), and of the same builds made from this
repository with [`harness/build/build.sh`](harness/build/build.sh):

| Build | Measured binary | `build.sh`, darwin/arm64 | `build.sh`, linux/amd64 |
| --- | --- | --- | --- |
| baseline | `931b5f5a4a3947f9a4ab329e4731434dd8a9722aee681526f84fafb85130e889` | `3b563b61f1922f67b05af42170b7247704a404861f0762bddef5fd2c8a340601` | `a8b4267fabcb29b8c55011652cc7d38fe2155a899015c08ee0753ebd29859a28` |
| fixed | `bac15e1475c911b2ef43b06292a7a89d850fe3316f5eca614a8c8034c88af574` | `e6d67ca341f3664439b182971dc5add80c081de9887492afeba38e671b355f2e` | `9b2be870f2ea19cf1855509f64a7257c739be67b87f67b3d8d2ce73292530c4c` |
| research | `167a5a650592d314ffbb24ddbe3e2b81c45fb49c13062acd53c02052d58ddb82` | `c6a41bb1c3e16a7f1d31968a0844ee64a524e06cd37c512aed03a42f1eef7118` | `7db8367877cc410667fa685d0f5d6e7130aaa20537f9aef34dd01634bab019d6` |
| research-r2 | not measured | `c70ddc1a987909f0e93c59604a93b5cf48a74757087165e2a4a9cad6f8623adf` | `197d8bb31fe3daa7128a31963db3794f385e1b0247338acb2e5318a9152a1782` |

**Why the hashes differ (DRV-3).** `go build` embeds the version-control stamp of the git repository it runs in. The
measured binaries were built inside IsoKron's internal harness repository, so each carries that repository's
`vcs.revision`, `vcs.time` and `vcs.modified`, and its hash can be reproduced only from a clone of that repository at
the same commit (which IsoKron did, for the fixed and research builds). `build.sh` here builds with `-buildvcs=false`, so
its output depends only on the source, the Go version and the target platform, and anyone with go1.26.11 gets the hashes
above. `go version -m` on a measured binary and on the matching `build.sh` binary gives the same Go version, module
versions and build settings; the only difference is the four `vcs` lines of the measured one. The code is the same.
Each measured build's record, with every patch file's sha256 (each matches the file in `experiment-patches/`), is in
[`results/builds/`](results/builds/).

## What it changes

Each change targets a finding of IsoKron's audit or friction measured in a usability pass of 19 everyday jobs on the
unmodified server, done by an AI agent using the server natively. Details, files and tests for each:
[CHANGES-EXPERIMENT.md](CHANGES-EXPERIMENT.md); the findings: [FINDINGS.md](FINDINGS.md).

**Fixed build (16 commits, fixes for 17 bugs).** Each bug was reported upstream on 12 November 2026 with its patch
offered; all were open with no response yet on that day.

| Patch | Fix | Upstream |
| --- | --- | --- |
| bugfix/01 (K8S-06, K8S-15) | Split multi-document manifests like kubectl's YAML reader: `--- ` and `--- # c` separators no longer drop later documents, a trailing `---` no longer fails with `EOF` | [#1659](https://github.com/containers/kubernetes-mcp-server/issues/1659) |
| bugfix/02 (K8S-01) | A failed multi-document apply names the documents applied and those not attempted | [#1660](https://github.com/containers/kubernetes-mcp-server/issues/1660) |
| bugfix/03 (K8S-03) | `pods_delete` deletes the Pod first and reports Service or Route clean-up failures | [#1665](https://github.com/containers/kubernetes-mcp-server/issues/1665) |
| bugfix/04 (K8S-07) | Write results omit `managedFields` | [#1669](https://github.com/containers/kubernetes-mcp-server/issues/1669) |
| bugfix/05 (K8S-08) | `vm_create` refuses an existing VirtualMachine | [#1663](https://github.com/containers/kubernetes-mcp-server/issues/1663) |
| bugfix/06 (K8S-09) | Wrongly typed `params` (Tekton) and `networks` (`vm_create`) are refused instead of dropped | [comment on #1584](https://github.com/containers/kubernetes-mcp-server/issues/1584#issuecomment-6829524892) |
| bugfix/07 (K8S-11) | `pods_run` refuses ports outside 1-65535 | [#1667](https://github.com/containers/kubernetes-mcp-server/issues/1667) |
| bugfix/08 (K8S-12) | `vm_clone` is no longer annotated idempotent | [#1670](https://github.com/containers/kubernetes-mcp-server/issues/1670) |
| bugfix/09 (K8S-14) | Unknown tool names in `enabled_tools` and `disabled_tools` stop the server from starting | [#1668](https://github.com/containers/kubernetes-mcp-server/issues/1668) |
| bugfix/10 (K8S-19) | `pods_exec` returns the output of a command that exits non-zero | [#1664](https://github.com/containers/kubernetes-mcp-server/issues/1664) |
| bugfix/11 (K8S-20) | `helm_install` creates chart objects in the `namespace` argument | [#1658](https://github.com/containers/kubernetes-mcp-server/issues/1658) |
| bugfix/12 (K8S-17N) | `helm_uninstall` reports a refused release lookup as an error, not "not found" | [#1666](https://github.com/containers/kubernetes-mcp-server/issues/1666) |
| bugfix/13 (K8S-23) | No SelfSubjectAccessReview when a namespace is given | [#1671](https://github.com/containers/kubernetes-mcp-server/issues/1671) |
| bugfix/14 (K8S-24) | `events_list` timestamps in RFC 3339 UTC | [#1672](https://github.com/containers/kubernetes-mcp-server/issues/1672) |
| bugfix/15 (K8S-25) | `vm_create` builds its VirtualMachine as an object and applies exactly one; `workload` is a field value | [#1661](https://github.com/containers/kubernetes-mcp-server/issues/1661) |
| bugfix/16 (K8S-02) | `pods_run` is annotated destructive and never takes over objects it did not create | [#1662](https://github.com/containers/kubernetes-mcp-server/issues/1662) |

**Research build (19 usability commits, research only).** IsoKron's design work; several changes run against choices
upstream made on purpose, most of all the second one (forced apply, upstream #799), which is a proposal and not a fix.

| Patch | Change |
| --- | --- |
| usability/01 (C3, C25) | Write results are short receipts unless `output: full`; scale results state the change |
| usability/02 (C24, C26) | Server-side apply is not forced unless `force: true`; field conflicts are returned with their owners; `mode: create` never modifies an existing object |
| usability/03 (C28) | Multi-document applies are validated with a dry run first; per-document results, `dry_run`, optional compensating rollback |
| usability/04 (C2) | New `resources_patch` with an optional `resourceVersion` precondition |
| usability/05 (C6) | Delete results say gone, pending or terminating; `Background` propagation; optional uid and `resourceVersion` preconditions |
| usability/06 (C8) | Delete, scale and label by label selector, with per-item results |
| usability/07 (C9, C27) | List several kinds in one call; multi-get by name; field projection |
| usability/08 (C22, C15) | Trimmed structured list rows; an empty list says so |
| usability/09 (C13) | New `auth_can_i` |
| usability/10 (C10) | New `resources_rollout`: restart, undo, status |
| usability/11 (C5, K8S-05) | Bounded `wait` on writes and deletes; `helm_install` `wait`, `timeout`, `rollback_on_failure` |
| usability/12 (C20, C21) | `pods_exec` returns stderr next to stdout; `pods_log` reports kubelet retrieval failures as errors |
| usability/13 (C7) | `pods_run` takes `command`, `args`, `restartPolicy`; create-only unless `replace` |
| usability/14 (C12, C23, K8S-21) | `helm_list` shows every release state; `helm_uninstall` names the state left |
| usability/15 (C14, K8S-16) | Tekton cancel reports the run's state; restart keeps the run's labels and annotations |
| usability/16 (K8S-13) | `vm_lifecycle` `run_policy`; restart keeps the VM's run strategy |
| usability/17 (C30) | Tool arguments are validated against the input schema; unknown arguments are refused |
| usability/18 (C29, C16) | Default server instructions; descriptions say how apply, patch and charts behave |
| usability/19 (C32) | `helm_install` annotated destructive and not idempotent |

**Corrections after the measurement (build `research-r2`).** Made on 15 November 2026 after running the MCP Write
Safety requirements on v0.1.2 and on the research build side by side; the benchmark below measured the research build
without them.

| Patch | Change |
| --- | --- |
| corrections/01 (RES-4) | The default write receipt also names each field the call changed, with its new value; the identity-only receipt is `output: compact` |

## What it measured

The baseline and the research build (before/after; no fixed-only arm was run), on a local kind cluster (Kubernetes
1.37.0), never a hosted or real cluster. 244 measured runs on 12 November 2026 (01:27–04:45 UTC), one agent per run
through the Claude Agent SDK 0.3.319 with the server as the only tool source, toolsets `core` and `config` (plus `helm`
for one task), a fresh namespace per run, a seeded run order, and correctness from a mechanical checker reading the
cluster as a separate identity; no model judges anything. The harness was frozen and timestamped before the first
measured run. 204 runs passed and 40 failed; none was inconclusive or abandoned.

**Set A, pre-registered** (six ordinary jobs committed to git before any change was designed; the fair test), Claude
Opus 5.5 (`claude-opus-5-5`), 30 runs per build:

| Measure | v0.1.2 | research build | change |
| --- | --- | --- | --- |
| Runs correct | 30 of 30 | 30 of 30 | |
| Expected cost per verified success (USD) | 0.0975 | 0.0614 | −37.0% |
| Expected tokens per verified success | 66,796 | 64,574 | −3.3% |
| Tool calls per run (mean) | 9.73 | 3.97 | −59.2% |
| Model turns per run (mean) | 4.20 | 3.33 | −20.6% |
| Result bytes per run (mean) | 9,676 | 3,034 | −68.6% |
| Failed tool calls (total) | 11 | 0 | |

With Claude Sonnet 5.5 on set A: 30/30 on both builds, expected cost per success −29.3%, tokens +4.4%, tool calls per
run 7.40 → 3.70.

**Set B, friction-derived** (nine jobs from the usability pass plus one new twin; they favour the research build by
construction and are never pooled with set A), Opus, 50 runs per build: 25/50 → 35/50 correct as recorded, expected
cost per success −45.3%, tokens −12.0%, tool calls per run 13.16 → 5.30. One of our checker criteria (task F2) was
wrong; with a labelled post-hoc correction set B is 25/50 → 39/50.

Tokens are flat while cost falls: the research build's tool list is 54% larger (+3,692 cl100k tokens), which adds about
5.7 k tokens to every turn's context, but those are read back from the prompt cache at the cheapest rate, while the
saved calls and smaller results are paid at full price. Per-task tables, the split by type of change, the
observer-effect control, every failed run and the limits: [results/BENCHMARK.md](results/BENCHMARK.md).

## Evidence status

Every measured series (probes, the re-run on upstream main, the two native usability sessions, the regression series
and the benchmark) was frozen before its first run, with RFC 3161 timestamps from two authorities (Sectigo, DigiCert),
and its run records are on a hash chain. The benchmark series was anchored at its freeze; the others were anchored
later, on 11 November. **None of the series has been closed into an Evidence Pack yet**: there is no pack digest to
verify, and the gate records (the re-run on main, the refuter verdicts) and sign-offs are not on the chains. Until they
are, treat the numbers here as IsoKron's records, checkable against the published data and the frozen harness files
(byte-identical copies in [`harness/`](harness/README.md)), not as an independently verifiable pack.

## Build and compare

```sh
git clone <this repository> kubernetes-mcp-experiment && cd kubernetes-mcp-experiment
export GOTOOLCHAIN=local                       # never download a toolchain
export PATH=/opt/homebrew/opt/go@1.26/bin:$PATH # macOS with Homebrew go@1.26; any go1.26.11 works
for b in baseline fixed research research-r2; do harness/build/build.sh $b; done
shasum -a 256 harness/build/out/*/kubernetes-mcp-server
```

The script takes the `upstream-v0.1.2` tree (checking its tree id), applies the series in `SERIES` order with
`git apply`, checks that the patched tree equals the build's tag, and builds with the measured flags: `-trimpath`,
`CGO_ENABLED=0`, `-mod=readonly`, BuildTime set to the pin's commit date, version string `v0.1.2-<build>`, plus
`-buildvcs=false`. It records everything in `harness/build/out/<build>/BUILD.json`. `GOOS`/`GOARCH` cross-build;
`KMS_SOURCE=github` takes the tree from a fresh clone of upstream instead (same hash). go.mod asks for Go 1.26.10; the
measured builds used go1.26.11. Another Go version builds, but gives other hashes. The first build downloads the Go
modules (go.sum verified).

Or apply the patch series to a clean upstream checkout:

```sh
git clone https://github.com/containers/kubernetes-mcp-server && cd kubernetes-mcp-server
git checkout 66553fed5c16454e5814b41e331b4357f15806d8
for s in bugfix usability; do
  sed 's/#.*//; /^[[:space:]]*$/d' /path/to/experiment-patches/$s/SERIES |
    while read -r p; do git am "/path/to/experiment-patches/$s/$p" < /dev/null; done
done
```

(Stop after `bugfix` for the fixed build. For upstream main `13bfb96f`, use `experiment-patches/main-13bfb96f/` the same
way.) Upstream's own `make test` runs the upstream suites plus the tests each change adds. Run a binary like the
official server (`kubernetes-mcp-server --config <file.toml>` over stdio; [`harness/server/kms.toml`](harness/server/kms.toml)
is the measured configuration) and point your MCP client at it.

To repeat the measurement, see [`harness/README.md`](harness/README.md): the task set, seeds and checkers are here, but
the agent driver depends on IsoKron's internal tooling and is not published, so a rerun needs your own agent runner.

## Limitations

- One cluster distribution (kind), one Kubernetes version (1.37.0), one node. Not tested: OpenShift Route code paths,
  running VirtualMachines (KubeVirt findings rest on the CRDs), the kiali, kcp, netobserv and observability toolsets,
  the HTTP transport, load.
- The benchmark: five runs per task and build (two in the observer arm), so intervals are wide and no significance test
  is claimed; set A was pre-registered by git commit, not by an evidence freeze before the change list; set B favours the
  research build by construction; one task's checker was wrong (labelled post-hoc view); the split by type of change is
  an accounting model, not an ablation; one client; no fixed-only arm, so the bug fixes' own effect is not measured.
- The findings were found with AI assistance; the refuters' model is not recorded; the run records are not yet closed
  into Evidence Packs (above).
- The research build changes behaviour users rely on (forced apply, full-object write results, ignored unknown
  arguments). It is an experiment, not a proposed upstream release.

## Reporting

- **Your results differ from ours:** please open an issue here with the task, model, client version, build hashes and
  counts (tool calls, turns, failed calls, correctness). Reproductions that disagree are the most useful feedback.
- **A bug in one of the experiment commits, the build script or the task set:** open an issue here.
- **Anything in the upstream server:** report it to
  [containers/kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server), following its security policy
  for vulnerabilities. This repository does not track upstream.

This build is not maintained against later upstream releases. Fixes that prove useful are offered upstream as small,
separate pull requests once the sign-off question is settled.

## License

Apache License 2.0, as upstream; the license text is in [LICENSE](LICENSE). The experiment commits change upstream
files; each changed file is listed with its commit in [CHANGES-EXPERIMENT.md](CHANGES-EXPERIMENT.md), and the commit
history records every change. The experiment commits, the build script, the benchmark harness files and the
documentation and results are by IsoKron (Stormhaven Enterprises LLC) and are released under the same license; see
[NOTICE](NOTICE).
