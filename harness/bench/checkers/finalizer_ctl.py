#!/usr/bin/env python3
"""K6's seed-side controller (TASKS-DRAFT K6: "a small loop in the checker harness, not visible to the agent").

The labelled ConfigMap s42-cfg-held carries the finalizer isokron.test/hold. This loop, run by bench/drive.mjs from just
before the agent starts until the checker has finished, watches it as the CONTROL identity (lib/kc.sh). When its
deletionTimestamp appears, it waits until 20 s after that timestamp and then removes the finalizer (a JSON merge patch
of metadata.finalizers to the list without it), so the object goes away 20 s after it was deleted. It never deletes
anything and touches nothing else. It records what it saw and did in --out.

  finalizer_ctl.py --ns bench-k6 --out FILE [--delay 20] [--timeout 1800]      stops on SIGTERM or after --timeout
"""
import calendar
import json
import os
import signal
import subprocess
import sys
import time

ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
KC = os.path.join(ROOT, "lib", "kc.sh")
NAME, FIN = "s42-cfg-held", "isokron.test/hold"
stop = False


def on_term(*_):
    global stop
    stop = True


def get(ns):
    r = subprocess.run([KC, "-n", ns, "get", "configmap", NAME, "-o", "json", "--ignore-not-found"], capture_output=True, text=True, timeout=30)
    if r.returncode != 0:
        return "error", r.stderr.strip()[:200]
    return ("absent", None) if not r.stdout.strip() else ("present", json.loads(r.stdout))


def main(argv):
    arg = lambda k, d=None: argv[argv.index(k) + 1] if k in argv else d
    ns, out, delay, timeout = arg("--ns"), arg("--out"), float(arg("--delay", "20")), float(arg("--timeout", "1800"))
    if not ns or not out:
        print("usage: finalizer_ctl.py --ns NS --out FILE", file=sys.stderr)
        return 2
    signal.signal(signal.SIGTERM, on_term)
    rec = {"format": "isokron/bench-finalizer-ctl@1", "ns": ns, "object": f"configmaps/{NAME}", "finalizer": FIN, "delay_s": delay,
           "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "deletion_seen": None, "removed_at": None, "errors": []}
    save = lambda: json.dump(rec, open(out, "w"), indent=1)
    save()
    end = time.time() + timeout
    while not stop and time.time() < end:
        st, o = get(ns)
        if st == "error":
            rec["errors"].append(o)
            rec["errors"] = rec["errors"][-5:]
        elif st == "absent":
            rec["absent_at"] = rec.get("absent_at") or time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        else:
            dts = o["metadata"].get("deletionTimestamp")
            fins = o["metadata"].get("finalizers") or []
            if dts and FIN in fins:
                rec["deletion_seen"] = rec["deletion_seen"] or dts
                due = calendar.timegm(time.strptime(dts, "%Y-%m-%dT%H:%M:%SZ")) + delay
                if time.time() >= due:
                    patch = json.dumps({"metadata": {"finalizers": [f for f in fins if f != FIN]}})
                    r = subprocess.run([KC, "-n", ns, "patch", "configmap", NAME, "--type=merge", "-p", patch], capture_output=True, text=True, timeout=30)
                    rec.update(removed_at=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), patch_exit=r.returncode, patch_stderr=r.stderr.strip()[:200])
        save()
        time.sleep(1)
    rec["stopped_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    save()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
