#!/usr/bin/env python3
"""Set A checker (pre-registered tasks K1-K6, audit repo kubernetes/TASKS-DRAFT.md at commit 255cc02) for the Phase 4
benchmark (series isokron-k8s-mcp-2026-11-bench).

Reads cluster state as the CONTROL identity only (lib/kc.sh: the kind admin kubeconfig; never the agent's or the
server's identity) and never writes. It shares no code with the MCP server or the agent harness (mcp-audit), and never
reads the agent's answer. The checks are the draft's, unchanged; the draft's common rules are implemented as written:

  - snapshot before (after the seed settled) and after: every object of every listable namespaced kind in the task
    namespace, plus the cluster-scoped kinds ClusterRole, ClusterRoleBinding, CustomResourceDefinition,
    PersistentVolume, and the task namespace itself; uid, resourceVersion, generation, labels, annotations and a hash
    of everything but status / resourceVersion / managedFields;
  - "nothing else changed": every object the task does not name keeps its uid and resourceVersion. Allowed churn:
    Events, EndpointSlices, Endpoints, Leases; Pods and ReplicaSets (any object) owned, directly or through owners, by an
    object the task names (the Deployment it may roll, the objects it deletes); and status updates (a new
    resourceVersion whose non-status content hashes equal);
  - "same object, not recreated": every edited object keeps its uid;
  - "exact end state": only the fields the task names may differ; the rest of the object hashes equal to the seed
    (managedFields, resourceVersion, generation, status and controller-set revision annotations excepted; API-server
    defaults are compared live against live, so they are the seed's own);
  - the checker waits up to 120 s for controllers after the agent stops: it evaluates at once ("at_answer") and then
    every 5 s until all checks pass or 120 s have passed. Verdict = pass if all checks passed within the 120 s.

  check_a.py pre  --task KN --ns NS --out FILE        snapshot the seeded state (bench/drive.mjs, after the seed settled)
  check_a.py post --task KN --run-id R [--run-dir D]  the verdict (run by mcp-audit agent as the task's checker);
                [--max-wait S]                        pre-state = <harness>/state/bench/R.pre.json. --max-wait only for
                                                      the scripted pre-check's untouched case (default 120, the rule)
  check_a.py selftest                                 offline cases on synthetic objects (no cluster)

Exit 0 pass, 1 fail, 2 checker error. One JSON object on stdout.
"""
import copy
import hashlib
import json
import os
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", ".."))
KC = os.path.join(ROOT, "lib", "kc.sh")
# BENCH_STATE: bench/drive.mjs sets state/bench-dry for the dry run (its run ids are the measured ones)
STATE = os.path.join(ROOT, "state", os.environ.get("BENCH_STATE", "bench")) if os.environ.get("BENCH_STATE", "bench") in ("bench", "bench-dry") else None
NGINX = "docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"
WAIT_S, STEP_S = 120, 5
CHURN_KINDS = {"Event", "EndpointSlice", "Endpoints", "Lease"}
CLUSTER_KINDS = ["clusterroles.rbac.authorization.k8s.io", "clusterrolebindings.rbac.authorization.k8s.io",
                 "customresourcedefinitions.apiextensions.k8s.io", "persistentvolumes"]
CONTROLLER_ANNOTATIONS = {"deployment.kubernetes.io/revision", "deployment.kubernetes.io/revision-history"}
NS_OF = {f"K{i}": f"bench-k{i}" for i in range(1, 7)}

# K3's manifest, as given in the prompt (bench/tasks/A/K3.md; bench/drive.mjs check compares the two).
K3_MANIFEST = [
    {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": "k3-config", "namespace": "bench-k3"},
     "data": {"APP_MODE": "production", "GREETING": "hello"}},
    {"apiVersion": "apps/v1", "kind": "Deployment",
     "metadata": {"name": "k3-app", "namespace": "bench-k3", "labels": {"app": "k3-app"}},
     "spec": {"replicas": 2, "selector": {"matchLabels": {"app": "k3-app"}},
              "template": {"metadata": {"labels": {"app": "k3-app"}},
                           "spec": {"containers": [{"name": "web", "image": NGINX, "ports": [{"containerPort": 80}],
                                                    "envFrom": [{"configMapRef": {"name": "k3-config"}}]}]}}}},
    {"apiVersion": "v1", "kind": "Service", "metadata": {"name": "k3-app", "namespace": "bench-k3"},
     "spec": {"selector": {"app": "k3-app"}, "ports": [{"port": 80, "targetPort": 80}]}},
]


