#!/usr/bin/env python3
"""Per-run set-up and clean-up for the Phase 4 benchmark (series isokron-k8s-mcp-2026-11-bench), CONTROL identity only
(lib/kc.sh). bench/drive.mjs and bench/precheck.py call it; nothing here goes through the MCP server.

  runctl.py ns --task T --run-id R              print the run's namespace (set A: bench-kN; set B: mcp-<run id>)
  runctl.py prepare --task T --run-id R         make sure the namespace is absent (a leftover of this series is deleted
                                                first), seed it, wait for the seed to settle, snapshot the pre-state
                                                (state/bench/R.pre.json)
  runctl.py hook --task T --run-id R            the task's run-time helper, in the foreground until SIGTERM:
                                                K6 bench/checkers/finalizer_ctl.py; F9 usability/native2/concurrent.py
                                                (unchanged; record state/bench/R.concurrent.json); others exit at once
  runctl.py cleanup --task T --run-id R         drop the test finalizers left in the namespace, delete it, wait until gone
"""
import json
import os
import signal
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, ".."))
KC = os.path.join(ROOT, "lib", "kc.sh")
# BENCH_STATE: bench/drive.mjs sets state/bench-dry for the dry run (its run ids are the measured ones)
STATE = os.path.join(ROOT, "state", os.environ.get("BENCH_STATE", "bench")) if os.environ.get("BENCH_STATE", "bench") in ("bench", "bench-dry") else None
SET_A = [f"K{i}" for i in range(1, 7)]
SET_B = ["F1a", "F1b", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9"]
RUN_LABEL = "bench-session"
TEST_FINALIZERS = {"isokron.test/hold"}
ENV = {**os.environ, "PATH": "/opt/homebrew/bin:" + os.environ.get("PATH", "/usr/bin:/bin")}


class Refused(Exception):
    pass


def kc(args, timeout=120, check=True):
    r = subprocess.run([KC] + args, capture_output=True, text=True, timeout=timeout, env=ENV)
    if check and r.returncode != 0:
        raise Refused(f"kubectl {' '.join(args[:7])}: {r.stderr.strip()[:300]}")
    return r


def ns_of(task, run_id):
    if task in SET_A:
        return f"bench-{task.lower()}"
    if task in SET_B:
        ns = f"mcp-{run_id}"
        if len(ns) > 63 or not all(c.islower() or c.isdigit() or c == "-" for c in ns):
            raise Refused(f"namespace {ns} is not a valid name")
        return ns
    raise Refused(f"unknown task {task}")


def ns_exists(ns):
    return kc(["get", "ns", ns, "-o", "name", "--ignore-not-found"]).stdout.strip() != ""


def cleanup(ns, wait_s=240):
    if not ns_exists(ns):
        return {"ns": ns, "existed": False}
    lab = kc(["get", "ns", ns, "-o", "jsonpath={.metadata.labels.audit\\.isokron\\.ai/run}"]).stdout.strip()
    if lab != RUN_LABEL:
        raise Refused(f"{ns} is not labelled audit.isokron.ai/run={RUN_LABEL} (got {lab or 'none'}); not touched")
    dropped = []
    out = kc(["get", "configmaps", "-n", ns, "-o", "json"]).stdout
    for o in json.loads(out)["items"]:
        fins = o["metadata"].get("finalizers") or []
        if set(fins) & TEST_FINALIZERS:
            keep = [f for f in fins if f not in TEST_FINALIZERS]
            kc(["-n", ns, "patch", "configmap", o["metadata"]["name"], "--type=merge", "-p", json.dumps({"metadata": {"finalizers": keep}})])
            dropped.append(o["metadata"]["name"])
    kc(["delete", "ns", ns, "--wait=false"], check=False)
    end = time.time() + wait_s
    while ns_exists(ns):
        if time.time() > end:
            raise Refused(f"namespace {ns} still exists {wait_s} s after delete")
        time.sleep(2)
    return {"ns": ns, "existed": True, "finalizers_dropped": dropped}


def sh(argv, what, timeout=600):
    r = subprocess.run(argv, capture_output=True, text=True, timeout=timeout, env=ENV)
    if r.returncode != 0:
        raise Refused(f"{what} failed (exit {r.returncode}): {(r.stderr or r.stdout).strip()[-400:]}")
    return r.stdout


def wait_until(fn, what, timeout=180, step=2):
    end = time.time() + timeout
    while True:
        v = fn()
        if v:
            return v
        if time.time() > end:
            raise Refused(f"settle: {what} not reached within {timeout} s")
        time.sleep(step)


def settle(task, ns):
    """Wait until the seeded state is the one the task describes (read-only)."""
    rs = lambda d: sh([KC, "-n", ns, "rollout", "status", f"deployment/{d}", "--timeout=180s"], f"rollout status {d}")
    if task == "K1":
        rs("web")
    elif task == "K2":
        rs("api")
    elif task == "K4":
        def broken():
            pods = json.loads(kc(["-n", ns, "get", "pods", "-l", "app=api", "-o", "json"]).stdout)["items"]
            bad = [p for p in pods if "busybox" in p["spec"]["containers"][0]["image"] and any((cs.get("restartCount") or 0) >= 1 for cs in p.get("status", {}).get("containerStatuses") or [])]
            good = [p for p in pods if "nginx" in p["spec"]["containers"][0]["image"] and any(c.get("type") == "Ready" and c.get("status") == "True" for c in p.get("status", {}).get("conditions") or [])]
            return len(bad) >= 1 and len(good) == 2
        wait_until(broken, "K4 revision 2 crash-looping while revision 1 serves 2 ready pods")
    elif task == "K5":
        for n in ("svc-auth", "svc-billing", "svc-catalog", "svc-orders", "svc-search", "svc-users", "worker-mail", "worker-thumbs", "cache"):
            rs(n)
    elif task == "K6":
        rs("s42-app"), rs("keep-web")
        sh([KC, "-n", ns, "wait", "--for=condition=complete", "job/s42-job", "--timeout=180s"], "job s42-job complete")
    elif task == "F9":
        rs("api")  # as the native driver (job 17)
    time.sleep(3)


def prepare(task, run_id):
    ns = ns_of(task, run_id)
    os.makedirs(STATE, exist_ok=True)
    pre = os.path.join(STATE, f"{run_id}.pre.json")
    if os.path.exists(pre):
        raise Refused(f"{pre} exists: a run id is used once")
    left = cleanup(ns)
    if task in SET_A:
        sh([os.path.join(HERE, "seeds", "seed_a.sh"), task], f"seed {task}")
    else:
        sh([os.path.join(HERE, "seeds", "seed_b.sh"), task, ns], f"seed {task}")
    settle(task, ns)
    if task in SET_A:
        out = sh(["python3", "-I", os.path.join(HERE, "checkers", "check_a.py"), "pre", "--task", task, "--ns", ns, "--out", pre], "pre-state")
    else:
        out = sh(["python3", "-I", os.path.join(HERE, "checkers", "check_b.py"), "pre", "--task", task, "--ns", ns, "--out", pre], "pre-state")
    return {"task": task, "run_id": run_id, "ns": ns, "leftover_cleaned": left, "pre": json.loads(out)}


def hook(task, run_id):
    ns = ns_of(task, run_id)
    os.makedirs(STATE, exist_ok=True)
    if task == "K6":
        argv = ["python3", "-I", os.path.join(HERE, "checkers", "finalizer_ctl.py"), "--ns", ns, "--out", os.path.join(STATE, f"{run_id}.finalizer.json")]
    elif task == "F9":
        argv = ["python3", "-I", os.path.join(ROOT, "usability", "native2", "concurrent.py"), "--ns", ns, "--out", os.path.join(STATE, f"{run_id}.concurrent.json"), "--timeout", "600"]
    else:
        return 0
    p = subprocess.Popen(argv, env=ENV)
    signal.signal(signal.SIGTERM, lambda *_: p.terminate())
    return p.wait()


def main(argv):
    arg = lambda k: argv[argv.index(k) + 1] if k in argv else None
    cmd, task, rid = (argv[0] if argv else ""), arg("--task"), arg("--run-id")
    try:
        if not task or not rid:
            raise Refused("--task and --run-id are required")
        if cmd == "ns":
            print(ns_of(task, rid))
        elif cmd == "prepare":
            print(json.dumps(prepare(task, rid)))
        elif cmd == "hook":
            return hook(task, rid)
        elif cmd == "cleanup":
            print(json.dumps(cleanup(ns_of(task, rid))))
        else:
            raise Refused("usage: runctl.py ns|prepare|hook|cleanup --task T --run-id R")
        return 0
    except Refused as e:
        print(json.dumps({"refused": str(e)}))
        return 3


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
