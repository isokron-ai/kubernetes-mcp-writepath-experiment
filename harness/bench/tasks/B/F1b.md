# F1 part b: NEW in set B, the friction-set twin of K3 planned in TASKS-DRAFT.md (comment separators + one invalid document)
Written for this benchmark (12 Nov 2026), after the change list: friction-derived, favours the research build by construction.
K3's manifest shape (ConfigMap, Deployment with 2 replicas, Service) with every separator written as "--- # comment" (H-11 /
K8S-06, K8S-15) and one document the API server rejects (a second Deployment with replicas -1; K8S-01 / C28). The documents
name the run's namespace. Seed: bench/seeds/seed_b.sh F1b (empty namespace); checker: bench/checkers/check_b.py (end state
only: the three valid objects as given and available, the rejected one absent, nothing else created).

<!-- PROMPT -->
Apply the following manifest to the `mcp-{{RUN_ID}}` namespace and confirm the Deployment `f1-app` is available. If any document in it is rejected, leave that one out (do not edit it to make it pass), apply the others, and tell me which document was rejected.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: f1-config
  namespace: mcp-{{RUN_ID}}
data:
  APP_MODE: production
  GREETING: hello
--- # batch worker
apiVersion: apps/v1
kind: Deployment
metadata:
  name: f1-worker
  namespace: mcp-{{RUN_ID}}
  labels:
    app: f1-worker
spec:
  replicas: -1
  selector:
    matchLabels:
      app: f1-worker
  template:
    metadata:
      labels:
        app: f1-worker
    spec:
      containers:
      - name: worker
        image: docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de
--- # web front end
apiVersion: apps/v1
kind: Deployment
metadata:
  name: f1-app
  namespace: mcp-{{RUN_ID}}
  labels:
    app: f1-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: f1-app
  template:
    metadata:
      labels:
        app: f1-app
    spec:
      containers:
      - name: web
        image: docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de
        ports:
        - containerPort: 80
        envFrom:
        - configMapRef:
            name: f1-config
--- # service
apiVersion: v1
kind: Service
metadata:
  name: f1-app
  namespace: mcp-{{RUN_ID}}
spec:
  selector:
    app: f1-app
  ports:
  - port: 80
    targetPort: 80
```
<!-- /PROMPT -->
