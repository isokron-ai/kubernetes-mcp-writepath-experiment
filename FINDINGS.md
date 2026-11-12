# Findings register: kubernetes-mcp-server v0.1.2

The findings of IsoKron's agent-interface audit of
[containers/kubernetes-mcp-server](https://github.com/containers/kubernetes-mcp-server) v0.1.2 (commit `66553fed`),
11–12 November 2026, against a local single-node kind cluster (Kubernetes 1.37.0) with KubeVirt v1.9.0 CRDs, Tekton
Pipelines v1.16.0 and local Helm charts. All 26 register items are listed: **17 bugs**, **7 design gaps**, **2 intended**.
The patch for each is in [`experiment-patches/`](experiment-patches/); what each patch changes is in
[CHANGES-EXPERIMENT.md](CHANGES-EXPERIMENT.md).

How to read the tables:

- **Evidence.** *Probe*: a frozen, scripted probe with a raw-API or CLI control arm; the count is reproductions out of
  repetitions. *Test*: a new upstream-style Go test fails at the pin and passes with the patch (true for every bug with a
  patch). *Hand*: reproduced by hand outside a frozen series. The probe records and API audit-log extracts are not
  published (they carry cluster identifiers); the counts and verdicts are.
- **Gates.** *Main*: re-run on upstream main `13bfb96f` (10 November 2026). *Refuter*: three fresh agents tried to refute
  each claim, first from the claim and the source only, then with the evidence and their own raw runs; "narrowed" means
  the claim held in the narrower scope given.
- **Regression.** The same frozen probes re-run on the fixed and research builds (60 runs, 11 November 2026); "not
  reproduced" means the behaviour is gone in that build.
- **Severity** is IsoKron's, about reliability and usability for an agent, not a security rating. Line numbers are at
  v0.1.2. Upstream status as of 12 November 2026: every issue open.

## Bugs (17; 16 patches; 15 issues and 1 comment upstream)

| ID | Finding | Tool(s) | Cause (v0.1.2) | Severity | Evidence | Control | Gates | Regression (fixed / research) | Upstream | Patch |
|---|---|---|---|---|---|---|---|---|---|---|
| K8S-20 | `helm_install` with `namespace: X` stores the release record in X but creates every chart object without `metadata.namespace` in the kubeconfig's namespace | `helm_install` (and `helm_uninstall`) | `pkg/helm/helm.go` `newAction` 134-155 | medium (reliability) | Probe 3/3; Test | `helm install -n X` as the same identity puts everything in X; a chart that templates its namespace installs correctly through the tool 3/3 | main 3/3; refuter confirmed | not reproduced / not reproduced | [#1658](https://github.com/containers/kubernetes-mcp-server/issues/1658) | bugfix/11 |
| K8S-06 | `--- ` and `--- # c` separators drop every later document; the call reports success | `resources_create_or_update` | `pkg/kubernetes/resources.go` 66-73 | medium (reliability) | Probe 3/3 per shape; Test | `kubectl` applies both documents | main 3/3; refuter confirmed | not reproduced / not reproduced | [#1659](https://github.com/containers/kubernetes-mcp-server/issues/1659) | bugfix/01 |
| K8S-15 | A trailing `---` and newline gives a bare `EOF` with nothing written; a comment-only document fails after the earlier ones were applied | `resources_create_or_update` | `resources.go` 65-81 | medium (with K8S-06) | Probe 3/3 per shape; Test | `kubectl` applies both files | main 3/3; refuter narrowed (needs `---` then a newline) | not reproduced / not reproduced | [#1659](https://github.com/containers/kubernetes-mcp-server/issues/1659) | bugfix/01 |
| K8S-01 | A multi-document apply stops at the first refused document; earlier documents stay applied and the error names only the refused one | `resources_create_or_update` | `resources.go` 183-207; `toolsets/core/resources.go` 427-430 | medium (reliability) | Probe 3/3 for each of 4 shapes (invalid value, unknown kind, forbidden namespace, injected 500); Test | `kubectl apply --server-side` applies the others and reports the refused one | main 3/3; refuter confirmed | not reproduced / not reproduced | [#1660](https://github.com/containers/kubernetes-mcp-server/issues/1660) | bugfix/02 (report); usability/03 (validate first) |
| K8S-25 | `workload` is rendered into `vm.yaml.tmpl` unescaped, so `<image>\n---\n<manifest>` applies extra documents with the server identity; the answer is "VirtualMachine created successfully" | `vm_create` | `toolsets/kubevirt/vm/create/tool.go` 381, 427-431, 209 | medium (argument handling) | Probe 3/3; Test; Hand | The same text as a quoted YAML scalar stores one object | main 3/3; refuter narrowed (only through `workload`; still bounded by RBAC and `denied_resources`) | not reproduced / not reproduced | [#1661](https://github.com/containers/kubernetes-mcp-server/issues/1661) | bugfix/15 |
| K8S-02 | Annotated `destructiveHint: false` and served under `disable_destructive`, yet force-applies over an existing Service with the same port (type set to ClusterIP, nodePort dropped, selector replaced, same uid) | `pods_run` | `toolsets/core/pods.go` 339; `kubernetes/pods.go` 203-214, 258; `resources.go` 195-198 | medium (annotation) | Probe 3/3; config check; Test | Non-forced server-side apply of the same body 409 3/3; POST 409 3/3 | main 3/3; refuter narrowed (same port; selector replaced whole) | not reproduced / not reproduced | [#1662](https://github.com/containers/kubernetes-mcp-server/issues/1662) | bugfix/16 |
| K8S-08 | `vm_create` with an existing VM name force-applies over it (runStrategy Always becomes Halted) and answers "VirtualMachine created successfully" | `vm_create` | vm create `tool.go` 209-219 | medium (reliability) | Probe 3/3; Test | POST of the existing name 409 3/3 | main 3/3; refuter confirmed | not reproduced / not reproduced | [#1663](https://github.com/containers/kubernetes-mcp-server/issues/1663) | bugfix/05 |
| K8S-19 | A non-zero exit drops all output (only "command terminated with exit code 3") | `pods_exec` | `kubernetes/pods.go` 419-421; `toolsets/core/pods.go` 507-515 | medium (usability) | Probe 3/3; Test | `kubectl exec` prints both streams and exits 3 | main 3/3; refuter narrowed (non-zero exit only; stderr hidden behind stdout is #1174) | inconclusive as pre-registered (stderr still hidden behind stdout) / not reproduced | [#1664](https://github.com/containers/kubernetes-mcp-server/issues/1664) | bugfix/10; usability/12 (stderr) |
| K8S-03 | Reports "Pod deleted successfully" when the Service delete is refused; deletes the Service before a Pod delete that then fails | `pods_delete` | `kubernetes/pods.go` 96-123 | low–medium (reliability) | Probe 3/3 (RBAC) and 3/3 (injected 403); Test | Service DELETE as the server identity 403 3/3; raw Pod DELETE succeeds 3/3 | main 3/3; refuter narrowed | not reproduced / not reproduced | [#1665](https://github.com/containers/kubernetes-mcp-server/issues/1665) | bugfix/03 |
| K8S-17N | A release lookup refused by RBAC is reported as "Release X not found" with `isError: false` | `helm_uninstall` | `pkg/helm/helm.go` 116-132 | low–medium | Refuter's raw run; Hand (baseline and main "not found" with the release still installed; fixed build and Helm CLI 403); Test | Helm CLI returns the 403 | raised by a refuter; never in a frozen probe or on main live | not in the regression series | [#1666](https://github.com/containers/kubernetes-mcp-server/issues/1666) | bugfix/12 |
| K8S-09 | Wrongly typed `params` (Tekton) and `networks` (`vm_create`) are dropped silently; unknown arguments such as `dryRun` are ignored (a "dry run" writes) | `tekton_task_start`, `tekton_pipeline_start`, `vm_create`; every tool for unknown arguments | `pkg/mcp/tools_gosdk.go` 19-125 | low | Probe 3/3 for each of 3 shapes; Test | Object form sets params; array sets networks; raw `dryRun=All` stores nothing 3/3 | main 3/3; refuter narrowed (dryRun is a separate schema claim) | params/networks not reproduced on both; dryRun reproduced / not reproduced | [comment on #1584](https://github.com/containers/kubernetes-mcp-server/issues/1584#issuecomment-6829524892) | bugfix/06; usability/17 (unknown arguments) |
| K8S-11 | A negative port gives success and no Service; 4294967376 gives a Service on port 80 | `pods_run` | `toolsets/core/pods.go` 542; `kubernetes/pods.go` 203 | low | Probe 3/3 per value; Test | Raw Service POST with 70000 and −80 refused 422 3/3 | main 3/3; refuter confirmed | not reproduced / not reproduced | [#1667](https://github.com/containers/kubernetes-mcp-server/issues/1667) | bugfix/07 |
| K8S-14 | `enabled_tools = ["resources_gett"]` starts with 0 tools; `disabled_tools = ["pods_deleet"]` serves all 21; a toolset typo is refused | configuration | `pkg/mcp/mcp.go` 77-81 | low (usability) | Live, 3 starts each; Test | A toolset typo refuses to start 3/3 | main yes (also `opt_in_tools`); refuter confirmed | not reproduced / not reproduced | [#1668](https://github.com/containers/kubernetes-mcp-server/issues/1668) | bugfix/09 |
| K8S-07 | Write results echo `managedFields`; reads strip them | write tools | `pkg/output/output.go` 178-200 | low (usability) | Probe 3/3; Test | — (format) | main 3/3; refuter confirmed | not reproduced / not reproduced | [#1669](https://github.com/containers/kubernetes-mcp-server/issues/1669) | bugfix/04 |
| K8S-12 | Annotated `idempotentHint: true`, but every call creates a new VirtualMachineClone | `vm_clone` | `toolsets/kubevirt/vm/clone/tool.go` 43 | low (annotation) | Probe 3/3; config check; Test | — (annotation) | main 3/3; refuter confirmed | annotation fixed on both (a new clone per call is by design) | [#1670](https://github.com/containers/kubernetes-mcp-server/issues/1670) | bugfix/08 |
| K8S-23 | One SelfSubjectAccessReview before every namespaced list, even when the namespace is given | `resources_list`, `pods_list_in_namespace` and other namespaced lists | `pkg/kubernetes/resources.go` 40 | low (efficiency) | API audit log 3/3; Test | `kubectl get` as the server identity: 0 access reviews | main 3/3; refuter confirmed | 0 access reviews on both (streamed audit-log count) | [#1671](https://github.com/containers/kubernetes-mcp-server/issues/1671) | bugfix/13 |
| K8S-24 | Timestamps printed with Go's `time.String()` in the host's time zone (`-0700 MST`) | `events_list` | `pkg/kubernetes/events.go` 43 | low | Probe 3/3; Test | Raw GET returns RFC 3339 UTC 3/3 | main 3/3; refuter narrowed (depends on the host time zone) | not reproduced / not reproduced | [#1672](https://github.com/containers/kubernetes-mcp-server/issues/1672) | bugfix/14 |

Measured impact where one was measured: K8S-20 failed native usability job 08 (the agent blamed the chart) and benchmark
task F3 (0/5 on v0.1.2, 5/5 on the research build). K8S-07: a Deployment write result of 2,194 bytes carried 766 bytes
of `managedFields`; in the first native session `managedFields` were 6,373 tokens, 17.6% of all response tokens.
K8S-23: 58 extra API requests in the first native session. K8S-02 with a different port: the call fails after the Pod
was created, leaving the Pod behind (3/3).

## Design gaps (7; not filed upstream: issues are for bugs only)

| ID | Finding | Tool(s) | Severity | Evidence | Control | Refuter | Related upstream | Research-build change | Regression (fixed / research) |
|---|---|---|---|---|---|---|---|---|---|
| K8S-04 | DELETE sends no uid or resourceVersion precondition: a recreated object is deleted with success | `resources_delete` | low (reliability) | Probe 10/10 | DELETE with the old uid 409 3/3; `kubectl` has no uid precondition either | narrowed: design gap (kubectl parity) | — | usability/05 (C6): optional preconditions | reproduced / reproduced (opt-in; the frozen test sends none, as pre-registered) |
| K8S-05 | `helm_install` waits 5 minutes with no rollback option; a failed release blocks the name; a 60 s client timeout hides the outcome | `helm_install` | low–medium (usability) | Probe 3/3 at 300.1 s; 10/10 client timeouts | Helm CLI `--rollback-on-failure` cleans up | narrowed: Helm default; timeout part in #1334 | open PR #1334 | usability/11 (C5, K8S-05): `wait`, `timeout`, `rollback_on_failure` | reproduced / reproduced (opt-in, as pre-registered) |
| K8S-10 | "deleted successfully" while a finalizer holds the object | `resources_delete` | low–medium (usability) | Probe 3/3 | Raw DELETE body carries `deletionTimestamp`; `kubectl delete` waits | narrowed: reporting gap | — | usability/05 (C6): gone, pending or terminating | reproduced / not reproduced |
| K8S-13 | `start` and `restart` overwrite the run strategy with Always; restart is two full PUTs | `vm_lifecycle` | low–medium (reliability) | Probe 3/3 each; audit log 1 vs 2 PUTs | none available (no virt-api) | narrowed: start documented; restart in #1655 | open PR #1655 | usability/16: `run_policy`; restart keeps the strategy | reproduced / reproduced (start keeps #1655's default, as pre-registered; restart now keeps the strategy) |
| K8S-16 | Tekton restart copies only `spec`; labels on the run are dropped | `tekton_pipelinerun_lifecycle` (restart), `tekton_taskrun_restart` | low (usability) | Probe 3/3 | none available (no `tkn`) | narrowed: labels on the run itself; TaskRun too | — | usability/15 (C14, K8S-16) | reproduced / not reproduced |
| K8S-21 | After a failed uninstall the release stays `uninstalling`, and `helm_list` (Helm v3 default filter) shows nothing | `helm_uninstall`, `helm_list` | low–medium (usability) | Probe 3/3 | Helm CLI as the same identity also leaves `uninstalling` | narrowed: Helm v3 SDK default | — | usability/14 (C12, C23) | not reproduced / not reproduced (its trigger was K8S-20) |
| K8S-22 | Job delete sends no propagationPolicy, so its Pods are orphaned | `resources_delete` | low–medium (reliability) | Probe 3/3 | `Background` removes the Pod 3/3; a raw DELETE without a policy also orphans (API default for Jobs) | confirmed | — | usability/05 (C6): Background by default | reproduced / not reproduced |

## Intended (2) and closed candidates

- **K8S-17:** `helm_uninstall` of a missing release returns success "Release x not found". Intended upstream
  (`IgnoreNotFound`, asserted by an upstream test). Its RBAC-refusal variant is the bug K8S-17N.
- **K8S-18:** reads return the object twice (text and `structuredContent`). Intended (#1232, MCP spec). A client that
  shows the model only `structuredContent` pays for the duplicate in transport, not in model tokens.
- **Not reproduced:** unknown fields in an applied manifest (server-side apply on 1.37 refuses them under Strict and
  Warn, 3/3; CRDs with `x-kubernetes-preserve-unknown-fields` untested); changes to a finished Tekton run (the webhook
  refuses them and the tool surfaces it; a running run was not tested); the validator skipping PATCH (the API refuses
  the manifest).
- **Measurements and controls, not findings:** forced apply overwrites other writers (replicas 10/10, ConfigMap key
  10/10; known and intended upstream, #445 #799 #1164 #1184; changed only in the research build by usability/02, C24,
  as a proposal); a conflict on `resources_scale` is surfaced (injected 409 3/3, real contention 10/10; every PUT
  carries `resourceVersion`); a stale `resourceVersion` in an applied body is refused 409 10/10.

## Kubernetes API controls used

| Control | What the API does | Result of the control | Result through the tool |
|---|---|---|---|
| `resourceVersion` on update (PUT) | 409 when stale | stale raw PUT 409 3/3 | scale with an injected 409: error, object unchanged 3/3; real contention surfaced 10/10 |
| Server-side apply without force | conflict naming the owning field manager | non-forced raw apply 409 3/3 | forced apply took over 3/3 (K8S-02); another writer's change lost 10/10 and 10/10 |
| `fieldValidation=Strict` | 400 on unknown or duplicate fields | refused 3/3 (Warn also refused) | refused 3/3 (not reproduced) |
| Delete preconditions (`uid`) | 409 when the object was replaced | DELETE with the old uid 409 3/3 | the recreated object was deleted 10/10 (K8S-04) |
| `dryRun=All` | validates without persisting | stored nothing 3/3 | `dryRun: true` stored the object 3/3 (K8S-09) |
| POST of an existing name | 409 AlreadyExists | 409 3/3 | overwritten 3/3 (K8S-02, K8S-08) |
| Job delete with `propagationPolicy: Background` | cascades to Pods | Pod removed 3/3 | Pod orphaned 3/3 (K8S-22) |
| RBAC of the server identity | 403 outside the run namespace and on Secrets | Service DELETE as the server identity 403 3/3 | writes outside the run namespace refused in every K8S-20 call; 0 accepted server writes outside the probe namespaces in the regression series |
| CLI parity (`kubectl`, `helm`) | — | `kubectl` continues past a failed document, parses both separator shapes, prints both exec streams, waits on a finalizer, cascades a Job and makes no access review; `helm install -n` honours the namespace | — |
