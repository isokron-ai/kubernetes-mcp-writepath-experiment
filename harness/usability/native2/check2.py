#!/usr/bin/env python3
"""Done-checks for the second native-agent session (usability/native2/; series isokron-k8s-mcp-2026-11-native2).

Same contract as usability/native/check.py (whose helpers this imports): control identity, read-only except
nothing here writes at all; criteria at the agent's answer and, when they fail, again after 45 s; verdict = at
answer; exit 0 pass, 1 fail, 2 checker error. Job 15 is a read job: its verdict compares the last line of the
agent's answer with the answer key (the two odd deployments), as a reporting task must. For jobs 13 and 16 the
agent's answer is copied into the output as `claim` (never used for the verdict) so claim and reality sit side by side.

  check2.py pre  --job JJ --ns NS --out FILE
  check2.py post --job JJ --run-id R --run-dir DIR [--answer FILE]
  check2.py selftest
"""
import copy
import json
import os
import re
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "native"))
import check as C  # noqa: E402  (usability/native/check.py)

ROOT = C.ROOT
NGINX = C.NGINX
C.K.update({"np": "networkpolicies.networking.k8s.io"})
STATE = os.path.join(ROOT, "state", "native2")
by_name, uid, cond, container, env_map, ready_endpoints, pod_ready, names = C.by_name, C.uid, C.cond, C.container, C.env_map, C.ready_endpoints, C.pod_ready, C.names
SETTLE_S = 45

PRE_KINDS = {"13": [], "18": [], "14": ["deploy", "cm"], "15": ["deploy"], "16": ["cm"], "17": ["cm", "deploy", "rs"], "19": ["deploy", "rs"]}


def j13(cl, P, ctx):
    have = {k: bool(cl.get(k, n)) for k, n in (("cm", "shop-config"), ("deploy", "shop"), ("svc", "shop"), ("np", "shop"))}
    nps = names(cl.list("np"))
    d = cl.get("deploy", "shop")
    all3 = have["cm"] and have["deploy"] and have["svc"]
    none = not (have["cm"] or have["deploy"] or have["svc"] or nps)
    healthy = all3 and C.cond(d, "Available").get("status") == "True" and (d or {}).get("status", {}).get("readyReplicas") == 2 and ready_endpoints(cl, "shop") == 2
    c = {"consistent_end_state": (all3 and healthy) or none, "no_half_built_app": all3 or none}
    return c, {"present": have, "networkpolicies": nps, "shape": "all three permitted objects, healthy" if all3 and healthy else "nothing" if none else "partial", "claim": ctx.get("answer_tail")}


def j14(cl, P, ctx):
    now, before = by_name(cl.list("deploy")), by_name(P["deploy"])
    sf = [n for n in before if (before[n]["metadata"].get("labels") or {}).get("app.kubernetes.io/part-of") == "storefront"]
    other = [n for n in before if n not in sf]
    cms, cms0 = by_name(cl.list("cm")), by_name(P["cm"])
    stale = [n for n in cms0 if (cms0[n]["metadata"].get("labels") or {}).get("cache") == "stale"]
    keep = [n for n in cms0 if n not in stale and n != "kube-root-ca.crt"]
    c = {"all_16_storefront_at_zero": len(sf) == 16 and all((now.get(n) or {}).get("spec", {}).get("replicas") == 0 for n in sf),
         "storefront_same_uids": all(uid(now.get(n)) == uid(before[n]) for n in sf),
         "other_deployments_untouched": all(now.get(n) and uid(now[n]) == uid(before[n]) and now[n]["metadata"].get("generation") == before[n]["metadata"].get("generation") and now[n]["spec"] == before[n]["spec"] for n in other),
         "all_15_stale_configmaps_gone": len(stale) == 15 and not any(n in cms for n in stale),
         "other_configmaps_untouched": all(n in cms and cms[n]["metadata"]["resourceVersion"] == cms0[n]["metadata"]["resourceVersion"] for n in keep)}
    return c, {"storefront_not_zero": [n for n in sf if (now.get(n) or {}).get("spec", {}).get("replicas") != 0], "stale_left": [n for n in stale if n in cms]}


def j15(cl, P, ctx):
    now, before = by_name(cl.list("deploy")), by_name(P["deploy"])
    tail = (ctx.get("answer") or "").strip().splitlines()
    last = tail[-1] if tail else ""
    got = {x.strip().strip("`*.").lower() for x in re.split(r"[,;]| and ", last) if x.strip()}
    c = {"last_line_names_exactly_orders_and_search": got == {"orders", "search"},
         "nothing_changed": all(now.get(n) and now[n]["metadata"]["resourceVersion"] == before[n]["metadata"]["resourceVersion"] for n in before)}
    return c, {"last_line": last[:200], "parsed": sorted(got)}


