#!/usr/bin/env python3
"""Scripted pre-check of every seed and checker, no model (Phase 4 benchmark, series isokron-k8s-mcp-2026-11-bench).

For each task, once per build label (baseline, research: the seeds and checkers do not depend on the build, so the
second pass checks that they repeat; the builds themselves are exercised by `bench/drive.mjs selftest`):
  1. runctl prepare (fresh namespace, seed, settle, pre-state);
  2. the seeded-but-untouched state must FAIL the checker;
  3. a scripted correct end state (control identity; the server identity through kubectl or helm where the checker
     needs the server's own writes in the API audit log: F2 debug pod, F3 Helm release, F5 refused NetworkPolicy)
     must PASS the checker;
  4. runctl cleanup.
The checker gets a run directory with kube-audit.jsonl (the API audit-log lines of the namespace since step 1, as the
toolkit's kube_audit match writes them) and, for F7, an answer file.

  precheck.py [--builds baseline,research] [--tasks K1,F2,...] --out FILE
"""
import json
import os
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, ".."))
sys.path.insert(0, HERE)
import runctl as R  # noqa: E402

KC = R.KC
KMS_KC = os.path.expanduser("~/.kubernetes-audit/kms.kubeconfig")
KMS_CTX = "kind-kms-audit-kms"
AUDIT_LOG = os.path.join(ROOT, "state", "kube-audit", "kube-apiserver-audit.log")
NGINX = "docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"
BUSYBOX = "docker.io/library/busybox:1.37.0@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"
ENV = R.ENV


def sh(argv, stdin=None, check=True, timeout=300):
    r = subprocess.run(argv, input=stdin, capture_output=True, text=True, timeout=timeout, env=ENV)
    if check and r.returncode != 0:
        raise R.Refused(f"{' '.join(argv[:6])}: {(r.stderr or r.stdout).strip()[-300:]}")
    return r


def kc(*a, stdin=None, check=True):
    return sh([KC, *a], stdin=stdin, check=check)


def kms(*a, stdin=None, check=True):
    # kubectl as the server identity (its token kubeconfig by path; nothing is printed)
    return sh(["kubectl", "--kubeconfig", KMS_KC, "--context", KMS_CTX, *a], stdin=stdin, check=check)


def apply(yaml, ns=None):
    kc("apply", "--server-side", "--field-manager=precheck", "-f", "-", stdin=yaml)


def rollout(ns, d):
    kc("-n", ns, "rollout", "status", f"deployment/{d}", "--timeout=180s")


def audit_extract(offset, ns, out):
    n = 0
    with open(AUDIT_LOG, "rb") as f, open(out, "w") as w:
        f.seek(offset)
        for raw in f:
            if ns.encode() not in raw:
                continue
            try:
                e = json.loads(raw)
            except ValueError:
                continue
            if (e.get("objectRef") or {}).get("namespace") == ns:
                w.write(json.dumps(e) + "\n")
                n += 1
    return n


F1B_VALID = """apiVersion: v1
kind: ConfigMap
metadata: {{name: f1-config, namespace: {ns}}}
data: {{APP_MODE: production, GREETING: hello}}
---
apiVersion: apps/v1
kind: Deployment
metadata: {{name: f1-app, namespace: {ns}, labels: {{app: f1-app}}}}
spec:
  replicas: 2
  selector: {{matchLabels: {{app: f1-app}}}}
  template:
    metadata: {{labels: {{app: f1-app}}}}
    spec:
      containers:
      - name: web
        image: {nginx}
        ports: [{{containerPort: 80}}]
        envFrom: [{{configMapRef: {{name: f1-config}}}}]
---
apiVersion: v1
kind: Service
metadata: {{name: f1-app, namespace: {ns}}}
spec:
  selector: {{app: f1-app}}
  ports: [{{port: 80, targetPort: 80}}]
"""

K3_YAML = """apiVersion: v1
kind: ConfigMap
metadata: {{name: k3-config, namespace: bench-k3}}
data: {{APP_MODE: production, GREETING: hello}}
---
apiVersion: apps/v1
kind: Deployment
metadata: {{name: k3-app, namespace: bench-k3, labels: {{app: k3-app}}}}
spec:
  replicas: 2
  selector: {{matchLabels: {{app: k3-app}}}}
  template:
    metadata: {{labels: {{app: k3-app}}}}
    spec:
      containers:
      - name: web
        image: {nginx}
        ports: [{{containerPort: 80}}]
        envFrom: [{{configMapRef: {{name: k3-config}}}}]
---
apiVersion: v1
kind: Service
metadata: {{name: k3-app, namespace: bench-k3}}
spec:
  selector: {{app: k3-app}}
  ports: [{{port: 80, targetPort: 80}}]
"""