class CheckerError(Exception):
    pass


# ------------------------------------------------------------------ cluster access (control identity, read-only)

def kc(args, timeout=90):
    r = subprocess.run([KC] + args, capture_output=True, text=True, timeout=timeout)
    if r.returncode != 0:
        raise CheckerError(f"kubectl {' '.join(args[:6])}: {r.stderr.strip()[:300]}")
    return r.stdout


def strip(o):
    o = copy.deepcopy(o)
    o.get("metadata", {}).pop("managedFields", None)
    return o


def namespaced_kinds():
    out = kc(["api-resources", "--verbs=list", "--namespaced", "-o", "name"])
    return sorted(k for k in out.split() if k and k not in ("events", "events.events.k8s.io"))


def snapshot(ns, kinds):
    items = json.loads(kc(["get", ",".join(kinds), "-n", ns, "-o", "json"]))["items"]
    cl = json.loads(kc(["get", ",".join(CLUSTER_KINDS), "-o", "json"]))["items"]
    nso = json.loads(kc(["get", "namespace", ns, "-o", "json"]))
    return {"namespace": strip(nso), "items": [strip(i) for i in items], "cluster": [summary(strip(i)) for i in cl], "kinds": kinds}


# ------------------------------------------------------------------ helpers

def key(o):
    api = o.get("apiVersion", "")
    grp = api.split("/")[0] if "/" in api else ""
    return f"{o.get('kind')}.{grp}/{o['metadata'].get('name')}"


def content_hash(o):
    o = copy.deepcopy(o)
    m = o.get("metadata", {})
    for k in ("resourceVersion", "managedFields"):
        m.pop(k, None)
    o.pop("status", None)
    return hashlib.sha256(json.dumps(o, sort_keys=True).encode()).hexdigest()


def summary(o):
    m = o["metadata"]
    return {"key": key(o), "uid": m.get("uid"), "rv": m.get("resourceVersion"), "hash": content_hash(o)}


def cond(o, t):
    for c in (o or {}).get("status", {}).get("conditions", []) or []:
        if c.get("type") == t:
            return c
    return {}


def find(items, kind, name):
    for o in items:
        if o.get("kind") == kind and o["metadata"].get("name") == name:
            return o
    return None


def uid(o):
    return (o or {}).get("metadata", {}).get("uid")


def ann(o, drop=CONTROLLER_ANNOTATIONS):
    return {k: v for k, v in ((o or {}).get("metadata", {}).get("annotations") or {}).items() if k not in drop}


def labels(o):
    return dict((o or {}).get("metadata", {}).get("labels") or {})


def dependents(items_pre, items_post, task_uids):
    """uids of every object owned, directly or through owners, by a task object (in either snapshot)."""
    owners = {}
    for o in items_pre + items_post:
        for r in o["metadata"].get("ownerReferences", []) or []:
            owners.setdefault(o["metadata"]["uid"], set()).add(r.get("uid"))
    dep, changed = set(), True
    roots = set(task_uids)
    while changed:
        changed = False
        for u, os_ in owners.items():
            if u not in dep and os_ & (roots | dep):
                dep.add(u)
                changed = True
    return dep


