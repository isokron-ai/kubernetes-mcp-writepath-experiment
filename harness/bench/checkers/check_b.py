#!/usr/bin/env python3
"""Set B checker (friction-derived F1-F9) for the Phase 4 benchmark (series isokron-k8s-mcp-2026-11-bench).

The native sessions' done-checks, reused: the evaluators are imported unchanged from usability/native/check.py (jobs
01, 07, 08, 10) and usability/native2/check2.py (jobs 13-17), and run with the same contract as there: control identity,
read-only, criteria at the agent's answer and, when those fail and the job waits on a controller, again after 45 s
("settled", information only); verdict = at answer; exit 0 pass, 1 fail, 2 checker error. Only the paths differ
(namespace mcp-<run id>, pre-state and hook records under state/bench/).

Changes made by the scripted pre-check (a seeded-but-untouched state must fail; bench/precheck.py), labelled in the
output as `bench_fix`:
  - F2 (job 07): the native check passes an untouched namespace (no debug pod is also what a finished job looks like).
    Added `debug_pod_was_created`: the run's API audit log (kube-audit.jsonl) shows the server identity creating a Pod
    in the namespace (201).
  - F5 (job 13): the native check accepts "nothing in place" as consistent, so an untouched namespace passes. Added
    `the_request_was_attempted`: the run's API audit log shows at least one create/update/patch by the server identity
    in the namespace (any response code, dry runs included), so "nothing" passes only after a refusal or a rollback.
F1b is new in set B (K3's planned twin): evaluator `f1b` below.

  check_b.py pre  --task T --ns NS --out FILE
  check_b.py post --task T --run-id R --run-dir DIR [--answer FILE] [--settle S]   (--settle: the pre-check's
                                                                                   untouched case only; default 45)
  check_b.py selftest
"""
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(ROOT, "usability", "native"))
sys.path.insert(0, os.path.join(ROOT, "usability", "native2"))
import check as C  # noqa: E402  usability/native/check.py, unchanged
import check2 as C2  # noqa: E402  usability/native2/check2.py, unchanged

# BENCH_STATE: bench/drive.mjs sets state/bench-dry for the dry run (its run ids are the measured ones)
STATE = os.path.join(ROOT, "state", os.environ.get("BENCH_STATE", "bench")) if os.environ.get("BENCH_STATE", "bench") in ("bench", "bench-dry") else None
KMS = C.KMS
NGINX = C.NGINX
# task -> (module, native job)
MAP = {"F1a": (C, "01"), "F2": (C, "07"), "F3": (C, "08"), "F4": (C, "10"),
       "F5": (C2, "13"), "F6": (C2, "14"), "F7": (C2, "15"), "F8": (C2, "16"), "F9": (C2, "17")}
TASKS = sorted(list(MAP) + ["F1b"])

F1B_DATA = {"APP_MODE": "production", "GREETING": "hello"}


def kms_writes(lines, ns, verbs=("create", "update", "patch"), resource=None):
    out = []
    for l in lines or []:
        o = l.get("objectRef") or {}
        if (l.get("user") or {}).get("username") != KMS or l.get("verb") not in verbs or o.get("namespace") != ns:
            continue
        if resource and (o.get("resource") != resource or o.get("subresource")):
            continue
        if l.get("stage", "ResponseComplete") != "ResponseComplete":
            continue
        out.append(l)
    return out


