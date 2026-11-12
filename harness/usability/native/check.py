#!/usr/bin/env python3
"""Done-checks for the native-agent usability session (usability/native/; series isokron-k8s-mcp-2026-11-native).

Reads cluster state as the CONTROL identity (lib/kc.sh: the kind admin kubeconfig, never the agent's or the
server's identity) and never changes it. The agent's own answer is never read. One evaluator per job, the same
12 jobs and done-checks as the Phase 1b pass (audit repo kubernetes/friction-2026-11-11/usability.config.json),
made mechanical.

  check.py pre  --job JJ --ns NS --out FILE        snapshot the seeded starting state (run by drive.mjs after the seed)
  check.py post --job JJ --run-id R --run-dir DIR  the done-check (run by mcp-audit agent as the task's checker;
                                                   NS = mcp-R, pre-state = <harness>/state/native/R.pre.json)
  check.py selftest                                offline: every evaluator on synthetic objects (no cluster)

post prints one JSON object: criteria at the answer (checked as soon as the agent stopped), and, when those fail,
the same criteria again after SETTLE_S seconds ("settled"), so a premature "done" (the cluster was still converging)
can be told apart from a wrong end state. The verdict is the at-answer result. Exit 0 pass, 1 fail, 2 checker error.

Read-only except job 9's SubjectAccessReviews (a create that the API server evaluates and never stores).
"""
import copy
import json
import os
import re
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", ".."))
KC = os.path.join(ROOT, "lib", "kc.sh")
NGINX = "docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"
SETTLE_S = 45
WIDGET = "widgets.ux.audit.isokron.test"
KMS = "system:serviceaccount:mcp-audit-system:kms"

K = {  # short name -> kubectl resource
    "deploy": "deployments.apps", "rs": "replicasets.apps", "cm": "configmaps", "svc": "services",
    "sa": "serviceaccounts", "pod": "pods", "job": "jobs.batch", "eps": "endpointslices.discovery.k8s.io",
    "role": "roles.rbac.authorization.k8s.io", "rb": "rolebindings.rbac.authorization.k8s.io",
    "crb": "clusterrolebindings.rbac.authorization.k8s.io", "secret": "secrets",
    "pr": "pipelineruns.tekton.dev", "pipeline": "pipelines.tekton.dev", "widget": WIDGET,
}


class CheckerError(Exception):
    pass


# ---------------------------------------------------------------- cluster access (control identity)

class Live:
    def __init__(self, ns):
        self.ns = ns

    def _kc(self, args, stdin=None):
        r = subprocess.run([KC] + args, input=stdin, capture_output=True, text=True, timeout=60)
        return r.returncode, r.stdout, r.stderr

    def list(self, kind, ns="", selector=None):
        ns = self.ns if ns == "" else ns
        a = ["get", K[kind], "-o", "json"] + (["-n", ns] if ns else []) + (["-l", selector] if selector else [])
        code, out, err = self._kc(a)
        if code != 0:
            if "the server doesn't have a resource type" in err:
                return []
            raise CheckerError(f"kubectl {' '.join(a)}: {err.strip()[:300]}")
        return [strip(i) for i in json.loads(out).get("items", [])]

    def get(self, kind, name, ns=""):
        for i in self.list(kind, ns):
            if i["metadata"]["name"] == name:
                return i
        return None

    def logs(self, pod, ns=""):
        code, out, err = self._kc(["logs", "-n", ns or self.ns, pod, "--tail=200"])
        return out if code == 0 else ""

    def sar(self, user, ns, verb, resource, subresource=""):
        attrs = {"namespace": ns, "verb": verb, "resource": resource}
        if subresource:
            attrs["subresource"] = subresource
        body = json.dumps({"apiVersion": "authorization.k8s.io/v1", "kind": "SubjectAccessReview",
                           "spec": {"user": user, "groups": ["system:serviceaccounts", f"system:serviceaccounts:{ns}", "system:authenticated"],
                                    "resourceAttributes": attrs}})
        code, out, err = self._kc(["create", "-f", "-", "-o", "json"], stdin=body)
        if code != 0:
            raise CheckerError(f"SubjectAccessReview: {err.strip()[:300]}")
        return bool(json.loads(out).get("status", {}).get("allowed"))


def strip(o):
    o = copy.deepcopy(o)
    o.get("metadata", {}).pop("managedFields", None)
    return o


def audit_lines(run_dir):
    p = os.path.join(run_dir or "", "kube-audit.jsonl")
    if not run_dir or not os.path.exists(p):
        return None
    out = []
    for l in open(p):
        l = l.strip()
        if l:
            try:
                out.append(json.loads(l))
            except ValueError:
                pass
    return out


# ---------------------------------------------------------------- helpers

def names(items):
    return sorted(i["metadata"]["name"] for i in items)


def by_name(items):
    return {i["metadata"]["name"]: i for i in items}


def uid(o):
    return (o or {}).get("metadata", {}).get("uid")


def cond(o, t):
    for c in (o or {}).get("status", {}).get("conditions", []) or []:
        if c.get("type") == t:
            return c
    return {}