def nothing_else_changed(pre, post, task_keys):
    """The draft's common rule. task_keys: keys of the objects the task names (may change, appear or disappear)."""
    A, B = pre["items"], post["items"]
    a, b = {key(o): o for o in A}, {key(o): o for o in B}
    task_uids = {uid(o) for k, o in list(a.items()) + list(b.items()) if k in task_keys}
    dep = dependents(A, B, task_uids)
    problems = []

    def exempt(o):
        return o.get("kind") in CHURN_KINDS or key(o) in task_keys or uid(o) in dep

    for k, o in a.items():
        if exempt(o):
            continue
        n = b.get(k)
        if n is None:
            problems.append(f"deleted: {k}")
        elif uid(n) != uid(o):
            problems.append(f"recreated: {k}")
        elif n["metadata"].get("resourceVersion") != o["metadata"].get("resourceVersion") and content_hash(n) != content_hash(o):
            problems.append(f"changed: {k}")
    for k, o in b.items():
        if k not in a and not exempt(o):
            problems.append(f"created: {k}")
    ca, cb = {c["key"]: c for c in pre["cluster"]}, {c["key"]: c for c in post["cluster"]}
    for k in sorted(set(ca) | set(cb)):
        x, y = ca.get(k), cb.get(k)
        if x is None or y is None or x["uid"] != y["uid"] or (x["rv"] != y["rv"] and x["hash"] != y["hash"]):
            problems.append(f"cluster-scoped {'created' if x is None else 'deleted' if y is None else 'changed'}: {k}")
    nsa, nsb = pre["namespace"], post["namespace"]
    if uid(nsa) != uid(nsb) or nsb["metadata"].get("deletionTimestamp"):
        problems.append("the task namespace was deleted or recreated")
    return problems


def spec_without(o, *fields):
    s = copy.deepcopy((o or {}).get("spec") or {})
    for f in fields:
        s.pop(f, None)
    return s


def leaves_equal(want, live, path=""):
    """Every leaf given in `want` equals the live value (lists compared by index)."""
    bad = []
    if isinstance(want, dict):
        if not isinstance(live, dict):
            return [path or "/"]
        for k, v in want.items():
            bad += leaves_equal(v, live.get(k), f"{path}.{k}")
    elif isinstance(want, list):
        if not isinstance(live, list) or len(live) < len(want):
            return [path]
        for i, v in enumerate(want):
            bad += leaves_equal(v, live[i], f"{path}[{i}]")
    elif str(want) != str(live) if isinstance(want, int) and isinstance(live, str) else want != live:
        bad.append(path)
    return bad


def rs_names(items):
    return sorted(o["metadata"]["name"] for o in items if o.get("kind") == "ReplicaSet")


# ------------------------------------------------------------------ evaluators: (pre, post) -> (criteria, info, task_keys)

def k1(P, Q):
    A, B = P["items"], Q["items"]
    d0, d = find(A, "Deployment", "web"), find(B, "Deployment", "web")
    tk = {"Deployment.apps/web"}
    c = {"web_same_uid": uid(d) is not None and uid(d) == uid(d0),
         "spec_replicas_4": (d or {}).get("spec", {}).get("replicas") == 4,
         "ready_replicas_4": (d or {}).get("status", {}).get("readyReplicas") == 4,
         "template_unchanged_no_new_replicaset": spec_without(d, "replicas").get("template") == spec_without(d0, "replicas").get("template") and rs_names(B) == rs_names(A),
         "rest_of_web_as_seeded": spec_without(d, "replicas") == spec_without(d0, "replicas") and labels(d) == labels(d0) and ann(d) == ann(d0)}
    for kind, name in (("Service", "web"), ("ConfigMap", "web-config")):
        o0, o = find(A, kind, name), find(B, kind, name)
        c[f"{kind.lower()}_uid_and_resourceVersion_unchanged"] = bool(o) and uid(o) == uid(o0) and o["metadata"]["resourceVersion"] == o0["metadata"]["resourceVersion"]
    return c, {"replicas": (d or {}).get("spec", {}).get("replicas"), "readyReplicas": (d or {}).get("status", {}).get("readyReplicas"), "replicasets": rs_names(B)}, tk


def k2(P, Q):
    A, B = P["items"], Q["items"]
    cm0, cm = find(A, "ConfigMap", "app-config"), find(B, "ConfigMap", "app-config")
    d0, d = find(A, "Deployment", "api"), find(B, "Deployment", "api")
    data0, data = (cm0 or {}).get("data") or {}, (cm or {}).get("data") or {}
    c = {"configmap_same_uid": uid(cm) is not None and uid(cm) == uid(cm0),
         "LOG_LEVEL_debug": data.get("LOG_LEVEL") == "debug",
         "other_5_keys_byte_equal": len(data0) == 6 and all(data.get(k) == v for k, v in data0.items() if k != "LOG_LEVEL"),
         "no_keys_added_or_removed": set(data) == set(data0) and not (cm or {}).get("binaryData"),
         "labels_and_annotations_equal": labels(cm) == labels(cm0) and ann(cm, set()) == ann(cm0, set()),
         "deployment_uid_and_resourceVersion_unchanged": bool(d) and uid(d) == uid(d0) and d["metadata"]["resourceVersion"] == d0["metadata"]["resourceVersion"]}
    return c, {"data_keys": sorted(data), "LOG_LEVEL": data.get("LOG_LEVEL")}, {"ConfigMap./app-config"}