def f1b(cl, P, ctx):
    ns = cl.ns
    cm, d, s, w = cl.get("cm", "f1-config"), cl.get("deploy", "f1-app"), cl.get("svc", "f1-app"), cl.get("deploy", "f1-worker")
    ct = C.container(d)
    c = {"configmap_as_given": bool(cm) and (cm.get("data") or {}) == F1B_DATA,
         "deployment_as_given": bool(d) and d["spec"].get("replicas") == 2 and ct.get("name") == "web" and ct.get("image") == NGINX
         and any(p.get("containerPort") == 80 for p in ct.get("ports") or []) and any((e.get("configMapRef") or {}).get("name") == "f1-config" for e in ct.get("envFrom") or [])
         and (d["metadata"].get("labels") or {}).get("app") == "f1-app" and (d["spec"].get("template", {}).get("metadata", {}).get("labels") or {}).get("app") == "f1-app",
         "deployment_available_2_ready": C.cond(d, "Available").get("status") == "True" and (d or {}).get("status", {}).get("readyReplicas") == 2,
         "service_as_given": bool(s) and (s["spec"].get("selector") or {}) == {"app": "f1-app"} and any(p.get("port") == 80 and str(p.get("targetPort")) == "80" for p in s["spec"].get("ports", [])),
         "service_2_ready_endpoints": C.ready_endpoints(cl, "f1-app") == 2,
         "rejected_document_absent": w is None}
    extra = {"cm": [n for n in C.names(cl.list("cm")) if n not in ("f1-config", "kube-root-ca.crt")],
             "deploy": [n for n in C.names(cl.list("deploy")) if n != "f1-app"], "svc": [n for n in C.names(cl.list("svc")) if n != "f1-app"]}
    c["nothing_else_created"] = not any(extra.values())
    ans = (ctx.get("answer") or "")
    return c, {"extra": extra, "answer_names_f1_worker": "f1-worker" in ans, "claim": ans.strip()[-800:]}


def evaluate(task, cl, P, ctx):
    if task == "F1b":
        crit, info = f1b(cl, P, ctx)
    else:
        mod, job = MAP[task]
        crit, info = mod.EVAL[job](cl, P, ctx)
    if task == "F2":
        crit["debug_pod_was_created (bench_fix)"] = any((l.get("responseStatus") or {}).get("code") == 201 for l in kms_writes(ctx.get("audit"), cl.ns, ("create",), "pods"))
    if task == "F5":
        crit["the_request_was_attempted (bench_fix)"] = bool(kms_writes(ctx.get("audit"), cl.ns))
    return {"pass": all(crit.values()), "criteria": crit}, info


SETTLES = {"F1a", "F1b", "F2", "F4", "F5", "F6", "F9"}  # the native SETTLES of jobs 01, 07, 10, 13, 14, 17, plus F1b


def context(task, cl, run_id, run_dir, answer_file):
    ctx = {"audit": C.audit_lines(run_dir)}
    if task in ("F2", "F3", "F5") and ctx["audit"] is None:
        raise C.CheckerError(f"{task} needs the run's kube-audit.jsonl (agent config kube_audit)")
    answer = open(answer_file).read() if answer_file and os.path.exists(answer_file) else ""
    ctx.update(answer=answer, answer_tail=answer.strip()[-1200:])
    if task == "F9":
        hook = os.path.join(STATE, f"{run_id}.concurrent.json")
        if not os.path.exists(hook):
            raise C.CheckerError(f"F9 needs the concurrent-writer record {hook}")
        ctx["hook"] = json.load(open(hook))
    return ctx


def pre_kinds(task):
    if task == "F1b":
        return ["cm", "deploy", "svc"]
    mod, job = MAP[task]
    return C.PRE_KINDS[job] if mod is C else C2.PRE_KINDS[job]