def container(o, idx=0):
    cs = (o or {}).get("spec", {}).get("template", {}).get("spec", {}).get("containers", []) or [{}]
    return cs[idx] if idx < len(cs) else {}


def env_map(c):
    return {e["name"]: e for e in c.get("env", []) or []}


def ready_endpoints(cl, svc):
    n = 0
    for s in cl.list("eps", selector=f"kubernetes.io/service-name={svc}"):
        for e in s.get("endpoints", []) or []:
            if (e.get("conditions") or {}).get("ready"):
                n += len(e.get("addresses", []) or [])
    return n


def pod_ready(p):
    return p.get("status", {}).get("phase") == "Running" and cond(p, "Ready").get("status") == "True" and not p["metadata"].get("deletionTimestamp")


def owned_by(p, kind, prefix):
    return any(r.get("kind") == kind and r.get("name", "").startswith(prefix) for r in p["metadata"].get("ownerReferences", []) or [])


def ts(s):
    # RFC 3339 'YYYY-MM-DDTHH:MM:SSZ' compares as text
    return s or ""


# ---------------------------------------------------------------- pre-state

PRE_KINDS = {
    "01": [], "08": [], "09": [], "12": [],
    "02": ["deploy", "rs"], "03": ["deploy", "rs", "cm"], "04": ["deploy", "rs"], "05": ["deploy", "rs"],
    "06": ["deploy", "rs", "svc"], "07": ["deploy", "rs", "svc", "pod"], "10": ["deploy", "cm", "svc", "sa", "job"],
    "11": ["pipeline", "pr"],
}


def pre(cl, job):
    return {k: cl.list(k) for k in PRE_KINDS[job]}


# ---------------------------------------------------------------- evaluators: (cl, pre, ctx) -> (criteria, info)

def j01(cl, P, ctx):
    c, i = {}, {}
    cm, d, s = cl.get("cm", "ux-web-config"), cl.get("deploy", "ux-web"), cl.get("svc", "ux-web")
    c["configmap_greeting_hello"] = bool(cm) and (cm.get("data") or {}).get("greeting") == "hello"
    c["deployment_2_replicas"] = bool(d) and d["spec"].get("replicas") == 2
    c["deployment_image_pinned"] = bool(d) and container(d).get("image") == NGINX
    e = env_map(container(d)).get("GREETING", {}).get("valueFrom", {}).get("configMapKeyRef", {})
    c["env_GREETING_from_configmap"] = e.get("name") == "ux-web-config" and e.get("key") == "greeting"
    c["deployment_available"] = cond(d, "Available").get("status") == "True"
    c["deployment_2_ready"] = (d or {}).get("status", {}).get("readyReplicas") == 2
    sel = (s or {}).get("spec", {}).get("selector") or {}
    tl = (d or {}).get("spec", {}).get("template", {}).get("metadata", {}).get("labels") or {}
    c["service_port_80"] = bool(s) and any(p.get("port") == 80 for p in s["spec"].get("ports", []))
    c["service_selects_the_pods"] = bool(sel) and all(tl.get(k) == v for k, v in sel.items())
    n = ready_endpoints(cl, "ux-web")
    i["ready_endpoints"] = n
    c["service_2_ready_endpoints"] = n == 2
    return c, i


def j02(cl, P, ctx):
    d, d0 = cl.get("deploy", "web"), by_name(P["deploy"]).get("web")
    c = {"same_uid": uid(d) == uid(d0) and uid(d) is not None,
         "spec_replicas_5": (d or {}).get("spec", {}).get("replicas") == 5,
         "ready_replicas_5": (d or {}).get("status", {}).get("readyReplicas") == 5,
         "no_new_replicaset": names(cl.list("rs")) == names(P["rs"])}
    return c, {"replicasets": names(cl.list("rs"))}


def j03(cl, P, ctx):
    cm, cm0 = cl.get("cm", "app-config"), by_name(P["cm"]).get("app-config")
    d, d0 = cl.get("deploy", "api"), by_name(P["deploy"]).get("api")
    data, data0 = (cm or {}).get("data") or {}, (cm0 or {}).get("data") or {}
    c = {"configmap_same_uid": uid(cm) == uid(cm0) and uid(cm) is not None,
         "LOG_LEVEL_debug": data.get("LOG_LEVEL") == "debug",
         "other_5_keys_unchanged": {k: v for k, v in data.items() if k != "LOG_LEVEL"} == {k: v for k, v in data0.items() if k != "LOG_LEVEL"},
         "deployment_same_uid": uid(d) == uid(d0) and uid(d) is not None}
    new_rs = [n for n in names(cl.list("rs")) if n not in names(P["rs"])]
    c["new_replicaset_after_edit"] = len(new_rs) > 0
    # the pods that serve now must have started after the ConfigMap's last change (envFrom is read at container start)
    changed = max([ts(m.get("time")) for m in ctx.get("cm_managed", {}).get("app-config", [])] or [""])
    pods = [p for p in cl.list("pod", selector="app=api") if pod_ready(p)]
    c["deployment_ready"] = cond(d, "Available").get("status") == "True" and (d or {}).get("status", {}).get("readyReplicas") == (d or {}).get("spec", {}).get("replicas")
    c["serving_pods_started_after_the_edit"] = bool(pods) and bool(changed) and all(ts(p["metadata"]["creationTimestamp"]) >= changed for p in pods)
    return c, {"new_replicasets": new_rs, "configmap_changed_at": changed, "ready_pods": [(p["metadata"]["name"], p["metadata"]["creationTimestamp"]) for p in pods]}