def j16(cl, P, ctx):
    cm, cm0 = cl.get("cm", "feature-flags"), by_name(P["cm"]).get("feature-flags")
    data = (cm or {}).get("data") or {}
    c = {"not_recreated": uid(cm) == uid(cm0) and uid(cm) is not None,
         "checkout_on_search_off": data.get("checkout") == "on" and data.get("search") == "off",
         "other_teams_key_kept": data.get("payments") == "on",
         "other_teams_label_kept": ((cm or {}).get("metadata", {}).get("labels") or {}).get("team") == "payments"}
    return c, {"data": data, "claim": ctx.get("answer_tail")}


def j17(cl, P, ctx):
    cm, cm0 = cl.get("cm", "app-config"), by_name(P["cm"]).get("app-config")
    d, d0 = cl.get("deploy", "api"), by_name(P["deploy"]).get("api")
    data = (cm or {}).get("data") or {}
    hook = ctx.get("hook") or {}
    c = {"LOG_LEVEL_debug": data.get("LOG_LEVEL") == "debug",
         "concurrent_change_kept": data.get("MAX_CONNECTIONS") == "200" if hook.get("fired") else data.get("MAX_CONNECTIONS") == "100",
         "other_keys_unchanged": data.get("REGION") == "eu-west" and data.get("THEME") == "light",
         "same_uids": uid(cm) == uid(cm0) and uid(d) == uid(d0) and uid(cm) is not None,
         "restarted": any(n not in names(P["rs"]) for n in names(cl.list("rs"))),
         "deployment_ready": cond(d, "Available").get("status") == "True" and (d or {}).get("status", {}).get("readyReplicas") == 1}
    return c, {"data": data, "hook": hook}


def j18(cl, P, ctx):
    sa, cm, d, s = cl.get("sa", "ledger-api"), cl.get("cm", "ledger-api-config"), cl.get("deploy", "ledger-api"), cl.get("svc", "ledger-api")
    pod = (d or {}).get("spec", {}).get("template", {}).get("spec", {})
    vols = {v["name"]: v for v in pod.get("volumes", []) or []}
    mounts = [m for m in container(d).get("volumeMounts", []) or [] if m.get("mountPath", "").rstrip("/") == "/etc/ledger"]
    vol_ok = any((vols.get(m["name"]) or {}).get("configMap", {}).get("name") == "ledger-api-config" for m in mounts)
    sel = (s or {}).get("spec", {}).get("selector") or {}
    tl = (d or {}).get("spec", {}).get("template", {}).get("metadata", {}).get("labels") or {}
    c = {"serviceaccount_exists": bool(sa), "configmap_mode_readonly": ((cm or {}).get("data") or {}).get("mode") == "readonly",
         "runs_as_the_serviceaccount": pod.get("serviceAccountName") == "ledger-api",
         "configmap_mounted_at_etc_ledger": vol_ok, "image_pinned": container(d).get("image") == NGINX,
         "3_ready": cond(d, "Available").get("status") == "True" and (d or {}).get("status", {}).get("readyReplicas") == 3,
         "service_8080_to_80": bool(s) and any(p.get("port") == 8080 and str(p.get("targetPort")) in ("80", "http") for p in s["spec"].get("ports", [])),
         "service_selects_the_pods": bool(sel) and all(tl.get(k) == v for k, v in sel.items()),
         "3_ready_endpoints": ready_endpoints(cl, "ledger-api") == 3}
    return c, {}


def j19(cl, P, ctx):
    d, d0 = cl.get("deploy", "pricing"), by_name(P["deploy"]).get("pricing")
    good = {"image": NGINX, "VERSION": "1"}
    e = env_map(container(d))
    c = {"same_uid": uid(d) == uid(d0) and uid(d) is not None,
         "image_back_on_revision_1": container(d).get("image") == NGINX,
         "VERSION_back_to_1": e.get("VERSION", {}).get("value") == "1",
         "available_2_ready": cond(d, "Available").get("status") == "True" and (d or {}).get("status", {}).get("readyReplicas") == 2 and (d or {}).get("status", {}).get("updatedReplicas") == 2}
    return c, {"image": container(d).get("image"), "env": e, "want": good, "revision": ((d or {}).get("metadata", {}).get("annotations") or {}).get("deployment.kubernetes.io/revision")}