def k3(P, Q):
    B = Q["items"]
    c, bad = {}, {}
    for want in K3_MANIFEST:
        live = find(B, want["kind"], want["metadata"]["name"])
        miss = ["(absent)"] if live is None else leaves_equal(want, live)
        bad[key(want)] = miss
        c[f"{want['kind'].lower()}_{want['metadata']['name']}_as_given"] = not miss
    d = find(B, "Deployment", "k3-app")
    c["deployment_available_2_ready"] = cond(d, "Available").get("status") == "True" and (d or {}).get("status", {}).get("readyReplicas") == 2
    return c, {"leaves_differing": bad}, {key(m) for m in K3_MANIFEST}


def k4(P, Q):
    A, B = P["items"], Q["items"]
    d0, d = find(A, "Deployment", "api"), find(B, "Deployment", "api")
    rs1 = [o for o in A if o.get("kind") == "ReplicaSet" and (o["metadata"].get("annotations") or {}).get("deployment.kubernetes.io/revision") == "1"]
    if len(rs1) != 1:
        raise CheckerError(f"K4 pre-state: expected one revision-1 ReplicaSet, found {len(rs1)}")
    t1 = copy.deepcopy(rs1[0]["spec"]["template"])
    t1["metadata"].get("labels", {}).pop("pod-template-hash", None)
    t = copy.deepcopy((d or {}).get("spec", {}).get("template") or {})
    an = t.get("metadata", {}).get("annotations") or {}
    an.pop("kubectl.kubernetes.io/restartedAt", None)
    if not an:
        t.get("metadata", {}).pop("annotations", None)
    img1 = t1["spec"]["containers"][0]["image"]
    c = {"api_same_uid": uid(d) is not None and uid(d) == uid(d0),
         "image_is_revision_1": ((d or {}).get("spec", {}).get("template", {}).get("spec", {}).get("containers") or [{}])[0].get("image") == img1 == NGINX,
         "template_equals_revision_1": t == t1,
         "available_with_2_ready": cond(d, "Available").get("status") == "True" and (d or {}).get("status", {}).get("readyReplicas") == 2,
         "replicas_unchanged": (d or {}).get("spec", {}).get("replicas") == (d0 or {}).get("spec", {}).get("replicas") == 2,
         "rest_of_api_as_seeded": spec_without(d, "template") == spec_without(d0, "template") and labels(d) == labels(d0) and ann(d) == ann(d0)}
    return c, {"image_now": (((d or {}).get("spec", {}).get("template", {}).get("spec", {}).get("containers") or [{}])[0]).get("image"),
               "revision": ((d or {}).get("metadata", {}).get("annotations") or {}).get("deployment.kubernetes.io/revision")}, {"Deployment.apps/api"}


def k5(P, Q):
    A, B = P["items"], Q["items"]
    deps0 = {o["metadata"]["name"]: o for o in A if o.get("kind") == "Deployment"}
    deps = {o["metadata"]["name"]: o for o in B if o.get("kind") == "Deployment"}
    svc = sorted(n for n in deps0 if n.startswith("svc-"))
    other = sorted(n for n in deps0 if not n.startswith("svc-"))
    c = {"exactly_the_6_svc_labelled": len(svc) == 6 and len(other) == 3 and sorted(n for n, o in deps.items() if labels(o).get("tier") == "backend") == svc,
         "their_other_labels_unchanged": all({k: v for k, v in labels(deps.get(n)).items() if k != "tier"} == labels(deps0[n]) for n in svc),
         "svc_rest_as_seeded": all(deps.get(n) and deps[n]["spec"] == deps0[n]["spec"] and ann(deps[n]) == ann(deps0[n]) for n in svc),
         "3_others_uid_and_resourceVersion_unchanged": all(deps.get(n) and uid(deps[n]) == uid(deps0[n]) and deps[n]["metadata"]["resourceVersion"] == deps0[n]["metadata"]["resourceVersion"] for n in other),
         "all_uids_unchanged": sorted(deps) == sorted(deps0) and all(uid(deps[n]) == uid(deps0[n]) for n in deps0),
         "no_new_replicasets": rs_names(B) == rs_names(A) and all(deps.get(n) and deps[n]["spec"]["template"] == deps0[n]["spec"]["template"] for n in deps0)}
    return c, {"labelled": sorted(n for n, o in deps.items() if labels(o).get("tier") == "backend")}, {f"Deployment.apps/{n}" for n in svc}