def j04(cl, P, ctx):
    d, d0 = cl.get("deploy", "api"), by_name(P["deploy"]).get("api")
    t, t0 = (d or {}).get("spec", {}).get("template", {}).get("spec"), (d0 or {}).get("spec", {}).get("template", {}).get("spec")
    c = {"same_uid": uid(d) == uid(d0) and uid(d) is not None,
         "image_back_on_the_pinned_digest": container(d).get("image") == container(d0).get("image") == NGINX,
         "available": cond(d, "Available").get("status") == "True",
         "ready_replicas_2": (d or {}).get("status", {}).get("readyReplicas") == 2,
         "pod_template_spec_unchanged": t == t0}
    i = {"image_now": container(d).get("image"), "revision": ((d or {}).get("metadata", {}).get("annotations") or {}).get("deployment.kubernetes.io/revision")}
    if t != t0:
        i["template_spec_keys_differing"] = sorted(k for k in set(t or {}) | set(t0 or {}) if (t or {}).get(k) != (t0 or {}).get(k))
    return c, i


def j05(cl, P, ctx):
    now, before = by_name(cl.list("deploy")), by_name(P["deploy"])
    c = {"same_nine_deployments": sorted(now) == sorted(before)}
    svc = [n for n in before if n.startswith("svc-")]
    other = [n for n in before if not n.startswith("svc-")]
    lab = lambda n: (now.get(n, {}).get("metadata", {}).get("labels") or {})
    c["six_svc_labelled_tier_backend"] = len(svc) == 6 and all(lab(n).get("tier") == "backend" for n in svc)
    c["three_others_not_labelled"] = all("tier" not in lab(n) for n in other)
    c["other_labels_unchanged"] = all({k: v for k, v in lab(n).items() if k != "tier"} == (before[n]["metadata"].get("labels") or {}) for n in before if n in now)
    c["uids_unchanged"] = all(uid(now.get(n)) == uid(before[n]) for n in before)
    c["generations_unchanged"] = all(now.get(n, {}).get("metadata", {}).get("generation") == before[n]["metadata"].get("generation") for n in before)
    c["no_new_replicasets"] = names(cl.list("rs")) == names(P["rs"])
    return c, {"labelled": sorted(n for n in now if lab(n).get("tier") == "backend")}


def j06(cl, P, ctx):
    ns = cl.ns
    d, d0 = cl.get("deploy", "worker"), by_name(P["deploy"]).get("worker")
    e, e0 = env_map(container(d)), env_map(container(d0))
    host = e.get("DB_HOST", {}).get("value")
    ok_hosts = {"db", f"db.{ns}", f"db.{ns}.svc", f"db.{ns}.svc.cluster.local", f"db.{ns}.svc.cluster.local."}
    c = {"same_uid": uid(d) == uid(d0) and uid(d) is not None,
         "DB_HOST_points_at_the_db_service": host in ok_hosts,
         "other_env_unchanged": {k: v for k, v in e.items() if k != "DB_HOST"} == {k: v for k, v in e0.items() if k != "DB_HOST"},
         "command_and_image_unchanged": container(d).get("command") == container(d0).get("command") and container(d).get("image") == container(d0).get("image"),
         "db_untouched": uid(cl.get("deploy", "db")) == uid(by_name(P["deploy"]).get("db")) and uid(cl.get("svc", "db")) == uid(by_name(P["svc"]).get("db"))}
    pods = [p for p in cl.list("pod", selector="app=worker") if pod_ready(p)]
    c["worker_pod_running_ready"] = len(pods) >= 1
    logs = {p["metadata"]["name"]: cl.logs(p["metadata"]["name"]) for p in pods}
    c["log_says_connected"] = any(re.search(r"^connected$", t, re.M) for t in logs.values())
    return c, {"DB_HOST": host, "ready_worker_pods": sorted(logs)}


def j07(cl, P, ctx):
    pods = cl.list("pod")
    stray = [p["metadata"]["name"] for p in pods if not owned_by(p, "ReplicaSet", "web-")]
    term = [p["metadata"]["name"] for p in pods if p["metadata"].get("deletionTimestamp")]
    s, s0 = cl.get("svc", "web"), by_name(P["svc"]).get("web")
    d, d0 = cl.get("deploy", "web"), by_name(P["deploy"]).get("web")
    c = {"debug_pod_gone": not stray, "nothing_terminating": not term,
         "service_unchanged": bool(s) and uid(s) == uid(s0) and s["metadata"]["resourceVersion"] == s0["metadata"]["resourceVersion"] and s["spec"] == s0["spec"],
         "deployment_unchanged": bool(d) and uid(d) == uid(d0) and d["metadata"].get("generation") == d0["metadata"].get("generation") and d["spec"] == d0["spec"],
         "service_2_ready_endpoints": ready_endpoints(cl, "web") == 2}
    extra = {k: [n for n in names(cl.list(k)) if n not in names(P[k])] for k in ("svc", "deploy")}
    return c, {"pods_not_from_web": stray, "terminating": term, "new_objects": extra}


