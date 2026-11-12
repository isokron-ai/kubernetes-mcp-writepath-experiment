#!/usr/bin/env bash
# Seed one job's starting state for the native-agent usability session (series isokron-k8s-mcp-2026-11-native),
# as the CONTROL identity (lib/kc.sh: admin kubeconfig, kind context). Same objects as the Phase 1b seed
# (usability/seed.sh, sha256 d0b38641...); only the namespace is a parameter (the run's own, fresh namespace
# mcp-<run id>) and the run label value differs. drive.mjs calls it before each run; nothing here goes through
# the MCP server.
#   seed.sh JOB NS     JOB = 01..12; creates namespace NS with the kms RoleBindings and the seed objects;
#                      refuses an NS that already exists (every run gets a fresh namespace)
#   seed.sh crd        installs the job-12 test CRD (idempotent)
#   seed.sh cleanup    deletes every namespace labelled audit.isokron.ai/run=$RUN, and the test CRD
set -euo pipefail
export PATH=/opt/homebrew/bin:$PATH
KC="$(cd "$(dirname "$0")/../.." && pwd)/lib/kc.sh"
RUN=bench-session
NGINX=docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de
BUSYBOX=docker.io/library/busybox:1.37.0@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec
job="$1"
NS="${2:-}"
case "$job" in
  crd|cleanup) ;;
  0[1-9]|1[0-2])
    [[ "$NS" =~ ^mcp-[a-z0-9-]{1,50}$ ]] || { echo "seed.sh: NS must be mcp-<bench run id>, got '$NS'" >&2; exit 2; }
    if $KC get ns "$NS" >/dev/null 2>&1; then echo "seed.sh: namespace $NS already exists (runs need a fresh one)" >&2; exit 2; fi ;;
  *) echo "unknown job $job" >&2; exit 2 ;;
esac

ns_and_bindings() { # ns [helm]
  $KC apply --server-side --field-manager=ux-seed -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $1
  labels: {audit.isokron.ai/run: $RUN, app.kubernetes.io/managed-by: kubernetes-audit-harness}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: kms-probe-write, namespace: $1}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kms-probe-write}
subjects: [{kind: ServiceAccount, name: kms, namespace: mcp-audit-system}]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: kms-probe-write-addons, namespace: $1}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kms-probe-write-addons}
subjects: [{kind: ServiceAccount, name: kms, namespace: mcp-audit-system}]
EOF
  if [ "${2:-}" = helm ]; then
    $KC apply --server-side --field-manager=ux-seed -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: kms-helm-releases, namespace: $1}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kms-helm-releases}
subjects: [{kind: ServiceAccount, name: kms, namespace: mcp-audit-system}]
EOF
  fi
}

deploy() { # ns name replicas image [extra container yaml lines already indented 8 spaces]
  cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: $2, namespace: $1, labels: {app: $2}}
spec:
  replicas: $3
  selector: {matchLabels: {app: $2}}
  template:
    metadata: {labels: {app: $2}}
    spec:
      containers:
      - name: $2
        image: $4
EOF
}

apply() { $KC apply --server-side --field-manager=ux-seed -f -; }

case "$job" in
  01) ns_and_bindings $NS ;;
  02) ns_and_bindings $NS; deploy $NS web 2 "$NGINX" | apply ;;
  03) ns_and_bindings $NS
      apply <<EOF
apiVersion: v1
kind: ConfigMap
metadata: {name: app-config, namespace: $NS}
data: {LOG_LEVEL: info, CACHE_TTL: "300", FEATURE_SEARCH: "on", MAX_UPLOAD_MB: "20", REGION: eu-west, THEME: light}
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
EOF
      ;;
  04) ns_and_bindings $NS; deploy $NS api 2 "$NGINX" | apply ;;
  05) ns_and_bindings $NS
      { for n in svc-auth svc-billing svc-catalog svc-orders svc-search svc-users worker-mail worker-thumbs cache; do deploy $NS "$n" 0 "$NGINX"; done; } | apply ;;
  06) ns_and_bindings $NS
      apply <<EOF
apiVersion: apps/v1
kind: Deployment
metadata: {name: db, namespace: $NS, labels: {app: db}}
spec:
  replicas: 1
  selector: {matchLabels: {app: db}}
  template:
    metadata: {labels: {app: db}}
    spec:
      containers:
      - name: db
        image: $BUSYBOX
        command: ["sh", "-c", "while true; do nc -l -p 5432 -e echo ok; done"]
        ports: [{containerPort: 5432}]
---
apiVersion: v1
kind: Service
metadata: {name: db, namespace: $NS}
spec:
  selector: {app: db}
  ports: [{port: 5432, targetPort: 5432}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: worker, namespace: $NS, labels: {app: worker}}
