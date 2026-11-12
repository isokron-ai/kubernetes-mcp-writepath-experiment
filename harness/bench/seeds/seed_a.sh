#!/usr/bin/env bash
# Set A (pre-registered K1-K6, audit repo kubernetes/TASKS-DRAFT.md at 255cc02) seeds for the Phase 4 benchmark
# (series isokron-k8s-mcp-2026-11-bench). Run as the CONTROL identity (lib/kc.sh); nothing goes through the MCP server.
# Open points of the draft settled here (nothing else):
#   - images by digest, preloaded on the kind node (bench/images.txt; checked by bench/drive.mjs ready), so no pull;
#   - every seed object is written with server-side apply, field manager seed-kubectl;
#   - the namespace is the task's fixed one (bench-kN, as the prompts say), deleted and re-created for every run.
# Also: the kms RoleBindings of every bench namespace (kms-probe-write, kms-probe-write-addons) as in the native seeds;
# K6 also binds kms-helm-releases (Secrets in this namespace only), since one of K6's labelled objects is a Secret
# and the server identity has no Secret access otherwise (the task would be impossible).
#   seed_a.sh K1..K6        create bench-kN fresh and seed it (refuses if bench-kN exists)
set -euo pipefail
export PATH=/opt/homebrew/bin:$PATH
KC="$(cd "$(dirname "$0")/../.." && pwd)/lib/kc.sh"
RUN=bench-session
NGINX=docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de
BUSYBOX=docker.io/library/busybox:1.37.0@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec
task="${1:-}"
case "$task" in K1|K2|K3|K4|K5|K6) ;; *) echo "seed_a.sh: task must be K1..K6, got '$task'" >&2; exit 2 ;; esac
NS="bench-$(echo "$task" | tr 'K' 'k')"
if $KC get ns "$NS" >/dev/null 2>&1; then echo "seed_a.sh: namespace $NS already exists (every run gets a fresh one)" >&2; exit 2; fi
apply() { $KC apply --server-side --field-manager=seed-kubectl -f - >/dev/null; }

{ cat <<EOF
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
EOF
if [ "$task" = K6 ]; then cat <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: kms-secrets, namespace: $NS}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: kms-helm-releases}
subjects: [{kind: ServiceAccount, name: kms, namespace: mcp-audit-system}]
EOF
fi; } | apply

deploy() { # name replicas labels-inner image [probe]
  cat <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: $1, namespace: $NS, labels: {$3}}
spec:
  replicas: $2
  selector: {matchLabels: {app: $1}}
  template:
    metadata: {labels: {app: $1}}
    spec:
      containers:
      - name: $1
        image: $4
        resources: {limits: {cpu: 200m, memory: 128Mi}}
EOF
  if [ "${5:-}" = probe ]; then cat <<EOF
        ports: [{containerPort: 80}]
        readinessProbe: {httpGet: {path: /, port: 80}, periodSeconds: 2}
EOF
  fi
}

case "$task" in
  K1) { deploy web 2 "app: web" "$NGINX" probe; cat <<EOF
---
apiVersion: v1
kind: Service
metadata: {name: web, namespace: $NS}
spec:
  selector: {app: web}
  ports: [{port: 80, targetPort: 80}]
---
apiVersion: v1
kind: ConfigMap
metadata: {name: web-config, namespace: $NS}
data: {GREETING: hello}
EOF
      } | apply ;;
  K2) apply <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: $NS
  labels: {team: core}
  annotations: {owner: platform}
data:
  LOG_LEVEL: info
  CACHE_TTL: "300"
  FEATURE_SEARCH: "on"
  MAX_UPLOAD_MB: "20"
  REGION: eu-west
  nginx.conf: |
    server {
      listen 80;
      location / { return 200 'ok'; }
    }
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
        resources: {limits: {cpu: 200m, memory: 128Mi}}
        volumeMounts: [{name: config, mountPath: /etc/app}]
      volumes: [{name: config, configMap: {name: app-config}}]
EOF
      ;;
  K3) ;;  # empty namespace: the agent applies the manifest given in the prompt
  K4) # revision 1: the good image; then revision 2: the broken one (busybox exits at once: CrashLoopBackOff; never
      # ready, since both revisions carry the same readiness probe on port 80). Everything but the image is identical
      # across the two revisions. With 2 replicas the rollout keeps both revision-1 pods (maxUnavailable rounds to 0).
      for img in "$NGINX" "$BUSYBOX"; do
        apply <<EOF
apiVersion: apps/v1
kind: Deployment
metadata: {name: api, namespace: $NS, labels: {app: api, team: core}}
spec:
  replicas: 2
  selector: {matchLabels: {app: api}}
  template:
    metadata: {labels: {app: api}}
    spec:
      containers:
      - name: api
        image: $img
        env: [{name: MODE, value: production}, {name: PORT, value: "80"}]
        resources: {limits: {cpu: 200m, memory: 128Mi}}
        readinessProbe: {httpGet: {path: /, port: 80}, periodSeconds: 2}
EOF
        if [ "$img" = "$NGINX" ]; then $KC -n "$NS" rollout status deployment/api --timeout=180s >/dev/null; fi
      done ;;
  K5) { for n in svc-auth svc-billing svc-catalog svc-orders svc-search svc-users worker-mail worker-thumbs cache; do
          deploy "$n" 0 "app: $n, component: ${n%%-*}, owner-team: team-${n#*-}" "$NGINX"; done; } | apply ;;
  K6) { cat <<EOF
apiVersion: v1
kind: ConfigMap
metadata: {name: s42-cfg-1, namespace: $NS, labels: {owner: session-42}}
data: {k: v1}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: s42-cfg-2, namespace: $NS, labels: {owner: session-42}}
data: {k: v2}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: s42-cfg-held, namespace: $NS, labels: {owner: session-42}, finalizers: [isokron.test/hold]}
data: {k: held}
---
apiVersion: v1
kind: Secret
metadata: {name: s42-token, namespace: $NS, labels: {owner: session-42}}
stringData: {token: not-a-real-secret}
---
apiVersion: v1
kind: Service
metadata: {name: s42-svc-1, namespace: $NS, labels: {owner: session-42}}
spec: {selector: {app: s42-app}, ports: [{port: 80}]}
---
apiVersion: v1
kind: Service
metadata: {name: s42-svc-2, namespace: $NS, labels: {owner: session-42}}
spec: {selector: {app: s42-app}, ports: [{port: 8080}]}
---
apiVersion: batch/v1
kind: Job
metadata: {name: s42-job, namespace: $NS, labels: {owner: session-42}}
spec:
  template:
    spec:
      restartPolicy: Never
      containers: [{name: once, image: "$BUSYBOX", command: ["true"]}]
---
apiVersion: v1
kind: ConfigMap
metadata: {name: keep-cfg, namespace: $NS}
data: {k: keep}
---
apiVersion: v1
kind: Secret
metadata: {name: keep-token, namespace: $NS}
stringData: {token: not-a-real-secret}
---
apiVersion: v1
kind: Service
metadata: {name: keep-svc, namespace: $NS}
spec: {selector: {app: keep-web}, ports: [{port: 80}]}
EOF
        deploy s42-app 1 "app: s42-app, owner: session-42" "$NGINX"
        deploy keep-web 1 "app: keep-web" "$NGINX"; } | apply ;;
esac
echo "seeded $task in $NS"