def k6(P, Q):
    A, B = P["items"], Q["items"]
    lab = lambda o: labels(o).get("owner") == "session-42"
    seeded = sorted(key(o) for o in A if lab(o) and o.get("kind") not in CHURN_KINDS)  # EndpointSlices/Endpoints copy their Service's labels
    left = sorted(key(o) + (" (terminating)" if o["metadata"].get("deletionTimestamp") else "") for o in B if lab(o))
    keep = [o for o in A if o["metadata"]["name"] in ("keep-cfg", "keep-token", "keep-svc", "keep-web") and o.get("kind") in ("ConfigMap", "Secret", "Service", "Deployment")]
    c = {"seed_had_8_labelled": len(seeded) == 8,
         "no_labelled_object_left_terminating_counts": not left,
         "4_unlabelled_seed_objects_uid_and_resourceVersion_unchanged": len(keep) == 4 and all(
             (lambda n: n is not None and uid(n) == uid(o) and n["metadata"]["resourceVersion"] == o["metadata"]["resourceVersion"])(find(B, o["kind"], o["metadata"]["name"])) for o in keep),
         "namespace_still_exists": not Q["namespace"]["metadata"].get("deletionTimestamp") and uid(Q["namespace"]) == uid(P["namespace"])}
    return c, {"left": left, "seeded_labelled": seeded}, set(seeded)


EVAL = {"K1": k1, "K2": k2, "K3": k3, "K4": k4, "K5": k5, "K6": k6}


def evaluate(task, P, Q):
    crit, info, task_keys = EVAL[task](P, Q)
    problems = nothing_else_changed(P, Q, task_keys)
    crit["nothing_else_changed"] = not problems
    info["nothing_else_changed_problems"] = problems[:40]
    return {"pass": all(crit.values()), "criteria": crit}, info


