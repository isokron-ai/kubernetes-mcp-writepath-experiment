# Changes in the Kubernetes MCP server experiment

Based on `containers/kubernetes-mcp-server` at tag v0.1.2, commit `66553fed` (the `upstream-v0.1.2` tag here; see
[EXPERIMENT.md](EXPERIMENT.md)). Two series of commits sit on top, in this order:

- **Fixed build** (16 commits, tag `fixed-v0.1.2`): one fix per bug in the [findings register](FINDINGS.md), each with
  a test that fails at the pin and passes with the fix. Patch files: [`experiment-patches/bugfix/`](experiment-patches/bugfix/),
  order in its `SERIES` file. Each was offered upstream in its issue (#1658 to #1672, and a comment on #1584).
- **Research build** (19 more commits, tag `research-v0.1.2`): the fixed build plus the usability changes and the
  design-gap changes. Patch files: [`experiment-patches/usability/`](experiment-patches/usability/).
- **Corrections after the measurement** (build `research-r2`): the research build plus fixes made after the benchmark
  was measured, from a comparison of v0.1.2 and the research build against the MCP Write Safety requirements on
  15 November 2026. Patch files: [`experiment-patches/corrections/`](experiment-patches/corrections/); see
  [Corrections after the measurement](#corrections-after-the-measurement). The benchmark measured `research`, not
  `research-r2`.

The harness and the write-ups call the builds `baseline`, `fixed` and `research`. The same 35 changes rebased on upstream
main `13bfb96f` are in [`experiment-patches/main-13bfb96f/`](experiment-patches/main-13bfb96f/README.md).

Every commit keeps the message it was made with; the messages carry the full reasoning, the trade-offs and the tests,
and `git show <commit>` gives the diff. The ids in each heading are the finding ids of the register (K8S-nn) and the
change-list ids of the usability pass (C1 to C32). The patches carry no `Signed-off-by` line; upstream's CONTRIBUTING
asks for a DCO sign-off, which is why no pull request has been opened.

Totals: fixed series 34 files, +1,385 / −223 lines, 24 test methods (58 subtests) added; usability series 58 files,
+6,474 / −131 lines, 41 test methods (59 subtests) added.

## Bug-fix series

### bugfix/01 (K8S-06, K8S-15): fix(core): split multi-document manifests like kubectl's YAML reader

Commit `710fbfb33a`. Patch `experiment-patches/bugfix/01-K8S-06-15-fix-core-split-multi-document-manifests-like-kubectl.patch`. Upstream: [#1659](https://github.com/containers/kubernetes-mcp-server/issues/1659).

- **Why:** A `--- ` (trailing space) or `--- # comment` separator made `resources_create_or_update` apply only the first document and report success; a trailing `---` and newline gave a bare `EOF` with nothing written, and a comment-only document failed after the earlier documents were applied.
- **Change:** Manifests are split like kubectl's YAML reader; trailing separators and comment-only documents are skipped.
- **Files:** `pkg/kubernetes/resources.go`
- **Tests and snapshots:** `pkg/kubernetes/manifests_test.go`, `pkg/mcp/resources_test.go`

### bugfix/02 (K8S-01): fix(core): name applied and unattempted documents when a multi-document apply fails

Commit `ef52462f38`. Patch `experiment-patches/bugfix/02-K8S-01-fix-core-name-applied-and-unattempted-documents-when.patch`. Upstream: [#1660](https://github.com/containers/kubernetes-mcp-server/issues/1660).

- **Why:** A multi-document apply stopped at the first refused document, left the earlier ones applied and named only the refused one.
- **Change:** The error names the documents already applied and those not attempted, and an unknown kind fails before any write. Preventing the partial write (validate first) is usability/03 (C28), in the research build only.
- **Files:** `pkg/kubernetes/resources.go`
- **Tests and snapshots:** `pkg/mcp/resources_test.go`

### bugfix/03 (K8S-03): fix(core): report pods_delete clean-up failures and delete the Pod first

Commit `2c782ca99a`. Patch `experiment-patches/bugfix/03-K8S-03-fix-core-report-pods_delete-clean-up-failures-and-de.patch`. Upstream: [#1665](https://github.com/containers/kubernetes-mcp-server/issues/1665).

- **Why:** `pods_delete` answered "Pod deleted successfully" when deleting its Service was refused, and deleted the Service before a Pod delete that could then fail.
- **Change:** The Pod is deleted first; Service and Route clean-up failures are reported (`PodCleanupError`).
- **Files:** `pkg/kubernetes/pods.go`, `pkg/toolsets/core/pods.go`
- **Tests and snapshots:** `pkg/mcp/pods_delete_test.go`

### bugfix/04 (K8S-07): fix(output): omit managedFields from write results

Commit `526af16d4e`. Patch `experiment-patches/bugfix/04-K8S-07-fix-output-omit-managedFields-from-write-results.patch`. Upstream: [#1669](https://github.com/containers/kubernetes-mcp-server/issues/1669).

- **Why:** Write results echoed `managedFields` (766 of 2,194 bytes of a Deployment write result); reads already stripped them.
- **Change:** `managedFields` are stripped from write results, lists included.
- **Files:** `pkg/output/output.go`
- **Tests and snapshots:** `pkg/mcp/resources_test.go`, `pkg/output/output_test.go`

### bugfix/05 (K8S-08): fix(kubevirt): refuse vm_create over an existing VirtualMachine

Commit `c88fc0c7d0`. Patch `experiment-patches/bugfix/05-K8S-08-fix-kubevirt-refuse-vm_create-over-an-existing-Virtu.patch`. Upstream: [#1663](https://github.com/containers/kubernetes-mcp-server/issues/1663).

- **Why:** `vm_create` with the name of an existing VirtualMachine force-applied over it (runStrategy Always became Halted) and answered "VirtualMachine created successfully".
- **Change:** `vm_create` refuses an existing VirtualMachine and names its uid and run strategy. It is a check-then-apply: a VM created by someone else between the read and the apply can still be applied over; closing that window needs a create (POST), which would change the tool's declared verbs.
- **Files:** `pkg/toolsets/kubevirt/vm/create/tool.go`
- **Tests and snapshots:** `pkg/mcp/kubevirt_test.go`, `pkg/mcp/testdata/toolsets-kubevirt-tools.json`

### bugfix/06 (K8S-09): fix(tekton,kubevirt): refuse wrongly typed params and networks arguments

Commit `e6b673828b`. Patch `experiment-patches/bugfix/06-K8S-09-fix-tekton-kubevirt-refuse-wrongly-typed-params-and-.patch`. Upstream: [comment on #1584](https://github.com/containers/kubernetes-mcp-server/issues/1584#issuecomment-6829524892).

- **Why:** Wrongly typed `params` (Tekton) and `networks` (`vm_create`) were dropped silently.
- **Change:** Wrongly typed values are refused with an error. Unknown arguments in general (the `dryRun: true` case) are usability/17 (C30).
- **Files:** `pkg/toolsets/kubevirt/vm/create/tool.go`, `pkg/toolsets/tekton/params.go`, `pkg/toolsets/tekton/pipeline.go`, `pkg/toolsets/tekton/task.go`
- **Tests and snapshots:** `pkg/mcp/kubevirt_test.go`, `pkg/mcp/tekton_test.go`

### bugfix/07 (K8S-11): fix(core): reject pods_run ports outside 1-65535

Commit `640e5049d2`. Patch `experiment-patches/bugfix/07-K8S-11-fix-core-reject-pods_run-ports-outside-1-65535.patch`. Upstream: [#1667](https://github.com/containers/kubernetes-mcp-server/issues/1667).

- **Why:** `pods_run` with a negative port succeeded without a Service; 4294967376 gave a Service on port 80.
- **Change:** The port must be 0 to 65535 before the int32 conversion. Fractional ports (8080.9 becomes 8080) are #1584 and unchanged.
- **Files:** `pkg/toolsets/core/pods.go`
- **Tests and snapshots:** `pkg/mcp/pods_run_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### bugfix/08 (K8S-12): fix(kubevirt): do not annotate vm_clone as idempotent

Commit `d0a337af34`. Patch `experiment-patches/bugfix/08-K8S-12-fix-kubevirt-do-not-annotate-vm_clone-as-idempotent.patch`. Upstream: [#1670](https://github.com/containers/kubernetes-mcp-server/issues/1670).

- **Why:** `vm_clone` was annotated `idempotentHint: true`, but every call creates a new VirtualMachineClone.
- **Change:** `idempotentHint: false`.
- **Files:** `pkg/toolsets/kubevirt/vm/clone/tool.go`
- **Tests and snapshots:** `pkg/mcp/kubevirt_test.go`, `pkg/mcp/testdata/toolsets-kubevirt-tools.json`

### bugfix/09 (K8S-14): fix(config): refuse unknown tool names in enabled_tools and disabled_tools

Commit `73d8d9cc83`. Patch `experiment-patches/bugfix/09-K8S-14-fix-config-refuse-unknown-tool-names-in-enabled_tool.patch`. Upstream: [#1668](https://github.com/containers/kubernetes-mcp-server/issues/1668).

- **Why:** A misspelt name in `enabled_tools` started the server with 0 tools; one in `disabled_tools` disabled nothing.
- **Change:** Unknown names in `enabled_tools` and `disabled_tools` fail start and reload. A name of a tool from a toolset that is not enabled is valid, as upstream main documents for `opt_in_tools`.
- **Files:** `docs/configuration.md`, `pkg/mcp/mcp.go`
- **Tests and snapshots:** `pkg/mcp/mcp_tools_test.go`

### bugfix/10 (K8S-19): fix(core): return pods_exec output when the command exits non-zero

Commit `acfd39dd04`. Patch `experiment-patches/bugfix/10-K8S-19-fix-core-return-pods_exec-output-when-the-command-ex.patch`. Upstream: [#1664](https://github.com/containers/kubernetes-mcp-server/issues/1664).

- **Why:** `pods_exec` dropped stdout and stderr when the command exited non-zero.
- **Change:** Both streams are returned with the error. Stderr hidden behind non-empty stdout on success (#1174) is usability/12 (C20).
- **Files:** `internal/test/mock_server.go`, `pkg/kubernetes/pods.go`, `pkg/toolsets/core/pods.go`
- **Tests and snapshots:** `pkg/kubernetes/pods_test.go`, `pkg/mcp/pods_exec_test.go`

### bugfix/11 (K8S-20): fix(helm): create chart objects in the namespace argument

Commit `808bb1d9c2`. Patch `experiment-patches/bugfix/11-K8S-20-fix-helm-create-chart-objects-in-the-namespace-argum.patch`. Upstream: [#1658](https://github.com/containers/kubernetes-mcp-server/issues/1658).

- **Why:** `helm_install` with `namespace: X` stored the release in X but created chart objects without `metadata.namespace` in the kubeconfig's namespace.
- **Change:** Helm's Kubernetes client gets the namespace argument.
- **Files:** `pkg/helm/helm.go`
- **Tests and snapshots:** `pkg/mcp/helm_test.go`

### bugfix/12 (K8S-17N): fix(helm): do not report a refused release lookup as "not found"

Commit `35b4ff40e1`. Patch `experiment-patches/bugfix/12-K8S-17N-fix-helm-do-not-report-a-refused-release-lookup-as-n.patch`. Upstream: [#1666](https://github.com/containers/kubernetes-mcp-server/issues/1666).

- **Why:** A release lookup refused by RBAC was reported as "Release X not found" with `isError: false`.
- **Change:** Only a missing release is "not found"; other lookup errors are returned as errors.
- **Files:** `pkg/helm/helm.go`
- **Tests and snapshots:** `pkg/mcp/helm_test.go`

### bugfix/13 (K8S-23): fix(core): skip the SelfSubjectAccessReview when a namespace is given

Commit `3afc3ed7fa`. Patch `experiment-patches/bugfix/13-K8S-23-fix-core-skip-the-SelfSubjectAccessReview-when-a-nam.patch`. Upstream: [#1671](https://github.com/containers/kubernetes-mcp-server/issues/1671).

- **Why:** Every namespaced list made a SelfSubjectAccessReview, even with the namespace given.
- **Change:** The access review is made only when no namespace is given (the case the code comment describes).
- **Files:** `pkg/kubernetes/resources.go`
- **Tests and snapshots:** `pkg/kubernetes/resources_list_access_review_test.go`

### bugfix/14 (K8S-24): fix(core): format events_list timestamps as RFC 3339 in UTC

Commit `a6ab94e7dd`. Patch `experiment-patches/bugfix/14-K8S-24-fix-core-format-events_list-timestamps-as-RFC-3339-i.patch`. Upstream: [#1672](https://github.com/containers/kubernetes-mcp-server/issues/1672).

- **Why:** `events_list` printed timestamps with Go's `time.String()` in the host's time zone.
- **Change:** RFC 3339 in UTC.
- **Files:** `pkg/kubernetes/events.go`
- **Tests and snapshots:** `pkg/mcp/events_test.go`

### bugfix/15 (K8S-25): fix(kubevirt): build vm_create's VirtualMachine as data, not templated YAML

Commit `b849257146`. Patch `experiment-patches/bugfix/15-K8S-25-fix-kubevirt-build-vm_create-s-VirtualMachine-as-dat.patch`. Upstream: [#1661](https://github.com/containers/kubernetes-mcp-server/issues/1661).

- **Why:** `vm_create` rendered its arguments into `vm.yaml.tmpl` with `text/template`, unescaped, so a `workload` value with a newline, `---` and a manifest applied that extra object too (within the server identity's RBAC), with the answer "VirtualMachine created successfully".
- **Change:** The VirtualMachine is built as an object, field by field, and applied as exactly one object; `vm.yaml.tmpl` is removed; a `workload` containing whitespace or control characters is refused.
- **Files:** `pkg/kubernetes/resources.go`, `pkg/toolsets/kubevirt/vm/create/tool.go`, `pkg/toolsets/kubevirt/vm/create/vm.yaml.tmpl`
- **Tests and snapshots:** `pkg/mcp/kubevirt_test.go`, `pkg/toolsets/kubevirt/vm/create/tool_test.go`

### bugfix/16 (K8S-02): fix(core): annotate pods_run as destructive and never take over existing objects

Commit `4395772676`. Patch `experiment-patches/bugfix/16-K8S-02-fix-core-annotate-pods_run-as-destructive-and-never-.patch`. Upstream: [#1662](https://github.com/containers/kubernetes-mcp-server/issues/1662).

- **Why:** `pods_run` was annotated `destructiveHint: false` (so it was served under `disable_destructive`) yet force-applied its Pod, Service and Route, taking over an existing Service with the same port.
- **Change:** Before the first write, an existing Pod, Service or Route of that name that `pods_run` did not create fails the call and nothing is changed; the tool is annotated destructive and declares the `get` verb it now uses.
- **Files:** `pkg/kubernetes/pods.go`, `pkg/toolsets/core/pods.go`
- **Tests and snapshots:** `pkg/mcp/pods_run_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

## Research build: usability series

19 commits on top of the fixed build (tag `research-v0.1.2`): every "build" item of the 32-item change list from the native-agent usability pass that is not already a bug fix (C1 = K8S-07, C4 = K8S-20, C17 = K8S-24 and C18 = K8S-23 are in the bug-fix series), plus the design gaps K8S-05, K8S-13, K8S-16 and K8S-21. Held, not built: C11 (compact text tables: invisible to a client that shows only structured content), C19 (structured content only when wanted: the measured client shows only structured content) and C31 (catalog de-duplication: with prompt caching it costs 0.1x; measure without caching first). These are IsoKron's design work for the measurement and are not offered upstream as they stand.

### usability/01 (C3, C25): feat(core): trimmed write receipts with output: full; scale results state the change (C3, C25)

Commit `287c3e0839`. Patch `experiment-patches/usability/01-C3-C25-feat-core-trimmed-write-receipts-with-output-full-sc.patch`. Upstream: not filed (research build).

- **Why:** Write results returned whole objects; scale results did not say what changed.
- **Change:** Write tools return a trimmed receipt by default (`output: full` restores the full object); scale results state the old and new replica counts.
- **Corrected after the measurement:** this receipt carried no field of the write, so a caller could not see what the
  call changed. In `research-r2` the default receipt also names each changed field with its new value; this receipt is
  `output: compact` ([corrections/01](#corrections01-res-4-featcore-write-receipts-name-the-fields-the-call-changed)).
- **Files:** `pkg/kubernetes/resources.go`, `pkg/toolsets/core/pods.go`, `pkg/toolsets/core/receipt.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/mcp/pods_run_test.go`, `pkg/mcp/resources_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/02 (C24, C26): feat(core): apply without force by default and report field conflicts; add create-only mode (C24, C26)

Commit `9aa0cfd320`. Patch `experiment-patches/usability/02-C24-C26-feat-core-apply-without-force-by-default-and-report-.patch`. Upstream: related: [#799](https://github.com/containers/kubernetes-mcp-server/issues/799).

- **Why:** `resources_create_or_update` force-applies every manifest, so a re-applied stale copy reverted another writer's change (native job 17, a lost update reported as success).
- **Change:** Server-side apply is no longer forced by default; a field conflict is returned naming each field and its manager; `force: true` restores the old behaviour; `mode: create` creates with POST and never modifies an existing object. This changes behaviour upstream chose on purpose: a proposal, not a fix.
- **Files:** `pkg/kubernetes/resources.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/mcp/resources_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/03 (C28): feat(core): validate multi-document manifests first; per-document results, dry_run and optional rollback (C28)

Commit `e6486e015b`. Patch `experiment-patches/usability/03-C28-feat-core-validate-multi-document-manifests-first-pe.patch`. Upstream: not filed (research build).

- **Why:** A multi-document apply could leave a partial write.
- **Change:** Every document is validated with a server-side dry run first; nothing is written if one is refused; per-document results; `dry_run`; optional compensating rollback (never atomic: objects that existed before are not restored).
- **Files:** `pkg/kubernetes/resources.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/kubernetes/resources_apply_rollback_test.go`, `pkg/mcp/resources_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/04 (C2): feat(core): add resources_patch with an optional resourceVersion precondition (C2)

Commit `6788d80a53`. Patch `experiment-patches/usability/04-C2-feat-core-add-resources_patch-with-an-optional-resou.patch`. Upstream: related: [#1352](https://github.com/containers/kubernetes-mcp-server/pull/1352).

- **Why:** Changing one field took a read and a full re-apply.
- **Change:** New `resources_patch` (strategic, merge or JSON patch, as proposed in #1352) with an optional `resourceVersion` precondition; JSON or YAML input; receipt result.
- **Files:** `pkg/kubernetes/patch.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/mcp/resources_patch_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/05 (C6): feat(core): delete results say gone or pending; Background propagation; uid/resourceVersion preconditions (C6)

Commit `411b6e472a`. Patch `experiment-patches/usability/05-C6-feat-core-delete-results-say-gone-or-pending-Backgro.patch`. Upstream: not filed (research build).

- **Why:** Deletes said "deleted" while finalizers held the object, orphaned a Job's Pods and had no precondition (K8S-04, K8S-10, K8S-22).
- **Change:** Delete results say gone, pending (with the finalizers) or terminating; propagation `Background` by default; optional `uid` and `resourceVersion` preconditions.
- **Files:** `pkg/kubernetes/pods.go`, `pkg/kubernetes/resources.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/kubernetes/resources_delete_test.go`, `pkg/mcp/resources_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/06 (C8): feat(core): batch delete, scale and label by selector with per-item results (C8)

Commit `943f9371a8`. Patch `experiment-patches/usability/06-C8-feat-core-batch-delete-scale-and-label-by-selector-w.patch`. Upstream: not filed (research build).

- **Why:** Bulk changes took one call per object (native job 14: 35 calls).
- **Change:** Delete, scale and label by label selector, with `dry_run`, per-item results and uid preconditions; best effort, and the result says so.
- **Files:** `pkg/kubernetes/batch.go`, `pkg/toolsets/core/batch.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/mcp/resources_batch_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/07 (C9, C27): feat(core): list several kinds in one call; multi-get and field projection (C9, C27)

Commit `83f9d698cf`. Patch `experiment-patches/usability/07-C9-C27-feat-core-list-several-kinds-in-one-call-multi-get-a.patch`. Upstream: not filed (research build).

- **Why:** Reading several kinds or objects took one call each.
- **Change:** `resources_list` takes `kinds` or `all_kinds`; `resources_get` takes `names`; both take `fields` for projection.
- **Files:** `pkg/kubernetes/batch.go`, `pkg/toolsets/core/multi.go`, `pkg/toolsets/core/projection.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/mcp/resources_multi_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`, `pkg/toolsets/core/projection_test.go`

### usability/08 (C22, C15): feat(core): trim structured list rows; say when a list is empty (C22, C15)

Commit `cc5418d580`. Patch `experiment-patches/usability/08-C22-C15-feat-core-trim-structured-list-rows-say-when-a-list-.patch`. Upstream: not filed (research build).

- **Why:** Structured list rows carried whole objects; an empty list was not stated.
- **Change:** Structured list rows drop what the request already fixes (apiVersion, kind, the given namespace) and the `-o wide` columns (`wide: true` keeps them); an empty list says so.
- **Files:** `pkg/toolsets/core/listtrim.go`, `pkg/toolsets/core/pods.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/mcp/resources_list_trim_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/09 (C13): feat(core): add auth_can_i (C13)

Commit `e6f9505e9f`. Patch `experiment-patches/usability/09-C13-feat-core-add-auth_can_i-C13.patch`. Upstream: not filed (research build).

- **Why:** An agent could not ask whether it may do something before trying (native job 09).
- **Change:** New `auth_can_i` (SelfSubjectAccessReview).
- **Files:** `pkg/kubernetes/auth_review.go`, `pkg/toolsets/core/auth.go`, `pkg/toolsets/core/toolset.go`
- **Tests and snapshots:** `pkg/mcp/auth_can_i_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/10 (C10): feat(core): add resources_rollout: restart, undo, status (C10)

Commit `ec76b63d9e`. Patch `experiment-patches/usability/10-C10-feat-core-add-resources_rollout-restart-undo-status-.patch`. Upstream: related: closed [#1294](https://github.com/containers/kubernetes-mcp-server/pull/1294).

- **Why:** Restart and rollback were done by hand-written annotations and full re-applies.
- **Change:** New `resources_rollout` with `restart`, `undo` (Deployments) and `status`; one tool with an action, where #1294 proposed four.
- **Files:** `pkg/kubernetes/rollout.go`, `pkg/toolsets/core/rollout.go`, `pkg/toolsets/core/toolset.go`
- **Tests and snapshots:** `pkg/mcp/resources_rollout_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/11 (C5, K8S-05): feat(core,helm): bounded waits on writes and deletes; helm_install wait, timeout and rollback_on_failure (C5, K8S-05)

Commit `cdaebde8e5`. Patch `experiment-patches/usability/11-C5-K8S-05-feat-core-helm-bounded-waits-on-writes-and-deletes-h.patch`. Upstream: related: [#1334](https://github.com/containers/kubernetes-mcp-server/pull/1334).

- **Why:** Agents polled after writes; `helm_install` waited 5 minutes with no rollback (K8S-05).
- **Change:** Optional bounded `wait` (0 to 120 s) on writes and deletes; `helm_install` takes `wait`, `timeout` (#1334's names) and `rollback_on_failure`; a failed install names the release state.
- **Files:** `pkg/helm/helm.go`, `pkg/kubernetes/wait.go`, `pkg/toolsets/core/resources.go`, `pkg/toolsets/core/rollout.go`, `pkg/toolsets/core/wait.go`, `pkg/toolsets/helm/helm.go`
- **Tests and snapshots:** `pkg/mcp/helm_test.go`, `pkg/mcp/resources_test.go`, `pkg/mcp/testdata/helm-chart-never-ready/Chart.yaml`, `pkg/mcp/testdata/helm-chart-never-ready/templates/deployment.yaml`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`, `pkg/mcp/testdata/toolsets-helm-tools.json`

### usability/12 (C20, C21): feat(core): pods_exec keeps stderr next to stdout; pods_log reports kubelet retrieval failures as errors (C20, C21)

Commit `6173f99246`. Patch `experiment-patches/usability/12-C20-C21-feat-core-pods_exec-keeps-stderr-next-to-stdout-pods.patch`. Upstream: related: [#1174](https://github.com/containers/kubernetes-mcp-server/pull/1174).

- **Why:** `pods_exec` hid stderr when stdout was non-empty; `pods_log` returned the kubelet's retrieval failure as a log line.
- **Change:** Stderr is returned after stdout; kubelet retrieval failures are errors.
- **Files:** `pkg/toolsets/core/pods.go`
- **Tests and snapshots:** `pkg/mcp/pods_exec_test.go`, `pkg/toolsets/core/pods_log_test.go`

### usability/13 (C7): feat(core): pods_run takes command, args and restartPolicy; create-only unless replace (C7)

Commit `c167b947f1`. Patch `experiment-patches/usability/13-C7-feat-core-pods_run-takes-command-args-and-restartPol.patch`. Upstream: not filed (research build).

- **Why:** `pods_run` could not run a command or a one-off Pod (native job 07).
- **Change:** `command`, `args` and `restartPolicy`; a re-run over objects an earlier `pods_run` created fails unless `replace: true`.
- **Files:** `pkg/kubernetes/pods.go`, `pkg/toolsets/core/pods.go`
- **Tests and snapshots:** `pkg/mcp/pods_run_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`

### usability/14 (C12, C23, K8S-21): feat(helm): helm_list shows every release state; helm_uninstall names the state left; all-namespaces fallback (C12, C23, K8S-21)

Commit `51a8566a0c`. Patch `experiment-patches/usability/14-C12-C23-feat-helm-helm_list-shows-every-release-state-helm_u.patch`. Upstream: not filed (research build).

- **Why:** `helm_list` used Helm's default filter, so a release stuck in `uninstalling` was invisible (K8S-21).
- **Change:** `helm_list` shows every state by default, with a `states` filter and an all-namespaces fallback; `helm_uninstall` names the state it left.
- **Files:** `pkg/helm/helm.go`, `pkg/toolsets/helm/helm.go`
- **Tests and snapshots:** `pkg/mcp/helm_test.go`, `pkg/mcp/testdata/toolsets-helm-tools.json`

### usability/15 (C14, K8S-16): feat(tekton): cancel reads the run and reports its state; restart keeps the run's labels and annotations (C14, K8S-16)

Commit `b799e1ee0b`. Patch `experiment-patches/usability/15-C14-K8S-16-feat-tekton-cancel-reads-the-run-and-reports-its-sta.patch`. Upstream: not filed (research build).

- **Why:** Tekton cancel did not report the run's state; restart dropped the run's labels (K8S-16).
- **Change:** Cancel reads the run and reports its state; restart keeps the run's own labels and annotations (not `tekton.dev/` ones).
- **Files:** `pkg/toolsets/tekton/pipelinerun.go`, `pkg/toolsets/tekton/taskrun.go`
- **Tests and snapshots:** `pkg/mcp/tekton_test.go`, `pkg/mcp/testdata/toolsets-tekton-tools.json`

### usability/16 (K8S-13): feat(kubevirt): vm_lifecycle run_policy; restart keeps the VM's run strategy (K8S-13)

Commit `aec5484161`. Patch `experiment-patches/usability/16-K8S-13-feat-kubevirt-vm_lifecycle-run_policy-restart-keeps-.patch`. Upstream: related: [#1655](https://github.com/containers/kubernetes-mcp-server/pull/1655).

- **Why:** `vm_lifecycle` start and restart overwrote the run strategy with Always; restart was two full PUTs.
- **Change:** `run_policy` as in #1655; restart keeps the VM's run strategy (one step beyond #1655); conflict retry.
- **Files:** `pkg/kubevirt/vm.go`, `pkg/toolsets/kubevirt/vm/lifecycle/tool.go`
- **Tests and snapshots:** `pkg/mcp/kubevirt_test.go`, `pkg/mcp/testdata/toolsets-kubevirt-tools.json`

### usability/17 (C30): feat(mcp): validate tool arguments against the input schema; refuse unknown arguments (C30)

Commit `32c705a0e4`. Patch `experiment-patches/usability/17-C30-feat-mcp-validate-tool-arguments-against-the-input-s.patch`. Upstream: not filed (research build).

- **Why:** Tool arguments were not checked against the input schema; unknown arguments (such as `dryRun`) were ignored (K8S-09).
- **Change:** Every call is validated against its input schema and unknown arguments are refused; required arguments are still reported by the handlers.
- **Files:** `pkg/mcp/argument_validation.go`, `pkg/mcp/tools_gosdk.go`
- **Tests and snapshots:** `pkg/mcp/argument_validation_test.go`, `pkg/mcp/configuration_test.go`, `pkg/mcp/helm_test.go`, `pkg/mcp/kubevirt_test.go`, `pkg/mcp/nodes_test.go`, `pkg/mcp/pods_test.go`, `pkg/mcp/resources_test.go`, `pkg/mcp/tekton_test.go`

### usability/18 (C29, C16): feat(mcp,core): default server instructions; descriptions say how apply, patch and charts work (C29, C16)

Commit `3b9b0f2ec8`. Patch `experiment-patches/usability/18-C29-C16-feat-mcp-core-default-server-instructions-descriptio.patch`. Upstream: not filed (research build).

- **Why:** No server instructions; descriptions did not say how apply, patch and charts behave.
- **Change:** Default server instructions when `server_instructions` is unset and `core` is enabled; descriptions explain the apply recipe, patching and chart paths.
- **Files:** `pkg/mcp/mcp.go`, `pkg/toolsets/core/events.go`, `pkg/toolsets/core/resources.go`, `pkg/toolsets/helm/helm.go`
- **Tests and snapshots:** `pkg/mcp/mcp_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`, `pkg/mcp/testdata/toolsets-helm-tools.json`

### usability/19 (C32): fix(helm): annotate helm_install as destructive and not idempotent (C32)

Commit `c9220491b1`. Patch `experiment-patches/usability/19-C32-fix-helm-annotate-helm_install-as-destructive-and-no.patch`. Upstream: not filed (research build).

- **Why:** `helm_install` was annotated non-destructive and idempotent.
- **Change:** `destructiveHint: true`, `idempotentHint: false` (the `pods_run` half of C32 is bugfix/16).
- **Files:** `pkg/toolsets/helm/helm.go`
- **Tests and snapshots:** `pkg/mcp/helm_test.go`, `pkg/mcp/testdata/toolsets-helm-tools.json`

## Corrections after the measurement

Made after the benchmark, on top of the research build (build `research-r2`, tree pinned as `RESEARCH_R2_TREE` in
[`harness/build/PIN`](harness/build/PIN)). They come from running the MCP Write Safety requirements (MWS 0.9) on v0.1.2
and on the research build side by side on 15 November 2026: each fixes a requirement the research build met less well
than v0.1.2. The benchmark figures in [results/](results/) are for `research`, without them.

### corrections/01 (RES-4): feat(core): write receipts name the fields the call changed

Commits `1aedbc5783` and `4b10d1b` (a shorter header line), one patch: `experiment-patches/corrections/01-RES4-feat-core-write-receipts-name-the-fields-the-call-c.patch`. Upstream: not filed (research build).

- **Why:** The usability/01 receipt gave each written object's identity and version only. MWS-RES-4 (a SHOULD) asks
  that a write's answer carry what the call changed, by name or new value; v0.1.2 met it by echoing the whole object,
  and the research build fell below it on `resources_create_or_update` (3 write paths: `violated` on the research
  build, `held` on v0.1.2).
- **Change:** The default receipt (`output: receipt`) adds, under `changed`, each field the call changed with its new
  value (null for a removed field). For an object that existed, these are the fields whose value differs between a
  read made just before the write and the API's answer; for a created object (`created: true`), the fields the call
  sent; if the earlier read fails, the fields the call sent, under `set`. Status and server-maintained metadata
  (`resourceVersion`, `generation`, `managedFields`, `uid`, `creationTimestamp`) are left out; strings are cut at 256
  characters and at most 50 fields are listed (the rest are counted). Applies to `resources_create_or_update`,
  `resources_patch` and `pods_run`; a dry run keeps the identity-only receipt.
- **Trade-off kept:** usability/01 trimmed the results for token cost (6,233 tokens of echoes in the first native
  usability series). That receipt is still there as `output: compact`; `output: full` still returns whole objects.
  The default answer stays a few lines for a one-field edit: on a local kind cluster, updating one key of a two-key ConfigMap answered in 547 bytes on v0.1.2 (whole object), 329 on the research build and about 380 on `research-r2` (`data.b: "3"` under `changed`). Each recorded write makes one extra read (`get`) before
  it; if that read is refused, the write still goes ahead. The tool list grows by 124 cl100k tokens (the longer
  `output` description on 4 tools; core toolset snapshot, 10,261 → 10,385).
- **Files:** `pkg/kubernetes/patch.go`, `pkg/kubernetes/pods.go`, `pkg/kubernetes/resources.go`, `pkg/toolsets/core/pods.go`, `pkg/toolsets/core/receipt.go`, `pkg/toolsets/core/resources.go`
- **Tests and snapshots:** `pkg/toolsets/core/receipt_test.go` (new), `pkg/mcp/resources_test.go`, `pkg/mcp/resources_patch_test.go`, `pkg/mcp/pods_run_test.go`, `pkg/mcp/testdata/toolsets-core-tools.json`, `pkg/mcp/testdata/toolsets-full-tools-multicluster.json`, `pkg/mcp/testdata/toolsets-full-tools-openshift.json`, `pkg/mcp/testdata/toolsets-full-tools.json`. The new update, patch and `pods_run` subtests fail on the research build and pass with the change.

## Upstream tests changed on purpose

Each in the commit that changes the behaviour, with the reason in its message: `events_list` timestamps (bugfix/14);
`TestRenderVMYaml` replaced by `TestBuildVM` (bugfix/15); the `pods_run` re-run expectation (usability/13); the
forced server-side apply test now passes `force: true` (usability/02); the K8S-01 partial-write test now expects the
dry-run refusal (usability/03); the Namespace delete now expects "pending" (usability/05); the empty-instructions test
(usability/18); the Tekton cancel fixture (usability/15); twelve assertions on handler messages for values the schema now
refuses, and a `nodes_log` test that passed a non-existent `tail` argument (usability/17).

One upstream test is order-dependent at the pin and was not changed: `TestPodsRun` run alone sees a Route because
another suite left the OpenShift Route API registered; it passes in the full suite on all three builds.

## Verification

- `go test -race -count=1 ./...` with upstream's envtest setup: 54 packages ok on baseline, fixed and research, and on
  upstream main `13bfb96f` plus all 35 rebased patches (go1.26.11, 11 November 2026).
- `go vet ./...` and `gofmt -l` clean on all builds; every commit is gofmt-clean on its own. golangci-lint was not run
  (upstream's `make lint` installs it with `curl | sh`).
- The fixed and research builds were each built twice with identical binary hashes.
- `research-r2` (15 November 2026): `go test -count=1 ./...` 54 packages ok, `go test -race` on `pkg/kubernetes` and
  `pkg/toolsets/core` ok, `go vet` and `gofmt -l` clean (go1.26.11); the rebased patch in
  `experiment-patches/main-13bfb96f/corrections/` applies with `git am` on main `13bfb96f` plus the 35, and
  `pkg/kubernetes`, `pkg/toolsets/core` and `pkg/mcp` pass there.
- Regression series (scripted, no model): every frozen probe re-run on the fixed and research builds, 60 runs; every
  item came out as pre-registered (14 research runs needed a post-hoc, streamed audit-log check; see
  [FINDINGS.md](FINDINGS.md), column "Regression").

## Tool-list size (cl100k tokens of the `tools` array, per server configuration)

| Configuration | v0.1.2 | fixed | research |
| --- | --- | --- | --- |
| core, config (default) | 21 tools / 6,807 | 21 / 6,819 | 25 / 10,499 (+54%), plus 177 tokens of default instructions |
| helm | 24 / 7,487 | 24 / 7,499 | 28 / 11,479 |
| tekton | 28 / 9,158 | 28 / 9,170 | 32 / 12,875 |
| kubevirt | 28 / 9,924 | 28 / 9,938 | 32 / 13,710 |
| `disable_destructive` | 16 / 5,351 | 15 / 5,088 (`pods_run` is now destructive) | 16 / 5,982 |

New tools in the research build: `resources_patch`, `resources_label`, `resources_rollout`, `auth_can_i`. Raw data:
[`results/catalog/weights-2026-11-11.json`](results/catalog/weights-2026-11-11.json).

## Changes that alter names or semantics

- New tools (research): `resources_patch`, `resources_label`, `resources_rollout`, `auth_can_i`. No tool is renamed or
  removed.
- Fixed build: `pods_run` is annotated destructive (hidden under `disable_destructive`) and refuses objects it did not
  create; `vm_create` refuses an existing VM and a `workload` with whitespace; unknown names in `enabled_tools` and
  `disabled_tools` stop the server from starting; wrongly typed `params` and `networks` are errors; `events_list` prints
  RFC 3339 UTC.
- Research build: server-side apply is not forced unless `force: true`; write results are receipts unless
  `output: full` (in `research-r2` the default receipt also names the changed fields; `output: compact` gives the
  identity-only receipt); unknown arguments are refused; deletes use `Background` propagation; `pods_run` re-runs need
  `replace: true`; `helm_list` lists every state; `helm_install` is annotated destructive; default server instructions
  are on when none are configured.