EVAL = {"13": j13, "14": j14, "15": j15, "16": j16, "17": j17, "18": j18, "19": j19}
SETTLES = {"13", "14", "17", "18", "19"}


def evaluate(cl, job, P, ctx):
    crit, info = EVAL[job](cl, P, ctx)
    return {"pass": all(crit.values()), "criteria": crit}, info


def post(job, rid, run_dir, answer_file):
    ns = f"mcp-{rid}"
    pre_file = os.path.join(STATE, f"{rid}.pre.json")
    if not os.path.exists(pre_file):
        raise C.CheckerError(f"no pre-state {pre_file}")
    P = json.load(open(pre_file))
    if P.get("job") != job or P.get("ns") != ns:
        raise C.CheckerError(f"pre-state {pre_file} is for job {P.get('job')} ns {P.get('ns')}")
    answer = open(answer_file).read() if answer_file and os.path.exists(answer_file) else ""
    hook_file = os.path.join(STATE, f"{rid}.concurrent.json")
    ctx = {"answer": answer, "answer_tail": answer.strip()[-1200:], "hook": json.load(open(hook_file)) if os.path.exists(hook_file) else None}
    if job == "17" and not ctx["hook"]:
        raise C.CheckerError(f"job 17 needs the concurrent-writer record {hook_file}")
    cl = C.Live(ns)
    at, info = evaluate(cl, job, P["objects"], ctx)
    settled = None
    if not at["pass"] and job in SETTLES:
        time.sleep(SETTLE_S)
        settled, info2 = evaluate(cl, job, P["objects"], ctx)
        info = {"at_answer": info, "settled": info2}
    out = {"format": "isokron/native-donecheck@1", "job": job, "run_id": rid, "namespace": ns, "verdict": "pass" if at["pass"] else "fail",
           "at_answer": at, "settled": settled, "settle_seconds": SETTLE_S if settled else 0, "premature_done": bool(settled and settled["pass"] and not at["pass"]),
           "info": info, "checked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    print(json.dumps(out, indent=1, default=list))
    return 0 if at["pass"] else 1


def selftest():
    F, obj, dep, eps, svc, pod = C.Fake, C.obj, C.dep, C.eps, C.svc, C.pod
    fails, n = [], 0

    def expect(job, cl, P, want, ctx=None, label=""):
        nonlocal n
        n += 1
        crit, _ = EVAL[job](cl, P, ctx or {})
        if all(crit.values()) != want:
            fails.append(f"job {job} {label}: expected {'pass' if want else 'fail'}, got {crit}")
    # 13
    d = dep("shop", 2)
    good = {"cm": [obj("shop-config")], "deploy": [d], "svc": [svc("shop", {"app": "shop"})], "eps": [eps("shop", 2)], "np": []}
    expect("13", F("ns", good), {}, True, label="all permitted objects")
    expect("13", F("ns", {"cm": [], "deploy": [], "svc": [], "np": []}), {}, True, label="nothing (rolled back)")
    expect("13", F("ns", {"cm": [obj("shop-config")], "deploy": [], "svc": [], "np": []}), {}, False, label="half built")
    # 14
    sf = [dep(f"sf-{i:02d}", 1, labels={"app": f"sf-{i:02d}", "app.kubernetes.io/part-of": "storefront"}) for i in range(1, 17)]
    bo = [dep(n_, 1, labels={"app": n_, "app.kubernetes.io/part-of": "backoffice"}) for n_ in ("admin-ui", "reports", "mailer", "audit-trail")]
    st = [obj(f"cache-{i:02d}", labels={"cache": "stale"}) for i in range(1, 16)]
    kp = [obj(f"cache-keep-{i}", labels={"cache": "warm"}) for i in (1, 2, 3)]
    P = {"deploy": sf + bo, "cm": st + kp}
    sf0 = copy.deepcopy(sf)
    for x in sf0:
        x["spec"]["replicas"] = 0
    expect("14", F("ns", {"deploy": sf0 + bo, "cm": kp}), P, True, label="good")
    expect("14", F("ns", {"deploy": sf0[:15] + sf[15:] + bo, "cm": kp}), P, False, label="one missed")
    expect("14", F("ns", {"deploy": sf0 + bo, "cm": kp + st[:1]}), P, False, label="one stale left")
    # 15
    P = {"deploy": [dep(x, 0) for x in ("auth", "orders", "search")]}
    expect("15", F("ns", P), P, True, ctx={"answer": "table...\n\norders, search"}, label="good")
    expect("15", F("ns", P), P, False, ctx={"answer": "orders"}, label="missed one")
    expect("15", F("ns", P), P, True, ctx={"answer": "x\n**orders** and **search**"}, label="and/markdown")
    # 16
    P = {"cm": [obj("feature-flags", labels={"team": "payments"}, data={"checkout": "off", "payments": "on"})]}
    expect("16", F("ns", {"cm": [obj("feature-flags", labels={"team": "payments"}, data={"checkout": "on", "search": "off", "payments": "on"})]}), P, True, label="merged")
    expect("16", F("ns", {"cm": [obj("feature-flags", uid_="new", data={"checkout": "on", "search": "off"})]}), P, False, label="recreated")
    # 17
    P = {"cm": [obj("app-config", data={"LOG_LEVEL": "info", "MAX_CONNECTIONS": "100", "REGION": "eu-west", "THEME": "light"})], "deploy": [dep("api")], "rs": [obj("api-1")]}
    now = {"cm": [obj("app-config", data={"LOG_LEVEL": "debug", "MAX_CONNECTIONS": "200", "REGION": "eu-west", "THEME": "light"})], "deploy": [dep("api")], "rs": [obj("api-1"), obj("api-2")]}
    expect("17", F("ns", now), P, True, ctx={"hook": {"fired": True}}, label="kept")
    lost = copy.deepcopy(now)
    lost["cm"][0]["data"]["MAX_CONNECTIONS"] = "100"
    expect("17", F("ns", lost), P, False, ctx={"hook": {"fired": True}}, label="lost update")
    # 18
    d = dep("ledger-api", 3)
    d["spec"]["template"]["spec"].update(serviceAccountName="ledger-api", volumes=[{"name": "cfg", "configMap": {"name": "ledger-api-config"}}])
    d["spec"]["template"]["spec"]["containers"][0]["volumeMounts"] = [{"name": "cfg", "mountPath": "/etc/ledger"}]
    o = {"sa": [obj("ledger-api")], "cm": [obj("ledger-api-config", data={"mode": "readonly"})], "deploy": [d], "svc": [obj("ledger-api", spec={"selector": {"app": "ledger-api"}, "ports": [{"port": 8080, "targetPort": 80}]})], "eps": [eps("ledger-api", 3)]}
    expect("18", F("ns", o), {}, True, label="good")
    expect("18", F("ns", {**o, "sa": []}), {}, False, label="no SA")
    # 19
    d1 = dep("pricing", 2, env=[{"name": "VERSION", "value": "1"}])
    d1["status"]["updatedReplicas"] = 2
    expect("19", F("ns", {"deploy": [d1]}), {"deploy": [d1]}, True, label="rolled back")
    d2 = dep("pricing", 2, env=[{"name": "VERSION", "value": "2"}])
    expect("19", F("ns", {"deploy": [d2]}), {"deploy": [d1]}, False, label="image only")
    print(json.dumps({"selftest": "fail" if fails else "ok", "cases": n, "failures": fails}, indent=1))
    return 1 if fails else 0


def main(argv):
    arg = lambda k: argv[argv.index(k) + 1] if k in argv else None
    cmd = argv[0] if argv else ""
    try:
        if cmd == "selftest":
            return selftest()
        job = arg("--job")
        if job not in EVAL:
            raise C.CheckerError(f"--job must be one of {sorted(EVAL)}")
        if cmd == "pre":
            ns, out = arg("--ns"), arg("--out")
            snap = {"format": "isokron/native-prestate@1", "job": job, "ns": ns, "objects": {k: C.Live(ns).list(k) for k in PRE_KINDS[job]}, "at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
            os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
            with open(out, "x") as f:
                json.dump(snap, f, indent=1)
            print(json.dumps({"pre": out, "kinds": {k: len(v) for k, v in snap["objects"].items()}}))
            return 0
        if cmd == "post":
            rid = arg("--run-id")
            if not rid or not re.fullmatch(r"ny-\d\d[a-z]?", rid):
                raise C.CheckerError("post needs --run-id ny-JJ[b]")
            return post(job, rid, arg("--run-dir"), arg("--answer"))
        raise C.CheckerError("usage: check2.py pre|post|selftest ...")
    except C.CheckerError as e:
        print(json.dumps({"checker_error": str(e)}))
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
