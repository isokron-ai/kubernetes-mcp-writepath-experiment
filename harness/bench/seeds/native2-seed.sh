#!/usr/bin/env bash
# Seed one job's starting state for the second native-agent session (series isokron-k8s-mcp-2026-11-native2;
# SOP v2.1 Phase 1b step 1a coverage: partial multi-resource write, bulk change, multi-read, retried write,
# concurrent edit, create-with-children, rollback). Run as the CONTROL identity (lib/kc.sh); nothing goes through the
# MCP server. Same namespace bindings as usability/native/seed.sh.
#   seed.sh JOB NS     JOB = 13..19; creates namespace NS (fresh) with the kms RoleBindings and the seed objects
#   seed.sh cleanup    deletes every namespace labelled audit.isokron.ai/run=$RUN
set -euo pipefail
export PATH=/opt/homebrew/bin:$PATH
KC="$(cd "$(dirname "$0")/../.." && pwd)/lib/kc.sh"
RUN=bench-session
NGINX=docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de
job="$1"
NS="${2:-}"
case "$job" in
  cleanup) $KC delete ns -l audit.isokron.ai/run=$RUN --wait=false || true; exit 0 ;;
  1[3-9])
    [[ "$NS" =~ ^mcp-[a-z0-9-]{1,50}$ ]] || { echo "seed.sh: NS must be mcp-<bench run id>, got '$NS'" >&2; exit 2; }
    if $KC get ns "$NS" >/dev/null 2>&1; then echo "seed.sh: namespace $NS already exists (runs need a fresh one)" >&2; exit 2; fi ;;
  *) echo "unknown job $job" >&2; exit 2 ;;
esac
apply() { $KC apply --server-side --field-manager=ux-seed -f -; }

apply <<EOT
apiVersion: v1
kind: Namespace
metadata:
  name: $NS
  labels: {audit.isokron.ai/run: $RUN, app.kubernetes.io/managed-by: kubernetes-audit-harness}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: kms-probe-write, namespace: $NS}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kms-probe-write}
subjects: [{kind: ServiceAccount, name: kms, namespace: mcp-audit-system}]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: kms-probe-write-addons, namespace: $NS}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kms-probe-write-addons}
subjects: [{kind: ServiceAccount, name: kms, namespace: mcp-audit-system}]
EOT

deploy() { # name replicas image [labels-json-inner] [limits-mem] [env-version]
  local labels="app: $1${4:+, $4}"
  cat <<EOT
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: $1, namespace: $NS, labels: {$labels}}
spec:
  replicas: $2
  selector: {matchLabels: {app: $1}}
  template:
    metadata: {labels: {app: $1}}
    spec:
      containers:
      - name: $1
        image: $3
        resources: {limits: {cpu: 250m, memory: ${5:-256Mi}}}
EOT
}

case "$job" in
  13) ;;  # empty namespace: the agent builds the shop app; NetworkPolicies are not writable for the server identity
  14) { for i in $(seq -w 1 16); do deploy "sf-$i" 1 "$NGINX" "app.kubernetes.io/part-of: storefront"; done
        for n in admin-ui reports mailer audit-trail; do deploy "$n" 1 "$NGINX" "app.kubernetes.io/part-of: backoffice"; done
        for i in $(seq -w 1 15); do printf -- '---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: cache-%s, namespace: %s, labels: {cache: stale}}\ndata: {k: "%s"}\n' "$i" "$NS" "$i"; done
        for n in 1 2 3; do printf -- '---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: cache-keep-%s, namespace: %s, labels: {cache: warm}}\ndata: {k: keep}\n' "$n" "$NS"; done
      } | apply ;;
  15) { for n in auth billing cart catalog payments users; do deploy "$n" 0 "$NGINX"; done
        deploy orders 0 docker.io/library/nginx:1.28-alpine
        deploy search 0 "$NGINX" "" 512Mi; } | apply ;;
  16) apply <<EOT
apiVersion: v1
kind: ConfigMap
metadata: {name: feature-flags, namespace: $NS, labels: {team: payments}}
data: {checkout: "off", payments: "on"}
EOT
      ;;
  17) apply <<EOT
apiVersion: v1
kind: ConfigMap
metadata: {name: app-config, namespace: $NS}
data: {LOG_LEVEL: info, MAX_CONNECTIONS: "100", REGION: eu-west, THEME: light}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: api, namespace: $NS, labels: {app: api}}
spec:
  replicas: 1
  selector: {matchLabels: {app: api}}
  template:
    metadata: {labels: {app: api}}
    spec:
      containers:
      - name: api
        image: $NGINX
        envFrom: [{configMapRef: {name: app-config}}]
EOT
      ;;
  18) ;;  # empty namespace: the agent creates ledger-api and its children
  19) for v in 1 2; do
        img=$NGINX; if [ $v = 2 ]; then img=docker.io/library/nginx:1.29.99-alpine; fi
        apply <<EOT
apiVersion: apps/v1
kind: Deployment
metadata: {name: pricing, namespace: $NS, labels: {app: pricing}}
spec:
  replicas: 2
  selector: {matchLabels: {app: pricing}}
  template:
    metadata: {labels: {app: pricing}}
    spec:
      containers:
      - name: pricing
        image: $img
        env: [{name: VERSION, value: "$v"}]
EOT
        if [ $v = 1 ]; then $KC -n "$NS" rollout status deployment/pricing --timeout=180s >/dev/null; fi  # DRV-2: an `&&` here ended the script with status 1
      done ;;
esac
