#!/usr/bin/env bash
# kubectl as the CONTROL identity (the kind cluster's admin), for the seeds, checkers and run helpers in bench/.
#
# REPLACEMENT, written for this repository. The measured harness's lib/kc.sh first ran a target guard (lib/target.mjs,
# reading the harness's config.env) that refused any kubeconfig other than the local kind cluster's; neither file is
# published. This version keeps the contract the seeds and checkers rely on (the admin kubeconfig and the kind context
# named on every call, the ambient KUBECONFIG ignored, identity-changing flags refused) without the guard.
#
#   AUDIT_KUBECONFIG  path of the kind cluster's admin kubeconfig (required; keep it outside this repository)
#   KUBE_CONTEXT      its context (default kind-kms-audit, the cluster name in cluster/kind-config.yaml)
#
# Point it only at a disposable local cluster: the seeds create and delete namespaces and cluster-scoped objects.
set -euo pipefail
unset KUBECONFIG
: "${AUDIT_KUBECONFIG:?set AUDIT_KUBECONFIG to the kind cluster's admin kubeconfig}"
KUBE_CONTEXT="${KUBE_CONTEXT:-kind-kms-audit}"
for a in "$@"; do
  case "$a" in
    --kubeconfig*|--context*|--cluster|--cluster=*|--server|--server=*|-s|--user|--user=*|--token*|--as|--as=*|--as-group*|--as-uid*|--certificate-authority*|--client-*|--insecure-skip-tls-verify*)
      echo "REFUSED: lib/kc.sh sets the kubeconfig, context and identity itself (got $a)" >&2; exit 2 ;;
  esac
done
exec kubectl --kubeconfig "$AUDIT_KUBECONFIG" --context "$KUBE_CONTEXT" "$@"