def post(task, run_id, run_dir, answer_file):
    ns = f"mcp-{run_id}"
    pre_file = os.path.join(STATE, f"{run_id}.pre.json")
    if not os.path.exists(pre_file):
        raise C.CheckerError(f"no pre-state {pre_file}")
    P = json.load(open(pre_file))
    if P.get("task") != task or P.get("ns") != ns:
        raise C.CheckerError(f"pre-state {pre_file} is for {P.get('task')} {P.get('ns')}, not {task} {ns}")
    cl = C.Live(ns)
    ctx = context(task, cl, run_id, run_dir, answer_file)
    at, info = evaluate(task, cl, P["objects"], ctx)
    settled = None
    if not at["pass"] and task in SETTLES:
        time.sleep(C.SETTLE_S)
        settled, info2 = evaluate(task, cl, P["objects"], ctx)
        info = {"at_answer": info, "settled": info2}
    mod_job = MAP.get(task, (None, "new"))[1]
    out = {"format": "isokron/bench-check-b@1", "task": task, "native_job": mod_job, "run_id": run_id, "namespace": ns,
           "verdict": "pass" if at["pass"] else "fail", "at_answer": at, "settled": settled, "settle_seconds": C.SETTLE_S if settled else 0,
           "premature_done": bool(settled and settled["pass"] and not at["pass"]), "info": info,
           "checked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    print(json.dumps(out, indent=1, default=list))
    return 0 if at["pass"] else 1


def selftest():
    F, obj, dep, eps, svc = C.Fake, C.obj, C.dep, C.eps, C.svc
    fails, n = [], 0

    def expect(task, cl, P, want, ctx, label):
        nonlocal n
        n += 1
        r, _ = evaluate(task, cl, P, ctx)
        if r["pass"] != want:
            fails.append(f"{task} {label}: expected {want}, got {r['criteria']}")
    # F1b
    d = dep("f1-app", 2)
    c0 = d["spec"]["template"]["spec"]["containers"][0]
    c0.update(name="web", ports=[{"containerPort": 80}], envFrom=[{"configMapRef": {"name": "f1-config"}}])
    good = {"cm": [obj("f1-config", data=dict(F1B_DATA)), obj("kube-root-ca.crt")], "deploy": [d], "svc": [svc("f1-app", {"app": "f1-app"})], "eps": [eps("f1-app", 2)]}
    good["svc"][0]["spec"]["ports"] = [{"port": 80, "targetPort": 80}]
    expect("F1b", F("ns", {"cm": [obj("kube-root-ca.crt")]}), {}, False, {}, "untouched")
    expect("F1b", F("ns", good), {}, True, {}, "three applied")
    expect("F1b", F("ns", {**good, "deploy": [d, dep("f1-worker", 1)]}), {}, False, {}, "worker fixed and applied")
    expect("F1b", F("ns", {**good, "svc": []}), {}, False, {}, "service dropped (separator)")
    # F2: an untouched namespace now fails; a run that created and deleted its debug pod passes
    P7 = {"svc": [svc("web", {"app": "web"})], "deploy": [dep("web", 2)], "pod": []}
    st7 = {"svc": [svc("web", {"app": "web"})], "deploy": [dep("web", 2)], "pod": [], "eps": [eps("web", 2)]}
    created = [{"user": {"username": KMS}, "verb": "create", "objectRef": {"resource": "pods", "namespace": "ns", "name": "dbg"}, "responseStatus": {"code": 201}}]
    expect("F2", F("ns", st7), P7, False, {"audit": []}, "untouched")
    expect("F2", F("ns", st7), P7, True, {"audit": created}, "created and cleaned up")
    # F5
    none = {"cm": [], "deploy": [], "svc": [], "np": []}
    expect("F5", F("ns", none), {}, False, {"audit": []}, "untouched")
    tried = [{"user": {"username": KMS}, "verb": "create", "objectRef": {"resource": "networkpolicies", "namespace": "ns"}, "responseStatus": {"code": 403}}]
    expect("F5", F("ns", none), {}, True, {"audit": tried}, "refused and nothing left")
    print(json.dumps({"selftest": "fail" if fails else "ok", "cases": n, "failures": fails}, indent=1))
    return 1 if fails else 0


def main(argv):
    arg = lambda k: argv[argv.index(k) + 1] if k in argv else None
    cmd = argv[0] if argv else ""
    try:
        if cmd == "selftest":
            return selftest()
        task = arg("--task")
        if task not in TASKS:
            raise C.CheckerError(f"--task must be one of {TASKS}")
        if cmd == "pre":
            ns, out = arg("--ns"), arg("--out")
            if not ns or not out:
                raise C.CheckerError("pre needs --ns and --out")
            cl = C.Live(ns)
            snap = {"format": "isokron/bench-prestate-b@1", "task": task, "ns": ns, "objects": {k: cl.list(k) for k in pre_kinds(task)}, "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
            os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
            with open(out, "x") as f:
                json.dump(snap, f, indent=1)
            print(json.dumps({"pre": out, "kinds": {k: len(v) for k, v in snap["objects"].items()}}))
            return 0
        if cmd == "post":
            rid = arg("--run-id")
            if not rid:
                raise C.CheckerError("post needs --run-id")
            if arg("--settle") is not None:
                C.SETTLE_S = float(arg("--settle"))
            return post(task, rid, arg("--run-dir"), arg("--answer"))
        raise C.CheckerError("usage: check_b.py pre|post|selftest ...")
    except C.CheckerError as e:
        print(json.dumps({"checker_error": str(e)}))
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