def j08(cl, P, ctx):
    ns, lines = cl.ns, ctx.get("audit")
    c, i = {}, {}
    if lines is None:
        raise CheckerError("job 8 needs the run's kube-audit.jsonl (agent config kube_audit) to see what the install created")
    kms = [l for l in lines if (l.get("user") or {}).get("username") == KMS and l.get("verb") == "create" and l.get("stage", "ResponseComplete") == "ResponseComplete"]
    def created(res, name, where):
        return [l for l in kms if (l.get("objectRef") or {}).get("resource") == res and (l.get("objectRef") or {}).get("name", (l.get("requestObject") or {}).get("metadata", {}).get("name")) == name and (l.get("objectRef") or {}).get("namespace") == where]
    cm_ok = [l for l in created("configmaps", "ux-web-config", ns) if (l.get("responseStatus") or {}).get("code") == 201]
    dp_ok = [l for l in created("deployments", "ux-web-web", ns) if (l.get("responseStatus") or {}).get("code") == 201]
    c["install_created_the_configmap_in_the_namespace"] = bool(cm_ok)
    c["install_created_the_deployment_in_the_namespace"] = bool(dp_ok)
    c["value_greeting_hola"] = any(((l.get("requestObject") or {}).get("data") or {}).get("greeting") == "hola" for l in cm_ok)
    c["value_replicaCount_2"] = any(((l.get("requestObject") or {}).get("spec") or {}).get("replicas") == 2 for l in dp_ok)
    left = {k: names(cl.list(k, selector="app.kubernetes.io/instance=ux-web")) for k in ("cm", "deploy", "pod")}
    left_default = {k: names(cl.list(k, ns="default", selector="app.kubernetes.io/instance=ux-web")) for k in ("cm", "deploy")}
    rel = names(cl.list("secret", selector="owner=helm,name=ux-web"))
    c["uninstalled_no_release_record"] = not rel
    c["uninstalled_no_release_objects"] = not any(left.values()) and not any(left_default.values())
    i["kms_writes_outside_the_namespace"] = sorted({f"{l.get('verb')} {(l.get('objectRef') or {}).get('resource')}/{(l.get('objectRef') or {}).get('name', '')} in {(l.get('objectRef') or {}).get('namespace')} -> {(l.get('responseStatus') or {}).get('code')}"
                                                    for l in lines if (l.get("user") or {}).get("username") == KMS and l.get("verb") in ("create", "update", "patch", "delete")
                                                    and (l.get("objectRef") or {}).get("namespace") not in (ns, None, "")})
    i["left"] = {"namespace": left, "default": left_default, "release_secrets": rel}
    return c, i


def rules_set(role):
    out = set()
    for r in (role or {}).get("rules", []) or []:
        for g in r.get("apiGroups", [""]) or [""]:
            for res in r.get("resources", []) or []:
                for v in r.get("verbs", []) or []:
                    out.add((g, res, v))
    return out


def j09(cl, P, ctx):
    ns = cl.ns
    sa, role, rbs = cl.get("sa", "reader"), cl.get("role", "pod-reader"), cl.list("rb")
    want = {("", "pods", "get"), ("", "pods", "list"), ("", "pods", "watch"), ("", "pods/log", "get")}
    subj = lambda rb: any(s.get("kind") == "ServiceAccount" and s.get("name") == "reader" and s.get("namespace", ns) == ns for s in rb.get("subjects", []) or [])
    binds = [rb for rb in rbs if subj(rb)]
    c = {"serviceaccount_exists": bool(sa),
         "role_rules_exact": rules_set(role) == want,
         "rolebinding_role_to_reader": any(rb.get("roleRef", {}).get("kind") == "Role" and rb["roleRef"].get("name") == "pod-reader" for rb in binds),
         "no_other_binding_for_reader": len(binds) == 1,
         "nothing_cluster_scoped_for_reader": not [b for b in cl.list("crb", ns=None) if subj(b)]}
    user = f"system:serviceaccount:{ns}:reader"
    asks = {"list pods": ("list", "pods", ""), "get pods": ("get", "pods", ""), "watch pods": ("watch", "pods", ""), "get pods/log": ("get", "pods", "log"),
            "delete pods": ("delete", "pods", ""), "get secrets": ("get", "secrets", ""), "get configmaps": ("get", "configmaps", "")}
    got = {k: cl.sar(user, ns, *v) for k, v in asks.items()}
    c["allowed_read_pods_and_logs"] = all(got[k] for k in ("list pods", "get pods", "watch pods", "get pods/log"))
    c["denied_delete_pods_secrets_configmaps"] = not any(got[k] for k in ("delete pods", "get secrets", "get configmaps"))
    return c, {"rules": sorted(map(list, rules_set(role))), "access": got}


LABELLED_KINDS = ["cm", "sa", "svc", "job", "deploy", "pod", "rs"]