spec:
  replicas: 1
  selector: {matchLabels: {app: worker}}
  template:
    metadata: {labels: {app: worker}}
    spec:
      containers:
      - name: worker
        image: $BUSYBOX
        command: ["sh", "-c", "echo worker starting; echo connecting to \$DB_HOST:\$DB_PORT; if ! nc -z -w 3 \$DB_HOST \$DB_PORT; then echo \"fatal: cannot reach database at \$DB_HOST:\$DB_PORT\" >&2; exit 1; fi; echo connected; sleep 3600"]
        env:
        - {name: DB_HOST, value: postgres-primary}
        - {name: DB_PORT, value: "5432"}
        - {name: DB_NAME, value: app}
        - {name: LOG_LEVEL, value: info}
EOF
      ;;
  07) ns_and_bindings $NS
      { deploy $NS web 2 "$NGINX"; cat <<EOF
---
apiVersion: v1
kind: Service
metadata: {name: web, namespace: $NS}
spec:
  selector: {app: web}
  ports: [{port: 80, targetPort: 80}]
EOF
      } | apply ;;
  08) ns_and_bindings $NS helm ;;
  09) ns_and_bindings $NS ;;
  10) ns_and_bindings $NS
      apply <<EOF
apiVersion: v1
kind: ConfigMap
metadata: {name: s-cfg-1, namespace: $NS, labels: {owner: session}}
data: {k: v1}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: s-cfg-2, namespace: $NS, labels: {owner: session}}
data: {k: v2}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: s-cfg-3, namespace: $NS, labels: {owner: session}}
data: {k: v3}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: s-cfg-held, namespace: $NS, labels: {owner: session}, finalizers: [isokron.test/hold]}
data: {k: held}
---
apiVersion: v1
kind: ServiceAccount
metadata: {name: s-sa-1, namespace: $NS, labels: {owner: session}}
---
apiVersion: v1
kind: ServiceAccount
metadata: {name: s-sa-2, namespace: $NS, labels: {owner: session}}
---
apiVersion: v1
kind: Service
metadata: {name: s-svc-1, namespace: $NS, labels: {owner: session}}
spec: {selector: {app: s-app-1}, ports: [{port: 80}]}
---
apiVersion: v1
kind: Service
metadata: {name: s-svc-2, namespace: $NS, labels: {owner: session}}
spec: {selector: {app: s-app-2}, ports: [{port: 80}]}
---
apiVersion: batch/v1
kind: Job
metadata: {name: s-job-1, namespace: $NS, labels: {owner: session}}
spec:
  template:
    spec:
      restartPolicy: Never
      containers: [{name: once, image: "$BUSYBOX", command: ["true"]}]
---
apiVersion: v1
kind: ConfigMap
metadata: {name: keep-cfg-1, namespace: $NS}
data: {k: keep1}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: keep-cfg-2, namespace: $NS}
data: {k: keep2}
---
apiVersion: v1
kind: Service
metadata: {name: keep-svc, namespace: $NS}
spec: {selector: {app: keep-web}, ports: [{port: 80}]}
EOF
      { for n in s-app-1 s-app-2 s-app-3; do deploy $NS "$n" 0 "$NGINX" | sed 's/labels: {app: '"$n"'}}$/labels: {app: '"$n"', owner: session}}/'; done; deploy $NS keep-web 0 "$NGINX"; } | apply ;;
  11) ns_and_bindings $NS
      apply <<EOF
apiVersion: tekton.dev/v1
kind: Pipeline
metadata: {name: build, namespace: $NS}
spec:
  params:
  - {name: target, type: string, default: all}
  - {name: sleep-seconds, type: string, default: "5"}
  tasks:
  - name: compile
    params:
    - {name: target, value: \$(params.target)}
    - {name: sleep-seconds, value: \$(params.sleep-seconds)}
    taskSpec:
      params:
      - {name: target, type: string}
      - {name: sleep-seconds, type: string}
      steps:
      - name: build
        image: $BUSYBOX
        script: |
          #!/bin/sh
          echo "building target=\$(params.target)"
          sleep \$(params.sleep-seconds)
          echo "done target=\$(params.target)"
EOF
      ;;
  12) ns_and_bindings $NS ;;
  crd) apply <<EOF
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: widgets.ux.audit.isokron.test, labels: {audit.isokron.ai/run: $RUN}}
spec:
  group: ux.audit.isokron.test
  scope: Namespaced
  names: {plural: widgets, singular: widget, kind: Widget, shortNames: [wdg]}
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            required: [size]
            properties:
              size: {type: integer, minimum: 1, maximum: 10}
              color: {type: string, enum: [red, green, blue]}
EOF
      $KC wait --for condition=Established --timeout=60s crd/widgets.ux.audit.isokron.test ;;
  cleanup)
      $KC delete ns -l audit.isokron.ai/run=$RUN --wait=false || true
      $KC delete crd widgets.ux.audit.isokron.test --ignore-not-found ;;
  *) echo "unknown job $job" >&2; exit 2 ;;
esac