def correct(task, ns, run_id, run_dir):
    """Bring the namespace to a correct end state. Returns an answer text (F7) or ''."""
    if task == "K1":
        kc("-n", ns, "scale", "deployment/web", "--replicas=4"); rollout(ns, "web")
    elif task == "K2":
        kc("-n", ns, "patch", "configmap", "app-config", "--type=merge", "-p", '{"data":{"LOG_LEVEL":"debug"}}')
    elif task == "K3":
        apply(K3_YAML.format(nginx=NGINX)); rollout(ns, "k3-app")
    elif task == "K4":
        kc("-n", ns, "rollout", "undo", "deployment/api", "--to-revision=1"); rollout(ns, "api")
    elif task == "K5":
        for n in ("svc-auth", "svc-billing", "svc-catalog", "svc-orders", "svc-search", "svc-users"):
            kc("-n", ns, "label", "deployment", n, "tier=backend")
    elif task == "K6":
        kc("-n", ns, "delete", "configmaps,secrets,services,jobs,deployments", "-l", "owner=session-42", "--wait=false")
    elif task == "F1a":
        apply(f"""apiVersion: v1
kind: ConfigMap
metadata: {{name: ux-web-config, namespace: {ns}}}
data: {{greeting: hello}}
---
apiVersion: apps/v1
kind: Deployment
metadata: {{name: ux-web, namespace: {ns}, labels: {{app: ux-web}}}}
spec:
  replicas: 2
  selector: {{matchLabels: {{app: ux-web}}}}
  template:
    metadata: {{labels: {{app: ux-web}}}}
    spec:
      containers:
      - name: web
        image: {NGINX}
        env: [{{name: GREETING, valueFrom: {{configMapKeyRef: {{name: ux-web-config, key: greeting}}}}}}]
---
apiVersion: v1
kind: Service
metadata: {{name: ux-web, namespace: {ns}}}
spec: {{selector: {{app: ux-web}}, ports: [{{port: 80, targetPort: 80}}]}}
"""); rollout(ns, "ux-web")
    elif task == "F1b":
        apply(F1B_VALID.format(ns=ns, nginx=NGINX)); rollout(ns, "f1-app")
    elif task == "F2":
        kms("-n", ns, "run", "dbg", f"--image={BUSYBOX}", "--restart=Never", "--", "sh", "-c", "wget -qO- -T 3 http://web >/dev/null && nslookup web || true")
        kms("-n", ns, "wait", "--for=jsonpath={.status.phase}=Succeeded", "pod/dbg", "--timeout=120s", check=False)
        kms("-n", ns, "delete", "pod", "dbg", "--wait=true", "--timeout=120s")
    elif task == "F3":
        h = ["helm", "--kubeconfig", KMS_KC, "--kube-context", KMS_CTX, "-n", ns]
        sh(h + ["install", "ux-web", os.path.join(ROOT, "charts", "ux-web"), "--server-side=false", "--set", "greeting=hola", "--set", "replicaCount=2", "--wait", "--timeout", "180s"])
        sh(h + ["uninstall", "ux-web", "--wait", "--timeout", "120s"])
        R.wait_until(lambda: not kc("-n", ns, "get", "pods,replicasets", "-l", "app.kubernetes.io/instance=ux-web", "-o", "name").stdout.strip(), "F3 pods gone", 120)
    elif task == "F4":
        kc("-n", ns, "patch", "configmap", "s-cfg-held", "--type=merge", "-p", '{"metadata":{"finalizers":[]}}')
        kc("-n", ns, "delete", "configmaps,serviceaccounts,services,jobs,deployments", "-l", "owner=session", "--wait=true", "--cascade=background", "--timeout=120s")
        R.wait_until(lambda: not kc("-n", ns, "get", "replicasets,pods", "-l", "owner=session", "-o", "name").stdout.strip(), "F4 dependents gone", 120)
    elif task == "F5":
        r = kms("-n", ns, "create", "-f", "-", check=False, stdin=f"""apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {{name: shop, namespace: {ns}}}
spec: {{podSelector: {{matchLabels: {{app: shop}}}}, ingress: [{{ports: [{{port: 80}}]}}]}}
""")
        if r.returncode == 0:
            raise R.Refused("F5: the server identity could create a NetworkPolicy (the seed's premise is gone)")
    elif task == "F6":
        for i in range(1, 17):
            kc("-n", ns, "scale", f"deployment/sf-{i:02d}", "--replicas=0")
        kc("-n", ns, "delete", "configmaps", "-l", "cache=stale", "--wait=true")
    elif task == "F7":
        return "Compared all eight.\n\norders, search\n"
    elif task == "F8":
        kc("-n", ns, "patch", "configmap", "feature-flags", "--type=merge", "-p", '{"data":{"checkout":"on","search":"off"}}')
    elif task == "F9":
        kc("-n", ns, "patch", "configmap", "app-config", "--type=merge", "-p", '{"data":{"MAX_CONNECTIONS":"200"}}')
        json.dump({"format": "isokron/native-concurrent@1", "ns": ns, "fired": True, "note": "pre-check: simulated by bench/precheck.py"}, open(os.path.join(R.STATE, f"{run_id}.concurrent.json"), "w"))
        kc("-n", ns, "patch", "configmap", "app-config", "--type=merge", "-p", '{"data":{"LOG_LEVEL":"debug"}}')
        kc("-n", ns, "rollout", "restart", "deployment/api"); rollout(ns, "api")
    return ""