def j10(cl, P, ctx):
    left = {k: names(cl.list(k, selector="owner=session")) for k in LABELLED_KINDS}
    c = {"no_labelled_object_left": not any(left.values())}
    keep = {("cm", "keep-cfg-1"), ("cm", "keep-cfg-2"), ("svc", "keep-svc"), ("deploy", "keep-web")}
    def same(k, n):
        a, b = cl.get(k, n), by_name(P[k]).get(n)
        if not a or not b or uid(a) != uid(b) or a["metadata"].get("deletionTimestamp"):
            return False
        if k == "deploy":
            return a["metadata"].get("generation") == b["metadata"].get("generation") and a["spec"] == b["spec"] and a["metadata"].get("labels") == b["metadata"].get("labels")
        return a["metadata"]["resourceVersion"] == b["metadata"]["resourceVersion"]
    c["unlabelled_objects_unchanged"] = all(same(k, n) for k, n in keep)
    held = cl.get("cm", "s-cfg-held")
    orphans = names(cl.list("pod", selector="job-name=s-job-1"))
    return c, {"left": left, "held_configmap": {"present": bool(held), "deletionTimestamp": (held or {}).get("metadata", {}).get("deletionTimestamp"), "finalizers": (held or {}).get("metadata", {}).get("finalizers")}, "pods_of_the_deleted_job": orphans}


def params(pr):
    return {p.get("name"): p.get("value") for p in (pr.get("spec", {}).get("params") or [])}


def j11(cl, P, ctx):
    want = {"target": "api", "sleep-seconds": "60"}
    runs = cl.list("pr")
    match = [r for r in runs if {k: str(v) for k, v in params(r).items() if k in want} == want]
    cancelled = [r for r in match if "cancel" in str(cond(r, "Succeeded").get("reason", "")).lower() or str(r["spec"].get("status", "")).startswith("Cancelled")]
    first = min((r["metadata"]["creationTimestamp"] for r in cancelled), default=None)
    later = [r for r in match if r not in cancelled and first and ts(r["metadata"]["creationTimestamp"]) >= first]
    c = {"one_cancelled_run_with_the_params": len(cancelled) >= 1,
         "restarted_run_with_the_same_params": len(later) >= 1,
         "pipeline_unchanged": uid(cl.get("pipeline", "build")) == uid(by_name(P["pipeline"]).get("build"))}
    return c, {"runs": [(r["metadata"]["name"], params(r), cond(r, "Succeeded").get("reason"), r["spec"].get("status")) for r in runs]}


def j12(cl, P, ctx):
    w = cl.get("widget", "ux-w1")
    spec = (w or {}).get("spec") or {}
    return {"widget_exists": bool(w), "size_3": spec.get("size") == 3, "color_blue": spec.get("color") == "blue"}, {"spec": spec}


EVAL = {"01": j01, "02": j02, "03": j03, "04": j04, "05": j05, "06": j06, "07": j07, "08": j08, "09": j09, "10": j10, "11": j11, "12": j12}
SETTLES = {"01", "02", "03", "04", "06", "07", "10", "11"}  # criteria that wait on a controller


def context(cl, job, run_dir):
    ctx = {"audit": audit_lines(run_dir)}
    if job == "03":
        cm = None
        code, out, err = cl._kc(["get", "configmap", "app-config", "-n", cl.ns, "-o", "json", "--show-managed-fields"]) if isinstance(cl, Live) else (1, "", "")
        if code == 0:
            cm = json.loads(out)
        ctx["cm_managed"] = {"app-config": (cm or {}).get("metadata", {}).get("managedFields", []) if cm else getattr(cl, "managed", {}).get("app-config", [])}
    return ctx


def evaluate(cl, job, P, run_dir):
    crit, info = EVAL[job](cl, P, context(cl, job, run_dir))
    return {"pass": all(crit.values()), "criteria": crit}, info