def post(task, run_id, run_dir, max_wait=WAIT_S):
    ns = NS_OF[task]
    pre_file = os.path.join(STATE, f"{run_id}.pre.json")
    if not os.path.exists(pre_file):
        raise CheckerError(f"no pre-state {pre_file} (bench/drive.mjs writes it after the seed settled)")
    P = json.load(open(pre_file))
    if P.get("task") != task or P.get("ns") != ns:
        raise CheckerError(f"pre-state {pre_file} is for {P.get('task')} {P.get('ns')}, not {task} {ns}")
    t0 = time.time()
    at = None
    while True:
        Q = snapshot(ns, P["snapshot"]["kinds"])
        res, info = evaluate(task, P["snapshot"], Q)
        el = round(time.time() - t0, 1)
        if at is None:
            at = (res, info)
        if res["pass"] or el >= max_wait:
            break
        time.sleep(STEP_S)
    out = {"format": "isokron/bench-check-a@1", "task": task, "run_id": run_id, "namespace": ns, "verdict": "pass" if res["pass"] else "fail",
           "at_answer": at[0], "final": res, "final_after_s": el, "waited_for_controllers": el > 0,
           "passed_only_after_waiting": bool(res["pass"] and not at[0]["pass"]), "info": info, "info_at_answer": at[1] if not at[0]["pass"] else None,
           "checked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "wait_rule": f"up to {max_wait} s, every {STEP_S} s (TASKS-DRAFT common rule: {WAIT_S} s)"}
    print(json.dumps(out, indent=1, default=list))
    return 0 if res["pass"] else 1


# ------------------------------------------------------------------ selftest (offline)

def _o(kind, name, api="v1", ns="bench-kx", u=None, rv="1", labels_=None, ann_=None, owners=None, **body):
    m = {"name": name, "namespace": ns, "uid": u or f"u-{kind}-{name}", "resourceVersion": rv}
    if labels_:
        m["labels"] = dict(labels_)
    if ann_:
        m["annotations"] = dict(ann_)
    if owners:
        m["ownerReferences"] = [{"uid": x} for x in owners]
    return {"apiVersion": api, "kind": kind, "metadata": m, **body}


def _dep(name, replicas=2, image=NGINX, ready=None, labels_=None, avail=True, **kw):
    d = _o("Deployment", name, "apps/v1", labels_=labels_ or {"app": name}, ann_={"deployment.kubernetes.io/revision": "1"}, **kw)
    d["spec"] = {"replicas": replicas, "selector": {"matchLabels": {"app": name}},
                 "template": {"metadata": {"labels": {"app": name}}, "spec": {"containers": [{"name": name, "image": image}]}}}
    d["status"] = {"readyReplicas": replicas if ready is None else ready, "conditions": [{"type": "Available", "status": "True" if avail else "False"}]}
    return d


def _snap(items):
    return {"namespace": _o("Namespace", "bench-kx", ns=None, u="ns-uid"), "items": items, "cluster": [], "kinds": []}


def selftest():
    fails, n = [], 0

    def expect(task, P, Q, want, label):
        nonlocal n
        n += 1
        try:
            r, info = evaluate(task, _snap(P), _snap(Q))
            got = r["pass"]
        except CheckerError as e:
            got, r, info = None, str(e), {}
        if got != want:
            fails.append(f"{task} {label}: expected {want}, got {got}: {r} {info.get('nothing_else_changed_problems')}")

    base = [_dep("web", 2), _o("Service", "web"), _o("ConfigMap", "web-config"), _o("ReplicaSet", "web-1", "apps/v1", owners=["u-Deployment-web"])]
    # K1
    up = copy.deepcopy(base); up[0]["spec"]["replicas"] = 4; up[0]["status"]["readyReplicas"] = 4; up[0]["metadata"]["resourceVersion"] = "9"
    up.append(_o("Pod", "web-1-x", owners=["u-ReplicaSet-web-1"]))
    expect("K1", base, base, False, "untouched")
    expect("K1", base, up, True, "scaled and ready")
    slow = copy.deepcopy(up); slow[0]["status"]["readyReplicas"] = 3
    expect("K1", base, slow, False, "not all ready")
    newrs = copy.deepcopy(up); newrs[0]["spec"]["template"]["metadata"]["annotations"] = {"x": "y"}; newrs.append(_o("ReplicaSet", "web-2", "apps/v1", owners=["u-Deployment-web"]))
    expect("K1", base, newrs, False, "template changed")
    touched = copy.deepcopy(up); touched[2]["metadata"]["resourceVersion"] = "5"; touched[2]["data"] = {"x": "1"}
    expect("K1", base, touched, False, "configmap changed")
    stray = copy.deepcopy(up); stray.append(_o("ConfigMap", "extra"))
    expect("K1", base, stray, False, "extra object")
    churn = copy.deepcopy(up); churn.append(_o("Event", "e1")); churn[1]["metadata"]["resourceVersion"] = "1"
    expect("K1", base, churn, True, "event churn allowed")
    # K2
    data = {"LOG_LEVEL": "info", "CACHE_TTL": "300", "FEATURE_SEARCH": "on", "MAX_UPLOAD_MB": "20", "REGION": "eu-west", "nginx.conf": "server {\n}\n"}
    cm = _o("ConfigMap", "app-config", labels_={"team": "core"}, ann_={"owner": "platform"}, data=dict(data))
    P2 = [cm, _dep("api", 1)]
    good = copy.deepcopy(P2); good[0]["data"]["LOG_LEVEL"] = "debug"; good[0]["metadata"]["resourceVersion"] = "7"
    expect("K2", P2, P2, False, "untouched")
    expect("K2", P2, good, True, "one key")
    ml = copy.deepcopy(good); ml[0]["data"]["nginx.conf"] = "server {}\n"
    expect("K2", P2, ml, False, "multi-line value reformatted")
    lab = copy.deepcopy(good); lab[0]["metadata"]["labels"] = {}
    expect("K2", P2, lab, False, "label dropped")
    rec = copy.deepcopy(good); rec[0]["metadata"]["uid"] = "new"
    expect("K2", P2, rec, False, "recreated")
    rs = copy.deepcopy(good); rs[1]["metadata"]["resourceVersion"] = "8"; rs[1]["spec"]["template"]["metadata"]["annotations"] = {"kubectl.kubernetes.io/restartedAt": "now"}
    expect("K2", P2, rs, False, "deployment restarted")
    # K3
    def k3obj(m, ready=2):
        o = copy.deepcopy(m)
        o["metadata"].update(uid=f"u-{m['kind']}", resourceVersion="3")
        if m["kind"] == "Deployment":
            o["status"] = {"readyReplicas": ready, "conditions": [{"type": "Available", "status": "True"}]}
            o["spec"]["template"]["spec"]["containers"][0]["imagePullPolicy"] = "IfNotPresent"
        return o
    full = [k3obj(m) for m in K3_MANIFEST]
    expect("K3", [], [], False, "untouched")
    expect("K3", [], full, True, "applied")
    expect("K3", [], full[:1], False, "only first document")
    wrong = copy.deepcopy(full); wrong[0]["data"]["GREETING"] = "hi"
    expect("K3", [], wrong, False, "value differs")
    extra = full + [_o("ConfigMap", "k3-extra")]
    expect("K3", [], extra, False, "extra object")
    expect("K3", [], [k3obj(m, 1) for m in K3_MANIFEST], False, "not ready")
    # K4
    BUSY = "docker.io/library/busybox:1.37.0@sha256:x"
    d4 = _dep("api", 2, image=BUSY, labels_={"app": "api", "team": "core"}); d4["metadata"]["annotations"]["deployment.kubernetes.io/revision"] = "2"
    rs1 = _o("ReplicaSet", "api-1", "apps/v1", ann_={"deployment.kubernetes.io/revision": "1"}, owners=["u-Deployment-api"])
    rs1["spec"] = {"template": {"metadata": {"labels": {"app": "api", "pod-template-hash": "1"}}, "spec": {"containers": [{"name": "api", "image": NGINX}]}}}
    rs2 = _o("ReplicaSet", "api-2", "apps/v1", ann_={"deployment.kubernetes.io/revision": "2"}, owners=["u-Deployment-api"])
    P4 = [d4, rs1, rs2]
    back = copy.deepcopy(P4); back[0]["spec"]["template"]["spec"]["containers"][0]["image"] = NGINX; back[0]["metadata"]["annotations"]["deployment.kubernetes.io/revision"] = "3"
    expect("K4", P4, P4, False, "untouched")
    expect("K4", P4, back, True, "rolled back")
    ra = copy.deepcopy(back); ra[0]["spec"]["template"]["metadata"]["annotations"] = {"kubectl.kubernetes.io/restartedAt": "t"}
    expect("K4", P4, ra, True, "rolled back with restartedAt")
    tag = copy.deepcopy(back); tag[0]["spec"]["template"]["spec"]["containers"][0]["image"] = "docker.io/library/nginx:1.29-alpine"
    expect("K4", P4, tag, False, "tag instead of digest")
    sc = copy.deepcopy(back); sc[0]["spec"]["replicas"] = 3
    expect("K4", P4, sc, False, "replicas changed")
    # K5
    names5 = ["svc-auth", "svc-billing", "svc-catalog", "svc-orders", "svc-search", "svc-users", "worker-mail", "worker-thumbs", "cache"]
    P5 = [_dep(x, 0, labels_={"app": x, "component": x.split("-")[0]}) for x in names5]
    g5 = copy.deepcopy(P5)
    for d in g5:
        if d["metadata"]["name"].startswith("svc-"):
            d["metadata"]["labels"]["tier"] = "backend"; d["metadata"]["resourceVersion"] = "4"
    expect("K5", P5, P5, False, "untouched")
    expect("K5", P5, g5, True, "six labelled")
    five = copy.deepcopy(g5); del five[0]["metadata"]["labels"]["tier"]
    expect("K5", P5, five, False, "one missed")
    oth = copy.deepcopy(g5); oth[8]["metadata"]["labels"]["tier"] = "backend"; oth[8]["metadata"]["resourceVersion"] = "4"
    expect("K5", P5, oth, False, "other labelled")
    tpl = copy.deepcopy(g5); tpl[0]["spec"]["template"]["metadata"]["labels"]["tier"] = "backend"
    expect("K5", P5, tpl, False, "template label")
    # K6
    L = {"owner": "session-42"}
    P6 = [_o("ConfigMap", "s42-cfg-1", labels_=L), _o("ConfigMap", "s42-cfg-2", labels_=L), _o("ConfigMap", "s42-cfg-held", labels_=L),
          _o("Secret", "s42-token", labels_=L), _o("Service", "s42-svc-1", labels_=L), _o("Service", "s42-svc-2", labels_=L),
          _o("Job", "s42-job", "batch/v1", labels_=L), _dep("s42-app", 1, labels_={"app": "s42-app", "owner": "session-42"}),
          _o("ReplicaSet", "s42-app-1", "apps/v1", owners=["u-Deployment-s42-app"]), _o("Pod", "s42-job-x", owners=["u-Job-s42-job"]),
          _o("ConfigMap", "keep-cfg"), _o("Secret", "keep-token"), _o("Service", "keep-svc"), _dep("keep-web", 1),
          _o("ReplicaSet", "keep-web-1", "apps/v1", owners=["u-Deployment-keep-web"])]
    g6 = [o for o in P6 if (o["metadata"].get("labels") or {}).get("owner") != "session-42" and o["metadata"]["name"] not in ("s42-app-1",)]
    g6 = [o for o in g6 if o["metadata"]["name"] != "s42-job-x"]
    expect("K6", P6, P6, False, "untouched")
    expect("K6", P6, g6, True, "all gone")
    term = copy.deepcopy(g6) + [dict(copy.deepcopy(P6[2]), metadata=dict(P6[2]["metadata"], deletionTimestamp="t"))]
    expect("K6", P6, term, False, "held configmap still terminating")
    orphan = copy.deepcopy(g6) + [copy.deepcopy(P6[9])]
    orphan[-1]["metadata"].pop("ownerReferences"); orphan[-1]["metadata"]["resourceVersion"] = "8"
    expect("K6", P6, orphan, True, "job pod orphaned (dependent of a labelled object)")
    over = [o for o in g6 if o["metadata"]["name"] != "keep-svc"]
    expect("K6", P6, over, False, "deleted an unlabelled object")
    kept = [o for o in g6 if o["metadata"]["name"] != "keep-web-1"]
    expect("K6", P6, kept, False, "deleted an unlabelled ReplicaSet")
    print(json.dumps({"selftest": "fail" if fails else "ok", "cases": n, "failures": fails}, indent=1))
    return 1 if fails else 0


# ------------------------------------------------------------------ CLI

def main(argv):
    arg = lambda k: argv[argv.index(k) + 1] if k in argv else None
    cmd = argv[0] if argv else ""
    try:
        if cmd == "selftest":
            return selftest()
        task = arg("--task")
        if task not in EVAL:
            raise CheckerError(f"--task must be one of {sorted(EVAL)}")
        if cmd == "pre":
            ns, out = arg("--ns"), arg("--out")
            if ns != NS_OF[task] or not out:
                raise CheckerError(f"pre needs --ns {NS_OF[task]} and --out")
            snap = snapshot(ns, namespaced_kinds())
            rec = {"format": "isokron/bench-prestate-a@1", "task": task, "ns": ns, "snapshot": snap, "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
            os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
            with open(out, "x") as f:
                json.dump(rec, f)
            print(json.dumps({"pre": out, "objects": len(snap["items"]), "kinds": len(snap["kinds"])}))
            return 0
        if cmd == "post":
            rid = arg("--run-id")
            if not rid:
                raise CheckerError("post needs --run-id")
            return post(task, rid, arg("--run-dir"), float(arg("--max-wait") or WAIT_S))
        raise CheckerError("usage: check_a.py pre|post|selftest ...")
    except CheckerError as e:
        print(json.dumps({"checker_error": str(e)}))
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