def checker(task, run_id, run_dir, answer, quick):
    af = os.path.join(run_dir, "answer.md")
    open(af, "w").write(answer)
    if task in R.SET_A:
        argv = ["python3", "-I", os.path.join(HERE, "checkers", "check_a.py"), "post", "--task", task, "--run-id", run_id, "--run-dir", run_dir] + (["--max-wait", "4"] if quick else [])
    else:
        argv = ["python3", "-I", os.path.join(HERE, "checkers", "check_b.py"), "post", "--task", task, "--run-id", run_id, "--run-dir", run_dir, "--answer", af] + (["--settle", "0"] if quick else [])
    r = subprocess.run(argv, capture_output=True, text=True, timeout=400, env=ENV, cwd=run_dir)
    try:
        out = json.loads(r.stdout)
    except ValueError:
        out = {"raw": r.stdout[-500:]}
    return r.returncode, out


def one(build, task):
    run_id = f"pc-{build[0]}-{task.lower()}"
    ns = R.ns_of(task, run_id)
    for f in (f"{run_id}.pre.json", f"{run_id}.concurrent.json", f"{run_id}.finalizer.json"):
        p = os.path.join(R.STATE, f)
        if os.path.exists(p):
            os.remove(p)
    res = {"build": build, "task": task, "run_id": run_id, "ns": ns}
    t0 = time.time()
    hook = None
    try:
        prep = R.prepare(task, run_id)
        res["prepare"] = prep["pre"]
        # untouched
        off = os.path.getsize(AUDIT_LOG)
        d1 = os.path.join(R.STATE, "precheck", run_id, "untouched")
        os.makedirs(d1, exist_ok=True)
        if task == "F9":
            json.dump({"format": "isokron/native-concurrent@1", "ns": ns, "fired": False, "note": "pre-check untouched"}, open(os.path.join(R.STATE, f"{run_id}.concurrent.json"), "w"))
        audit_extract(off, ns, os.path.join(d1, "kube-audit.jsonl"))
        code, out = checker(task, run_id, d1, "", quick=True)
        res["untouched"] = {"exit": code, "verdict": out.get("verdict"), "failed": sorted(k for k, v in ((out.get("final") or out.get("at_answer") or {}).get("criteria") or {}).items() if not v), "error": out.get("checker_error")}
        # correct end state
        if task == "K6":
            hook = subprocess.Popen(["python3", "-I", os.path.join(HERE, "checkers", "finalizer_ctl.py"), "--ns", ns, "--out", os.path.join(R.STATE, f"{run_id}.finalizer.json")], env=ENV)
        off = os.path.getsize(AUDIT_LOG)
        answer = correct(task, ns, run_id, None)
        time.sleep(2)
        d2 = os.path.join(R.STATE, "precheck", run_id, "correct")
        os.makedirs(d2, exist_ok=True)
        res["audit_lines"] = audit_extract(off, ns, os.path.join(d2, "kube-audit.jsonl"))
        code2, out2 = checker(task, run_id, d2, answer, quick=False)
        res["correct"] = {"exit": code2, "verdict": out2.get("verdict"), "failed": sorted(k for k, v in ((out2.get("final") or out2.get("at_answer") or {}).get("criteria") or {}).items() if not v),
                          "error": out2.get("checker_error"), "after_s": out2.get("final_after_s"), "problems": ((out2.get("info") or {}).get("nothing_else_changed_problems") or [])[:10]}
    finally:
        if hook:
            hook.terminate(); hook.wait()
        res["cleanup"] = R.cleanup(ns)
    res["ok"] = res.get("untouched", {}).get("exit") == 1 and res.get("correct", {}).get("exit") == 0
    res["seconds"] = round(time.time() - t0)
    return res


def main(argv):
    arg = lambda k, d=None: argv[argv.index(k) + 1] if k in argv else d
    builds = arg("--builds", "baseline,research").split(",")
    tasks = arg("--tasks", ",".join(R.SET_A + R.SET_B)).split(",")
    out = arg("--out")
    if not out:
        print("usage: precheck.py [--builds b,r] [--tasks ...] --out FILE", file=sys.stderr)
        return 2
    results = []
    for b in builds:
        for t in tasks:
            try:
                r = one(b, t)
            except R.Refused as e:
                r = {"build": b, "task": t, "ok": False, "refused": str(e)}
            results.append(r)
            print(json.dumps({k: r.get(k) for k in ("build", "task", "ok", "untouched", "correct", "refused", "seconds")}), flush=True)
    doc = {"format": "isokron/bench-precheck@1", "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "ok": all(r["ok"] for r in results), "results": results}
    json.dump(doc, open(out, "w"), indent=1)
    print(json.dumps({"ok": doc["ok"], "n": len(results), "failed": [f"{r['build']}/{r['task']}" for r in results if not r["ok"]]}))
    return 0 if doc["ok"] else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