def post(job, run_id, run_dir):
    ns = f"mcp-{run_id}"
    pre_file = os.path.join(ROOT, "state", "native", f"{run_id}.pre.json")
    if not os.path.exists(pre_file):
        raise CheckerError(f"no pre-state {pre_file} (drive.mjs writes it after seeding)")
    P = json.load(open(pre_file))
    if P.get("job") != job or P.get("ns") != ns:
        raise CheckerError(f"pre-state {pre_file} is for job {P.get('job')} ns {P.get('ns')}, not {job} {ns}")
    cl = Live(ns)
    at, info = evaluate(cl, job, P["objects"], run_dir)
    settled = None
    if not at["pass"] and job in SETTLES:
        time.sleep(SETTLE_S)
        settled, info2 = evaluate(cl, job, P["objects"], run_dir)
        info = {"at_answer": info, "settled": info2}
    out = {"format": "isokron/native-donecheck@1", "job": job, "run_id": run_id, "namespace": ns, "verdict": "pass" if at["pass"] else "fail",
           "at_answer": at, "settled": settled, "settle_seconds": SETTLE_S if settled else 0,
           "premature_done": bool(settled and settled["pass"] and not at["pass"]), "info": info,
           "checked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    print(json.dumps(out, indent=1, default=list))
    return 0 if at["pass"] else 1


# ---------------------------------------------------------------- selftest (offline, synthetic objects)

class Fake:
    """In-memory stand-in for Live: {kind: [objects]}, objects carry metadata.namespace."""

    def __init__(self, ns, objs, logs=None, allow=None, managed=None):
        self.ns, self.objs, self._logs, self.allow, self.managed = ns, objs, logs or {}, allow or set(), managed or {}

    def list(self, kind, ns="", selector=None):
        ns = self.ns if ns == "" else ns
        out = []
        for o in self.objs.get(kind, []):
            m = o["metadata"]
            if ns and m.get("namespace", self.ns) != ns:
                continue
            if selector and not all((m.get("labels") or {}).get(k) == v for k, v in (x.split("=", 1) for x in selector.split(","))):
                continue
            out.append(copy.deepcopy(o))
        return out

    def get(self, kind, name, ns=""):
        return next((o for o in self.list(kind, ns) if o["metadata"]["name"] == name), None)

    def logs(self, pod, ns=""):
        return self._logs.get(pod, "")

    def sar(self, user, ns, verb, resource, subresource=""):
        return f"{verb} {resource}{'/' + subresource if subresource else ''}" in self.allow


def obj(name, ns="ns", uid_=None, labels=None, **kw):
    m = {"name": name, "namespace": ns, "uid": uid_ or f"u-{name}", "resourceVersion": "1", "generation": 1, "creationTimestamp": "2026-11-12T00:00:00Z"}
    if labels:
        m["labels"] = labels
    m.update(kw.pop("meta", {}))
    return {"metadata": m, **kw}


def dep(name, replicas=1, image=NGINX, ready=None, labels=None, env=None, command=None, available=True, **meta):
    c = {"name": name, "image": image}
    if env is not None:
        c["env"] = env
    if command is not None:
        c["command"] = command
    return obj(name, labels=labels or {"app": name}, meta=meta,
               spec={"replicas": replicas, "selector": {"matchLabels": {"app": name}}, "template": {"metadata": {"labels": {"app": name}}, "spec": {"containers": [c]}}},
               status={"readyReplicas": replicas if ready is None else ready, "conditions": [{"type": "Available", "status": "True" if available else "False"}]})


def pod(name, labels, owner=None, ready=True, created="2026-11-12T00:10:00Z", **meta):
    m = {"creationTimestamp": created, **meta}
    if owner:
        m["ownerReferences"] = [{"kind": owner[0], "name": owner[1]}]
    return obj(name, labels=labels, meta=m, status={"phase": "Running", "conditions": [{"type": "Ready", "status": "True" if ready else "False"}]})


def eps(svc, n):
    return obj(f"{svc}-x", labels={"kubernetes.io/service-name": svc}, endpoints=[{"addresses": [f"10.0.0.{i}"], "conditions": {"ready": True}} for i in range(n)])


def svc(name, sel, port=80):
    return obj(name, spec={"selector": sel, "ports": [{"port": port}]})


def selftest():
    fails, n = [], 0

    def expect(job, cl, P, want, ctx=None, label=""):
        nonlocal n
        n += 1
        crit, info = EVAL[job](cl, P, ctx or {"audit": None, "cm_managed": getattr(cl, "managed", {})})
        got = all(crit.values())
        if got != want:
            fails.append(f"job {job} {label}: expected {'pass' if want else 'fail'}, got {crit}")
        return crit

    def mutate(objs, kind, name, fn):
        o = copy.deepcopy(objs)
        for x in o[kind]:
            if x["metadata"]["name"] == name:
                fn(x)
        return o

    # 01
    d = dep("ux-web", 2, env=[{"name": "GREETING", "valueFrom": {"configMapKeyRef": {"name": "ux-web-config", "key": "greeting"}}}])
    o = {"cm": [obj("ux-web-config", data={"greeting": "hello"})], "deploy": [d], "svc": [svc("ux-web", {"app": "ux-web"})], "eps": [eps("ux-web", 2)]}
    expect("01", Fake("ns", o), {}, True, label="good")
    expect("01", Fake("ns", mutate(o, "eps", "ux-web-x", lambda x: x["endpoints"].pop())), {}, False, label="one endpoint")
    expect("01", Fake("ns", mutate(o, "svc", "ux-web", lambda x: x["spec"].update(selector={"app": "other"}))), {}, False, label="wrong selector")
    # 02
    P = {"deploy": [dep("web", 2)], "rs": [obj("web-1")]}
    o = {"deploy": [dep("web", 5)], "rs": [obj("web-1")]}
    expect("02", Fake("ns", o), P, True, label="good")
    expect("02", Fake("ns", mutate(o, "deploy", "web", lambda x: x["status"].update(readyReplicas=3))), P, False, label="still rolling")
    expect("02", Fake("ns", {**o, "rs": [obj("web-1"), obj("web-2")]}), P, False, label="new rs")
    # 03
    data0 = {"LOG_LEVEL": "info", "CACHE_TTL": "300", "FEATURE_SEARCH": "on", "MAX_UPLOAD_MB": "20", "REGION": "eu-west", "THEME": "light"}
    P = {"cm": [obj("app-config", data=data0)], "deploy": [dep("api")], "rs": [obj("api-1")]}
    o = {"cm": [obj("app-config", data={**data0, "LOG_LEVEL": "debug"})], "deploy": [dep("api")], "rs": [obj("api-1"), obj("api-2")],
         "pod": [pod("api-2-a", {"app": "api"}, ("ReplicaSet", "api-2"), created="2026-11-12T00:10:00Z")]}
    mf = {"app-config": [{"time": "2026-11-12T00:00:00Z"}, {"time": "2026-11-12T00:09:00Z"}]}
    expect("03", Fake("ns", o, managed=mf), P, True, label="good")
    expect("03", Fake("ns", {**o, "rs": [obj("api-1")]}, managed=mf), P, False, label="not restarted")
    expect("03", Fake("ns", mutate(o, "cm", "app-config", lambda x: x["data"].update(THEME="dark")), managed=mf), P, False, label="other key changed")
    expect("03", Fake("ns", o, managed={"app-config": [{"time": "2026-11-12T00:11:00Z"}]}), P, False, label="pod older than edit")
    # 04
    P = {"deploy": [dep("api", 2)], "rs": [obj("api-1")]}
    o = {"deploy": [dep("api", 2)], "rs": [obj("api-1"), obj("api-2")]}
    expect("04", Fake("ns", o), P, True, label="good")
    expect("04", Fake("ns", mutate(o, "deploy", "api", lambda x: x["spec"]["template"]["spec"]["containers"][0].update(image="docker.io/library/nginx:1.29.99-alpine"))), P, False, label="bad image left")
    expect("04", Fake("ns", mutate(o, "deploy", "api", lambda x: x["spec"]["template"]["spec"]["containers"][0].update(imagePullPolicy="Always"))), P, False, label="template drift")
    # 05
    allnames = ["svc-auth", "svc-billing", "svc-catalog", "svc-orders", "svc-search", "svc-users", "worker-mail", "worker-thumbs", "cache"]
    P = {"deploy": [dep(n, 0) for n in allnames], "rs": [obj(n + "-1") for n in allnames]}
    o = {"deploy": [dep(n, 0, labels={"app": n, **({"tier": "backend"} if n.startswith("svc-") else {})}) for n in allnames], "rs": P["rs"]}
    expect("05", Fake("ns", o), P, True, label="good")
    expect("05", Fake("ns", mutate(o, "deploy", "cache", lambda x: x["metadata"]["labels"].update(tier="backend"))), P, False, label="cache labelled")
    expect("05", Fake("ns", mutate(o, "deploy", "svc-auth", lambda x: x["metadata"].update(generation=2))), P, False, label="spec touched")
    # 06
    env0 = [{"name": "DB_HOST", "value": "postgres-primary"}, {"name": "DB_PORT", "value": "5432"}, {"name": "DB_NAME", "value": "app"}, {"name": "LOG_LEVEL", "value": "info"}]
    P = {"deploy": [dep("worker", env=env0, command=["sh", "-c", "x"]), dep("db")], "rs": [], "svc": [svc("db", {"app": "db"}, 5432)]}
    env1 = [dict(e, value="db") if e["name"] == "DB_HOST" else e for e in env0]
    o = {"deploy": [dep("worker", env=env1, command=["sh", "-c", "x"]), dep("db")], "svc": [svc("db", {"app": "db"}, 5432)], "pod": [pod("worker-2-a", {"app": "worker"})]}
    expect("06", Fake("ns", o, logs={"worker-2-a": "worker starting\nconnecting to db:5432\nconnected\n"}), P, True, label="good")
    expect("06", Fake("ns", o, logs={"worker-2-a": "fatal: cannot reach database\n"}), P, False, label="not connected")
    expect("06", Fake("ns", mutate(o, "deploy", "worker", lambda x: x["spec"]["template"]["spec"]["containers"][0]["env"].append({"name": "X", "value": "1"})), logs={"worker-2-a": "connected\n"}), P, False, label="env drift")
    # 07
    P = {"deploy": [dep("web", 2)], "rs": [obj("web-1")], "svc": [svc("web", {"app": "web"})], "pod": []}
    o = {"deploy": [dep("web", 2)], "rs": [obj("web-1")], "svc": [svc("web", {"app": "web"})], "eps": [eps("web", 2)],
         "pod": [pod("web-1-a", {"app": "web"}, ("ReplicaSet", "web-1")), pod("web-1-b", {"app": "web"}, ("ReplicaSet", "web-1"))]}
    expect("07", Fake("ns", o), P, True, label="good")
    expect("07", Fake("ns", {**o, "pod": o["pod"] + [pod("debug", {"run": "debug"}, meta={"deletionTimestamp": "x"})]}), P, False, label="debug terminating")
    expect("07", Fake("ns", mutate(o, "svc", "web", lambda x: x["metadata"].update(resourceVersion="2"))), P, False, label="service touched")
    # 08
    A = lambda verb, res, name, ns_, code, body=None: {"user": {"username": KMS}, "verb": verb, "stage": "ResponseComplete", "objectRef": {"resource": res, "name": name, "namespace": ns_}, "responseStatus": {"code": code}, "requestObject": body or {}}
    good = [A("create", "configmaps", "ux-web-config", "ns", 201, {"data": {"greeting": "hola"}}), A("create", "deployments", "ux-web-web", "ns", 201, {"spec": {"replicas": 2}})]
    expect("08", Fake("ns", {}), {}, True, ctx={"audit": good}, label="good")
    expect("08", Fake("ns", {}), {}, False, ctx={"audit": [A("create", "configmaps", "ux-web-config", "default", 403), A("create", "deployments", "ux-web-web", "default", 403)]}, label="B1: objects sent to default")
    expect("08", Fake("ns", {"secret": [obj("sh.helm.release.v1.ux-web.v1", labels={"owner": "helm", "name": "ux-web"})]}), {}, False, ctx={"audit": good}, label="release record left")
    # 09
    role = obj("pod-reader", rules=[{"apiGroups": [""], "resources": ["pods"], "verbs": ["get", "list", "watch"]}, {"apiGroups": [""], "resources": ["pods/log"], "verbs": ["get"]}])
    rb = obj("reader-binding", roleRef={"kind": "Role", "name": "pod-reader"}, subjects=[{"kind": "ServiceAccount", "name": "reader", "namespace": "ns"}])
    o = {"sa": [obj("reader")], "role": [role], "rb": [rb], "crb": []}
    allow = {"list pods", "get pods", "watch pods", "get pods/log"}
    expect("09", Fake("ns", o, allow=allow), {}, True, label="good")
    expect("09", Fake("ns", mutate(o, "role", "pod-reader", lambda x: x["rules"][0]["verbs"].append("delete")), allow=allow | {"delete pods"}), {}, False, label="extra verb")
    expect("09", Fake("ns", {**o, "crb": [obj("x", ns=None, subjects=[{"kind": "ServiceAccount", "name": "reader", "namespace": "ns"}])]}, allow=allow), {}, False, label="cluster binding")
    # 10
    keep = {"cm": [obj("keep-cfg-1"), obj("keep-cfg-2")], "svc": [obj("keep-svc")], "deploy": [dep("keep-web", 0)], "sa": [], "job": []}
    P = {k: v + ([obj("s-cfg-1", labels={"owner": "session"})] if k == "cm" else []) for k, v in keep.items()}
    expect("10", Fake("ns", keep), P, True, label="good")
    expect("10", Fake("ns", {**keep, "cm": keep["cm"] + [obj("s-cfg-held", labels={"owner": "session"}, meta={"deletionTimestamp": "x", "finalizers": ["isokron.test/hold"]})]}), P, False, label="finalizer held")
    expect("10", Fake("ns", {**keep, "cm": [obj("keep-cfg-1")]}), P, False, label="kept object deleted")
    # 11
    pr = lambda name, created, reason, status=None: obj(name, meta={"creationTimestamp": created}, spec={"params": [{"name": "target", "value": "api"}, {"name": "sleep-seconds", "value": "60"}], **({"status": status} if status else {})}, status={"conditions": [{"type": "Succeeded", "reason": reason}]})
    P = {"pipeline": [obj("build")], "pr": []}
    o = {"pipeline": [obj("build")], "pr": [pr("build-run-1", "2026-11-12T00:01:00Z", "Cancelled", "Cancelled"), pr("build-run-2", "2026-11-12T00:02:00Z", "Running")]}
    expect("11", Fake("ns", o), P, True, label="good")
    expect("11", Fake("ns", {**o, "pr": o["pr"][:1]}), P, False, label="not restarted")
    expect("11", Fake("ns", {**o, "pr": [o["pr"][1]]}), P, False, label="never cancelled")
    # 12
    expect("12", Fake("ns", {"widget": [obj("ux-w1", spec={"size": 3, "color": "blue"})]}), {}, True, label="good")
    expect("12", Fake("ns", {"widget": [obj("ux-w1", spec={"size": "3", "color": "blue"})]}), {}, False, label="size as string")
    print(json.dumps({"selftest": "fail" if fails else "ok", "cases": n, "failures": fails}, indent=1))
    return 1 if fails else 0


# ---------------------------------------------------------------- CLI

def main(argv):
    def arg(k):
        return argv[argv.index(k) + 1] if k in argv else None
    cmd = argv[0] if argv else ""
    try:
        if cmd == "selftest":
            return selftest()
        job = arg("--job")
        if job not in EVAL:
            raise CheckerError(f"--job must be one of {sorted(EVAL)}")
        if cmd == "pre":
            ns, out = arg("--ns"), arg("--out")
            if not ns or not out:
                raise CheckerError("pre needs --ns and --out")
            snap = {"format": "isokron/native-prestate@1", "job": job, "ns": ns, "objects": pre(Live(ns), job), "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
            os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
            with open(out, "x") as f:
                json.dump(snap, f, indent=1)
            print(json.dumps({"pre": out, "kinds": {k: len(v) for k, v in snap["objects"].items()}}))
            return 0
        if cmd == "post":
            rid = arg("--run-id")
            if not rid or not re.fullmatch(r"nx-\d\d[a-z]?", rid):
                raise CheckerError("post needs --run-id nx-JJ[b]")
            return post(job, rid, arg("--run-dir"))
        raise CheckerError("usage: check.py pre|post|selftest ...")
    except CheckerError as e:
        print(json.dumps({"checker_error": str(e)}))
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
