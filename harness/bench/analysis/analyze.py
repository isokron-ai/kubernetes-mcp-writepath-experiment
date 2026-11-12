#!/usr/bin/env python3
"""Analysis of the Phase 4 benchmark (series isokron-k8s-mcp-2026-11-bench): the owner's section-5a report.

Reads every run of the measured store (`.evidence/isokron-k8s-mcp-2026-11-bench2/agent/<run>.<nonce12>/`: run-record.json,
metrics.json, checker.json, transcript.redacted.jsonl) and bench/order.json (task, build, arm of every run id). Correctness
is the checker's verdict as recorded by the toolkit (run status `pass`), never the agent's claim. Nothing here calls a
model or the cluster.

What it computes, per set (A and B separately, never pooled) and arm:
  - per run: status, tokens (input + output + cache creation + cache read, as `mcp-audit usability bench` counts them),
    cost, model turns, tool calls, failed calls, result bytes, wall time, claim and outcome (false "done" claims);
  - completion with Wilson 95 % intervals; expected tokens and cost per verified success (all attempts / successes)
    with p50 / p95 of per-attempt tokens and cost; every metric as baseline -> research with the % change;
  - the saving split by type of improvement (BENCHMARK-DRAFT 5a), from per-call attribution in the transcripts plus the
    per-task tags; the remainder is "unattributed". Method: see METHOD below and ANALYSIS.md.

Runs the toolkit abandoned at the 1.00 USD per-run cap count as failed attempts with their cost and tokens (they are
outcomes of the agent's behaviour, not harness failures); runs abandoned for any other harness reason are listed and
left out (they are re-run on their spare). `inconclusive` runs (audit-log problems) are attempts that are not verified.

  analyze.py --evidence DIR --order bench/order.json --out DIR [--dry]   (--dry: DIR = the dry-run root of this audit)
"""
import csv
import glob
import json
import math
import os
import re
import statistics
import sys
from collections import defaultdict

METHOD = """
Per-call roles (one per tool call, from the transcript's tool_use input and tool_result; first match wins):
  recovery   any call after a failed call (isError), until the next successful write              -> truthful results
  wait       resources_rollout action=status; a read that repeats an earlier identical read after a write (polling)
             (a write with a `wait` argument keeps its write role: what it saves is the polls)    -> waits
  batch      resources_label, resources_delete/scale with labelSelector (one call for many objects); or any write
             of a (tool, kind) that the run applies to two or more different objects (what a batch replaces) -> batch/multi-get
  multi_get  resources_get with `names`, resources_list with `kinds`/`all_kinds`; or any read whose tool the run
             uses on two or more different targets (what a multi-get replaces)                    -> batch/multi-get
  edit       resources_patch, resources_rollout undo/restart; or a get immediately followed by a full-object
             create_or_update (both calls of the read-modify-write pair)                          -> fewer edit round-trips
  other      everything else (discovery, single reads and writes)                                 -> unattributed
Per task (same task, research vs baseline, per-attempt means). Accounting identity: a run's tokens are the sum over its
model turns of (context + output); the context of a turn is the fixed base (system prompt, catalog, user prompt: the
first turn's context) plus everything said since. So T = turns x base + for every call (its tool_use input once as output
and again in every later turn, its result in every later turn) + the model's own text/thinking and its re-reads.
  catalog         = (median first-turn context, research - baseline) x research model turns   [a cost: positive tokens]
  smaller_results = for each tool present in both builds: (research mean result tokens per call - baseline mean) x
                    research calls of that tool x their mean re-reads (later model turns that carry the result)
  call roles      = (research - baseline) turn shares of the role x the baseline base context (a call's turn share =
                    1 / the number of calls its model turn issued), plus (research - baseline) calls of the role x the
                    baseline mean footprint of such a call (its turn's output share x (1 + re-reads) + result tokens x re-reads;
                    a turn's output, reasoning included, is shared among the calls it issued by argument size)
  unattributed    = the rest of the per-attempt change: the final answer turn and other turns without calls, calls of role `other`,
                    the cross-run prompt-cache state (cache writes vs reads), and anything the roles above do not capture
Completion: expected per success E = T / S. dE = [T_B/S_R - T_B/S_B] (completion, at baseline tokens) + [(T_R - T_B)/S_R]
(tokens, at research completion). The completion part goes to the task's primary type (bug fixes or truthful results,
by the cause of the baseline failures; PRIMARY below); the token part is split as above, divided by S_R.
Result tokens of a call ~ result characters / 3.0. A turn's output ~ the growth of the context to the next API call less
the results it received (the transcript carries each call's context but not its final output count).
"""

PRIMARY = {  # the type a completion change on this task is credited to (cause of the baseline failures; BENCHMARK-DRAFT section 4)
    "K1": "waits", "K2": "truthful_results", "K3": "bug_fixes", "K4": "edit_round_trips", "K5": "batch_multi_get", "K6": "truthful_results",
    "F1a": "bug_fixes", "F1b": "bug_fixes", "F2": "truthful_results", "F3": "bug_fixes", "F4": "truthful_results", "F5": "truthful_results",
    "F6": "batch_multi_get", "F7": "batch_multi_get", "F8": "truthful_results", "F9": "truthful_results",
}
ROLE_TYPE = {"wait": "waits", "batch": "batch_multi_get", "multi_get": "batch_multi_get", "edit": "edit_round_trips", "recovery": "truthful_results", "other": None}

TYPES = ["bug_fixes", "smaller_results", "batch_multi_get", "edit_round_trips", "waits", "truthful_results", "catalog", "unattributed"]
TYPE_LABEL = {"bug_fixes": "Bug fixes (completion)", "smaller_results": "Smaller results", "batch_multi_get": "Batch / multi-get",
              "edit_round_trips": "Fewer edit round-trips", "waits": "Waits (no polling)", "truthful_results": "Truthful results",
              "catalog": "Catalog cost (+54 % tools/list)", "unattributed": "Unattributed"}
CHARS_PER_TOKEN = 3.0  # JSON/YAML tool results; an approximation, stated in the report
WRITE_TOOLS = {"resources_create_or_update", "resources_delete", "resources_scale", "resources_patch", "resources_label", "resources_rollout",
               "pods_delete", "pods_run", "pods_exec", "helm_install", "helm_uninstall"}


def wilson(k, n, z=1.959964):
    if n == 0:
        return (None, None, None)
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return (p, max(0.0, c - h), min(1.0, c + h))


def q(xs, p):
    xs = sorted(x for x in xs if x is not None)
    if not xs:
        return None
    k = (len(xs) - 1) * p
    f, c = math.floor(k), math.ceil(k)
    return xs[f] if f == c else xs[f] + (xs[c] - xs[f]) * (k - f)


def pct(b, r):
    if b in (None, 0) or r is None:
        return None
    return 100.0 * (r - b) / b


# ------------------------------------------------------------------ reading runs

def load_transcript(path):
    msgs = []
    if not os.path.exists(path):
        return msgs
    for l in open(path):
        l = l.strip()
        if l:
            try:
                msgs.append(json.loads(l))
            except ValueError:
                pass
    return msgs


def calls_from_transcript(msgs):
    """API calls (unique assistant message ids, with usage) and tool calls (name, input, result chars, is_error, turn)."""
    turns, by_id, calls, results = [], {}, [], {}
    for m in msgs:
        if m.get("type") == "assistant":
            msg = m.get("message") or {}
            mid = msg.get("id") or f"anon-{len(turns)}"
            if mid not in by_id:
                u = msg.get("usage") or {}
                by_id[mid] = len(turns)
                turns.append({"id": mid, "context": (u.get("input_tokens") or 0) + (u.get("cache_creation_input_tokens") or 0) + (u.get("cache_read_input_tokens") or 0),
                              "output": u.get("output_tokens") or 0, "cache_creation": u.get("cache_creation_input_tokens") or 0})
            for c in msg.get("content") or []:
                if c.get("type") == "tool_use":
                    calls.append({"id": c.get("id"), "name": (c.get("name") or "").split("__")[-1], "input": c.get("input") or {}, "turn": by_id[mid]})
        elif m.get("type") == "user":
            cont = (m.get("message") or {}).get("content")
            for c in cont if isinstance(cont, list) else []:
                if c.get("type") == "tool_result":
                    body = c.get("content")
                    text = body if isinstance(body, str) else json.dumps(body)
                    results[c.get("tool_use_id")] = {"chars": len(text), "is_error": bool(c.get("is_error")), "text": text[:4000]}
    for c in calls:
        r = results.get(c["id"], {"chars": 0, "is_error": False, "text": ""})
        c.update(result_chars=r["chars"], is_error=r["is_error"], result_head=r["text"][:300])
    # The transcript's per-message usage carries the context of each API call (input + cache write + cache read) but
    # only the streaming-start output count, so per turn: result tokens ~ result characters / CHARS_PER_TOKEN, and the
    # turn's output = growth of the context to the next call - those results (the last turn: the run's remaining output).
    for i, t in enumerate(turns):
        mine = [c for c in calls if c["turn"] == i]
        nxt = turns[i + 1]["context"] if i + 1 < len(turns) else None
        res = sum(c["result_chars"] for c in mine) / CHARS_PER_TOKEN
        t["output_est"] = max(0.0, nxt - t["context"] - res) if nxt is not None else None
        ichars = sum(len(json.dumps(c["input"])) for c in mine) or 1
        for c in mine:
            c["turn_share"] = 1.0 / len(mine)
            c["result_tokens"] = c["result_chars"] / CHARS_PER_TOKEN
            c["rereads"] = max(0, len(turns) - 1 - i)
            # the output of the turn that issued the call (its tool_use block and the reasoning written with it), shared
            # among the turn's calls by argument size: written once, then context of every later turn
            c["input_tokens"] = (t["output_est"] or 0.0) * len(json.dumps(c["input"])) / ichars
    return turns, calls


def target_of(c):
    i = c["input"]
    return (i.get("kind") or ("Pod" if c["name"].startswith("pods_") else ""), i.get("name") or i.get("labelSelector") or "", i.get("namespace") or "")


READ_TOOLS = {"resources_get", "resources_list", "pods_get", "pods_list", "pods_list_in_namespace", "pods_log", "events_list", "helm_list", "auth_can_i"}


def roles(calls):
    """One role per call (METHOD). Groups first: per run, the distinct objects written by each (tool, kind) and the
    distinct targets read by each read tool; a group of two or more is the work one batch or multi-get call replaces."""
    is_write = lambda c: c["name"] in WRITE_TOOLS and not (c["name"] == "resources_rollout" and c["input"].get("action") == "status")
    wgroups, rgroups = defaultdict(set), defaultdict(set)
    for c in calls:
        kind, name, _ = target_of(c)
        if is_write(c):
            wgroups[(c["name"], kind)].add(name)
        elif c["name"] in READ_TOOLS:
            rgroups[c["name"]].add((kind, name))
    last_write, seen_reads, after_error = -1, {}, False
    for k, c in enumerate(calls):
        n, i = c["name"], c["input"]
        kind, name, _ = target_of(c)
        w = is_write(c)
        key = (n, json.dumps(i, sort_keys=True))
        if after_error:
            role = "recovery"
        elif n == "resources_rollout" and i.get("action") == "status":
            role = "wait"
        elif not w and key in seen_reads and last_write >= 0:
            role = "wait"  # the same read again after a write: polling for the write's effect
        elif n == "resources_label" or (n in ("resources_delete", "resources_scale") and i.get("labelSelector")):
            role = "batch"
        elif (n == "resources_get" and i.get("names")) or (n == "resources_list" and (i.get("kinds") or i.get("all_kinds"))):
            role = "multi_get"
        elif w and len(wgroups[(n, kind)]) >= 2:
            role = "batch"  # one of several per-item writes of the same kind and operation
        elif n == "resources_patch" or (n == "resources_rollout" and i.get("action") in ("undo", "restart")):
            role = "edit"
        elif w and n == "resources_create_or_update" and k > 0 and calls[k - 1]["name"] == "resources_get" and calls[k - 1]["role"] in ("other", "multi_get"):
            role = "edit"
            calls[k - 1]["role"] = "edit"  # the read of a read-modify-write pair
        elif not w and n in READ_TOOLS and len(rgroups[n]) >= 2:
            role = "multi_get"  # one of several reads of different targets with the same tool
        else:
            role = "other"
        c["role"] = role
        if not w:
            seen_reads.setdefault(key, k)
        if c["is_error"]:
            after_error = True
        elif w:
            after_error = False
            last_write = k
    return calls


KMS = "system:serviceaccount:mcp-audit-system:kms"


def f2_posthoc(d, chk):
    """POST-HOC (labelled): F2's frozen bench_fix criterion counted only a `create` of a Pod by the server identity, but
    the server creates pods with a server-side apply PATCH (201). The corrected criterion counts either, from the run's own
    kube-audit.jsonl; every other recorded criterion is taken as the frozen checker recorded it at the answer."""
    crit = dict(((chk.get("at_answer") or {}).get("criteria")) or {})
    key = "debug_pod_was_created (bench_fix)"
    if key not in crit:
        return None
    made = False
    p = os.path.join(d, "kube-audit.jsonl")
    for l in open(p) if os.path.exists(p) else []:
        try:
            e = json.loads(l)
        except ValueError:
            continue
        o = e.get("objectRef") or {}
        if (e.get("user") or {}).get("username") == KMS and o.get("resource") == "pods" and not o.get("subresource") and e.get("verb") in ("create", "patch") \
                and (e.get("responseStatus") or {}).get("code") == 201:
            made = True
    crit[key] = made
    return all(crit.values())


def load_runs(evdir, order, dry=False):
    runs = []
    meta = {m["run_id"]: m for m in order["measured"]}
    pattern = os.path.join(evdir, "dry-*", "*.*") if dry else os.path.join(evdir, "agent", "*.*")
    for d in sorted(glob.glob(pattern)):
        if not os.path.isdir(d):
            continue
        rid = os.path.basename(d).rsplit(".", 1)[0]
        rr = os.path.join(d, "run-record.json")
        if not os.path.exists(rr) or rid not in order["task_of"]:
            continue
        rec = json.load(open(rr))
        m = json.load(open(os.path.join(d, "metrics.json"))) if os.path.exists(os.path.join(d, "metrics.json")) else {}
        chk = json.load(open(os.path.join(d, "checker.json"))) if os.path.exists(os.path.join(d, "checker.json")) else {}
        turns, calls = calls_from_transcript(load_transcript(os.path.join(d, "transcript.redacted.jsonl")))
        roles(calls)
        tk = m.get("tokens") or {}
        task = order["task_of"][rid]
        arm = {"o": "opus", "s": "sonnet", "x": "observer"}[rid[0]]
        refusal = rec.get("refusal") or {}
        run_cap = rec.get("status") == "abandoned" and refusal.get("kind") == "budget" and "run budget reached" in (refusal.get("reason") or "")
        tc = m.get("tool_calls") or []
        runs.append({
            "run_id": rid, "task": task, "set": order["set_of"][task], "build": order["build_of"][rid], "arm": arm,
            "round": (meta.get(rid) or {}).get("round"), "spare": rid.endswith("-s"),
            "status": rec.get("status"), "refusal": refusal.get("reason"), "run_cap": run_cap,
            "attempt": rec.get("status") in ("pass", "fail", "agent_error", "inconclusive", "checker_error") or run_cap,
            "verified": rec.get("status") == "pass",
            "posthoc_f2": f2_posthoc(d, chk) if task == "F2" and rec.get("status") in ("pass", "fail") else None,
            "tokens": sum((tk.get(x) or 0) for x in ("input", "output", "cache_creation", "cache_read")),
            "input": tk.get("input") or 0, "output": tk.get("output") or 0, "cache_creation": tk.get("cache_creation") or 0, "cache_read": tk.get("cache_read") or 0,
            "cost": m.get("cost_usd") if m.get("cost_usd") is not None else rec.get("budget", {}).get("cost_usd"),
            "model_turns": rec.get("model_turns") if rec.get("model_turns") is not None else m.get("model_turns"),
            "tool_calls": len(tc) if tc else (rec.get("counts") or {}).get("tool_calls"),
            "failed_calls": sum(1 for x in tc if x.get("is_error")) if tc else (rec.get("counts") or {}).get("errors"),
            "result_bytes": sum(x.get("result_bytes") or 0 for x in tc),
            "wall_s": m.get("wall_seconds"), "claim": (rec.get("claim") or {}).get("claim"), "outcome": rec.get("outcome"),
            "false_done": rec.get("outcome") == "false_done",
            "checker_at_answer": ((chk.get("at_answer") or {}).get("pass")),
            "failed_criteria": sorted(k for k, v in (((chk.get("final") or chk.get("at_answer") or {}).get("criteria")) or {}).items() if not v), "premature": bool(chk.get("premature_done") or chk.get("passed_only_after_waiting")),
            "kube_audit_problems": len(((rec.get("kube_audit") or {}).get("problems")) or []),
            "turn_list": turns, "calls": calls, "dir": d,
        })
    return runs


# ------------------------------------------------------------------ aggregation

def agg(rs):
    att = [r for r in rs if r["attempt"]]
    S = sum(1 for r in att if r["verified"])
    T = sum(r["tokens"] for r in att)
    C = sum(r["cost"] or 0 for r in att)
    p, lo, hi = wilson(S, len(att))
    med = lambda k: statistics.median([r[k] for r in att if r[k] is not None]) if any(r[k] is not None for r in att) else None
    mean = lambda k: statistics.mean([r[k] for r in att if r[k] is not None]) if any(r[k] is not None for r in att) else None
    return {"attempts": len(att), "verified": S, "completion": p, "wilson_lo": lo, "wilson_hi": hi,
            "total_tokens": T, "total_cost": C,
            "exp_tokens_per_success": T / S if S else None, "exp_cost_per_success": C / S if S else None,
            "tokens_p50": q([r["tokens"] for r in att], .5), "tokens_p95": q([r["tokens"] for r in att], .95),
            "cost_p50": q([r["cost"] for r in att], .5), "cost_p95": q([r["cost"] for r in att], .95),
            "tool_calls_med": med("tool_calls"), "tool_calls_mean": mean("tool_calls"), "model_turns_med": med("model_turns"), "model_turns_mean": mean("model_turns"),
            "result_bytes_med": med("result_bytes"), "result_bytes_mean": mean("result_bytes"), "wall_s_med": med("wall_s"), "wall_s_mean": mean("wall_s"),
            "failed_calls": sum(r["failed_calls"] or 0 for r in att), "failed_calls_mean": mean("failed_calls"),
            "false_done": sum(1 for r in att if r["false_done"]), "premature": sum(1 for r in att if r["premature"]),
            "run_cap_hits": sum(1 for r in att if r["run_cap"]), "inconclusive": sum(1 for r in att if r["status"] == "inconclusive"),
            "agent_error": sum(1 for r in att if r["status"] == "agent_error")}


def fit_prices(rs):
    """USD per token for input, output, cache write, cache read: least squares over the runs' reported cost (exact when
    the provider bills linearly; the residual is reported)."""
    X = [[r["input"], r["output"], r["cache_creation"], r["cache_read"]] for r in rs if r["cost"] is not None]
    y = [r["cost"] for r in rs if r["cost"] is not None]
    n = 4
    M = [[sum(x[i] * x[j] for x in X) for j in range(n)] + [sum(x[i] * v for x, v in zip(X, y))] for i in range(n)]
    for i in range(n):
        piv = max(range(i, n), key=lambda k: abs(M[k][i]))
        M[i], M[piv] = M[piv], M[i]
        if abs(M[i][i]) < 1e-12:
            return None
        for k in range(n):
            if k != i:
                f = M[k][i] / M[i][i]
                M[k] = [a - f * b for a, b in zip(M[k], M[i])]
    sol = [M[i][n] / M[i][i] for i in range(n)]
    resid = max(abs(sum(a * b for a, b in zip(x, sol)) - v) for x, v in zip(X, y))
    return {"input": sol[0], "output": sol[1], "cache_write": sol[2], "cache_read": sol[3], "max_residual_usd": resid}


def first_context(rs):
    xs = [r["turn_list"][0]["context"] for r in rs if r["attempt"] and r["turn_list"]]
    return statistics.median(xs) if xs else None


def per_task_split(B, R, P=None):
    """Per-attempt token change of one task split by type (tokens per attempt), plus the inputs used."""
    aB, aR = [r for r in B if r["attempt"]], [r for r in R if r["attempt"]]
    if not aB or not aR:
        return None
    mB = statistics.mean(r["tokens"] for r in aB)
    mR = statistics.mean(r["tokens"] for r in aR)
    turnsB = statistics.mean((r["model_turns"] or len(r["turn_list"]) or 1) for r in aB)
    turnsR = statistics.mean((r["model_turns"] or len(r["turn_list"]) or 1) for r in aR)
    per_turn_B = mB / turnsB if turnsB else 0
    out = {t: 0.0 for t in TYPES}
    cost = {t: 0.0 for t in TYPES}
    P = P or {"input": 0, "output": 0, "cache_write": 0, "cache_read": 0}
    cB, cR = first_context(aB), first_context(aR)
    out["catalog"] = ((cR - cB) * turnsR) if (cB is not None and cR is not None) else 0.0
    cost["catalog"] = out["catalog"] * P["cache_read"]
    # smaller results: same tool in both builds
    def bytool(rs):
        d = defaultdict(list)
        for r in rs:
            for c in r["calls"]:
                d[c["name"]].append(c)
        return d
    tB, tR = bytool(aB), bytool(aR)
    sr = 0.0
    for name, cs in tR.items():
        if name not in tB:
            continue
        mbt = statistics.mean(c["result_tokens"] for c in tB[name])
        mrt = statistics.mean(c["result_tokens"] for c in cs)
        rer = statistics.mean(c["rereads"] for c in cs)
        sr += (mrt - mbt) * (len(cs) / len(aR)) * rer
        cost["smaller_results"] += (mrt - mbt) * (len(cs) / len(aR)) * (rer * P["cache_read"] + P["cache_write"])
    out["smaller_results"] = sr
    roleB, roleR = defaultdict(float), defaultdict(float)
    cntB, cntR, fpB, fpR = defaultdict(float), defaultdict(float), defaultdict(list), defaultdict(list)
    fcB, fcR = defaultdict(list), defaultdict(list)
    for r in aB:
        for c in r["calls"]:
            cntB[c["role"]] += 1 / len(aB)
            fpB[c["role"]].append(c["result_tokens"] * c["rereads"] + c["input_tokens"] * (1 + c["rereads"]))
            fcB[c["role"]].append(c["result_tokens"] * (c["rereads"] * P["cache_read"] + P["cache_write"]) + c["input_tokens"] * (P["output"] + c["rereads"] * P["cache_read"]))
    for r in aR:
        for c in r["calls"]:
            cntR[c["role"]] += 1 / len(aR)
            fpR[c["role"]].append(c["result_tokens"] * c["rereads"] + c["input_tokens"] * (1 + c["rereads"]))
            fcR[c["role"]].append(c["result_tokens"] * (c["rereads"] * P["cache_read"] + P["cache_write"]) + c["input_tokens"] * (P["output"] + c["rereads"] * P["cache_read"]))
    # each model turn's cost is shared by the calls it issued (parallel calls in one turn cost one turn)
    for r in aB:
        for c in r["calls"]:
            roleB[c["role"]] += c.get("turn_share", 1.0) / len(aB)
    for r in aR:
        for c in r["calls"]:
            roleR[c["role"]] += c.get("turn_share", 1.0) / len(aR)
    role_delta = {}
    for role in set(roleB) | set(roleR):
        dc = roleR[role] - roleB[role]
        role_delta[role] = dc
        typ = ROLE_TYPE.get(role)
        if typ:
            # turn part, plus the results these calls add, valued at baseline sizes (size changes are smaller_results)
            fp = statistics.mean(fpB[role]) if fpB[role] else (statistics.mean(fpR[role]) if fpR[role] else 0.0)
            out[typ] += dc * (cB or 0) + (cntR[role] - cntB[role]) * fp
            fc = statistics.mean(fcB[role]) if fcB[role] else (statistics.mean(fcR[role]) if fcR[role] else 0.0)
            cost[typ] += dc * (cB or 0) * P["cache_read"] + (cntR[role] - cntB[role]) * fc
    attributed = sum(v for k, v in out.items() if k != "unattributed")
    out["unattributed"] = (mR - mB) - attributed
    cmB = statistics.mean(r["cost"] or 0 for r in aB)
    cmR = statistics.mean(r["cost"] or 0 for r in aR)
    cost["unattributed"] = (cmR - cmB) - sum(v for k, v in cost.items() if k != "unattributed")
    return {"per_attempt": out, "per_attempt_cost": cost, "mean_tokens_B": mB, "mean_tokens_R": mR, "turns_B": turnsB, "turns_R": turnsR, "per_turn_B": per_turn_B,
            "first_context_B": cB, "first_context_R": cR, "role_calls_B": dict(roleB), "role_calls_R": dict(roleR), "role_delta": role_delta}


def set_split(runs, tasks, cost=False, P=None):
    """Set-level change in expected tokens (or cost) per verified success, split by type."""
    key = "cost" if cost else "tokens"
    B = [r for r in runs if r["task"] in tasks and r["build"] == "baseline" and r["attempt"]]
    R = [r for r in runs if r["task"] in tasks and r["build"] == "research" and r["attempt"]]
    TB, TR = sum(r[key] or 0 for r in B), sum(r[key] or 0 for r in R)
    SB, SR = sum(r["verified"] for r in B), sum(r["verified"] for r in R)
    if not SB or not SR:
        return None
    EB, ER = TB / SB, TR / SR
    split = {t: 0.0 for t in TYPES}
    # completion part, credited by successes gained per task
    comp = TB / SR - TB / SB
    gains = {t: sum(r["verified"] for r in R if r["task"] == t) - sum(r["verified"] for r in B if r["task"] == t) for t in tasks}
    tot_gain = sum(gains.values())
    if tot_gain:
        for t, g in gains.items():
            split[PRIMARY[t]] += comp * g / tot_gain
    else:
        split["unattributed"] += comp
    # token part: per-task per-attempt splits x research attempts, over S_R; tokens -> cost by each task's baseline cost per token
    tokpart = (TR - TB) / SR
    acc = 0.0
    for t in tasks:
        b = [r for r in B if r["task"] == t]
        rr = [r for r in R if r["task"] == t]
        s = per_task_split(b, rr, P)
        if not s:
            continue
        for typ, v in s["per_attempt_cost" if cost else "per_attempt"].items():
            if typ == "unattributed":
                continue
            val = v * len(rr) / SR
            split[typ] += val
            acc += val
    split["unattributed"] += tokpart - acc
    return {"E_B": EB, "E_R": ER, "delta": ER - EB, "pct": pct(EB, ER), "completion_part": comp, "token_part": tokpart, "split": split,
            "S_B": SB, "S_R": SR, "attempts_B": len(B), "attempts_R": len(R)}


# ------------------------------------------------------------------ report

def fmt(x, d=0):
    if x is None:
        return "-"
    if isinstance(x, float) and d:
        return f"{x:,.{d}f}"
    return f"{x:,.0f}" if isinstance(x, (int, float)) else str(x)


def fpct(x):
    return "-" if x is None else f"{x:+.1f} %"


def headline_rows(A, Bv):
    rows = []
    def row(label, b, r, d=0, lower_better=True):
        rows.append((label, b, r, pct(b, r), d))
    row("completion (verified / attempts)", A["completion"], Bv["completion"], 3)
    row("expected tokens per verified success", A["exp_tokens_per_success"], Bv["exp_tokens_per_success"])
    row("expected cost per verified success (USD)", A["exp_cost_per_success"], Bv["exp_cost_per_success"], 4)
    row("tokens per attempt p50", A["tokens_p50"], Bv["tokens_p50"])
    row("tokens per attempt p95", A["tokens_p95"], Bv["tokens_p95"])
    row("cost per attempt p50 (USD)", A["cost_p50"], Bv["cost_p50"], 4)
    row("cost per attempt p95 (USD)", A["cost_p95"], Bv["cost_p95"], 4)
    row("tool calls per attempt (mean)", A["tool_calls_mean"], Bv["tool_calls_mean"], 2)
    row("model turns per attempt (mean)", A["model_turns_mean"], Bv["model_turns_mean"], 2)
    row("result bytes per attempt (mean)", A["result_bytes_mean"], Bv["result_bytes_mean"])
    row("wall seconds per attempt (mean)", A["wall_s_mean"], Bv["wall_s_mean"], 1)
    row("failed calls (total)", A["failed_calls"], Bv["failed_calls"])
    row("false \"done\" claims (claimed done, checker failed)", A["false_done"], Bv["false_done"])
    return rows


def md_headline(title, A, Bv):
    L = [f"| metric ({title}) | baseline | research | change |", "|---|---|---|---|"]
    L.append(f"| attempts | {A['attempts']} | {Bv['attempts']} | |")
    L.append(f"| verified successes | {A['verified']} | {Bv['verified']} | |")
    L.append(f"| completion, Wilson 95 % | {A['completion']:.2f} [{A['wilson_lo']:.2f}-{A['wilson_hi']:.2f}] | {Bv['completion']:.2f} [{Bv['wilson_lo']:.2f}-{Bv['wilson_hi']:.2f}] | {fpct(pct(A['completion'], Bv['completion']))} |"
             if A["attempts"] and Bv["attempts"] else "| completion | - | - | |")
    for label, b, r, c, d in headline_rows(A, Bv)[1:]:
        L.append(f"| {label} | {fmt(b, d)} | {fmt(r, d)} | {fpct(c)} |")
    return "\n".join(L)


def main(argv):
    arg = lambda k, dflt=None: argv[argv.index(k) + 1] if k in argv else dflt
    evdir, order_f, out = arg("--evidence"), arg("--order"), arg("--out")
    if not evdir or not order_f or not out:
        print(__doc__)
        return 2
    order = json.load(open(order_f))
    runs = load_runs(evdir, order, dry="--dry" in argv)
    os.makedirs(out, exist_ok=True)
    sets = {"A": [t for t, s in order["set_of"].items() if s == "A"], "B": [t for t, s in order["set_of"].items() if s == "B"]}
    # POST-HOC view of set B with F2's creation criterion corrected (f2_posthoc); reported next to the recorded verdicts
    import copy as _copy
    post = []
    for r in runs:
        if r["posthoc_f2"] is not None:
            r2 = dict(r)
            r2["verified"] = bool(r["posthoc_f2"])
            r2["false_done"] = r["false_done"] and not r2["verified"]
            post.append(r2)
        else:
            post.append(r)
    res = {"method": METHOD, "primary": PRIMARY, "tags": order["tags"], "arms": {}}
    for arm in ("opus", "sonnet", "observer"):
        ra = [r for r in runs if r["arm"] == arm]
        if not ra:
            continue
        P = fit_prices(ra)
        res.setdefault("prices_usd_per_token", {})[arm] = P
        res["arms"][arm] = {}
        for sname, tasks in sets.items():
            rs = [r for r in ra if r["task"] in tasks]
            if not rs:
                continue
            b = agg([r for r in rs if r["build"] == "baseline"])
            r_ = agg([r for r in rs if r["build"] == "research"])
            per_task = {}
            for t in tasks:
                bt, rt = [r for r in rs if r["task"] == t and r["build"] == "baseline"], [r for r in rs if r["task"] == t and r["build"] == "research"]
                if not bt and not rt:
                    continue
                per_task[t] = {"baseline": agg(bt), "research": agg(rt), "split": per_task_split(bt, rt, P) if arm != "observer" else None}
            if sname == "B" and arm != "observer":
                pr = [r for r in post if r["arm"] == arm and r["task"] in tasks]
                res["arms"][arm]["B (post-hoc: F2 criterion corrected)"] = {
                    "baseline": agg([r for r in pr if r["build"] == "baseline"]), "research": agg([r for r in pr if r["build"] == "research"]),
                    "per_task": {t: {"baseline": agg([r for r in pr if r["task"] == t and r["build"] == "baseline"]), "research": agg([r for r in pr if r["task"] == t and r["build"] == "research"]), "split": None} for t in tasks},
                    "split_tokens": set_split(pr, tasks, P=P), "split_cost": set_split(pr, tasks, cost=True, P=P)}
            res["arms"][arm][sname] = {"baseline": b, "research": r_, "per_task": per_task,
                                       "split_tokens": set_split(rs, tasks, P=P) if arm != "observer" else None,
                                       "split_cost": set_split(rs, tasks, cost=True, P=P) if arm != "observer" else None}
    # per-run table
    cols = ["run_id", "set", "task", "build", "arm", "round", "status", "verified", "run_cap", "tokens", "input", "output", "cache_creation", "cache_read", "cost",
            "model_turns", "tool_calls", "failed_calls", "result_bytes", "wall_s", "claim", "outcome", "checker_at_answer", "premature", "kube_audit_problems", "refusal"]
    with open(os.path.join(out, "runs.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(cols)
        for r in sorted(runs, key=lambda x: x["run_id"]):
            w.writerow([r[c] for c in cols])
    with open(os.path.join(out, "calls.jsonl"), "w") as f:
        for r in runs:
            for c in r["calls"]:
                f.write(json.dumps({"run_id": r["run_id"], "task": r["task"], "build": r["build"], "arm": r["arm"], "tool": c["name"], "role": c["role"],
                                    "is_error": c["is_error"], "result_chars": c["result_chars"], "result_tokens": round(c["result_tokens"], 1), "rereads": c["rereads"],
                                    "input": c["input"]}) + "\n")
    json.dump(res, open(os.path.join(out, "analysis.json"), "w"), indent=1, default=str)
    # markdown
    L = ["# Analysis tables (generated by analyze.py)", ""]
    for arm, d in res["arms"].items():
        for sname, x in sorted(d.items()):
            L += [f"## {arm}, set {sname}", "", md_headline(f"set {sname}, {arm}", x["baseline"], x["research"]), ""]
            for label, key in (("tokens", "split_tokens"), ("cost (USD)", "split_cost")):
                s = x.get(key)
                if not s:
                    continue
                L += [f"### Change in expected {label} per verified success, by type of improvement (set {sname}, {arm})", "",
                      f"baseline {fmt(s['E_B'], 4 if 'cost' in key else 0)} -> research {fmt(s['E_R'], 4 if 'cost' in key else 0)}: {fmt(s['delta'], 4 if 'cost' in key else 0)} ({fpct(s['pct'])}); completion part {fmt(s['completion_part'], 4 if 'cost' in key else 0)}, token part {fmt(s['token_part'], 4 if 'cost' in key else 0)}", "",
                      "| type | change per verified success | share of the baseline value |", "|---|---|---|"]
                for t in TYPES:
                    v = s["split"][t]
                    L.append(f"| {TYPE_LABEL[t]} | {fmt(v, 4 if 'cost' in key else 0)} | {fpct(100 * v / s['E_B'])} |")
                L.append(f"| **total** | {fmt(s['delta'], 4 if 'cost' in key else 0)} | {fpct(s['pct'])} |")
                L.append("")
            L += [f"### Per task (set {sname}, {arm})", "", "| task | tags | baseline verified | research verified | baseline exp. tokens/success | research | change | baseline calls (mean) | research | baseline cost/success | research |", "|---|---|---|---|---|---|---|---|---|---|---|"]
            for t, pt in x["per_task"].items():
                b, r_ = pt["baseline"], pt["research"]
                L.append(f"| {t} | {', '.join(order['tags'][t])} | {b['verified']}/{b['attempts']} | {r_['verified']}/{r_['attempts']} | {fmt(b['exp_tokens_per_success'])} | {fmt(r_['exp_tokens_per_success'])} | {fpct(pct(b['exp_tokens_per_success'], r_['exp_tokens_per_success']))} | {fmt(b['tool_calls_mean'], 1)} | {fmt(r_['tool_calls_mean'], 1)} | {fmt(b['exp_cost_per_success'], 4)} | {fmt(r_['exp_cost_per_success'], 4)} |")
            L.append("")
    # per-type table from the task tags (BENCHMARK-DRAFT 5a, source 2)
    L += ["## Per type of improvement, from the task tags", "",
          "Each task is tagged with the change types it exercises; a task counts under every tag it has. Mean of the per-task % changes (expected tokens / cost per verified success; tasks with no success in a build are left out of the mean and counted in `n/a`), and the verified successes gained.", ""]
    for arm, d in res["arms"].items():
        if arm == "observer":
            continue
        for sname, x in sorted(d.items()):
            L += [f"### {arm}, set {sname}", "", "| type | tasks | mean % change, tokens / success | mean % change, cost / success | n/a | successes baseline -> research |", "|---|---|---|---|---|---|"]
            for typ in ["bug_fixes", "smaller_results", "batch", "multi_get", "edit_round_trips", "waits", "truthful_results"]:
                ts = [t for t in x["per_task"] if typ in order["tags"][t]]
                if not ts:
                    continue
                ch_t, ch_c, na = [], [], 0
                sb = sum(x["per_task"][t]["baseline"]["verified"] for t in ts)
                sr = sum(x["per_task"][t]["research"]["verified"] for t in ts)
                for t in ts:
                    b, r_ = x["per_task"][t]["baseline"], x["per_task"][t]["research"]
                    a1, a2 = pct(b["exp_tokens_per_success"], r_["exp_tokens_per_success"]), pct(b["exp_cost_per_success"], r_["exp_cost_per_success"])
                    if a1 is None:
                        na += 1
                    else:
                        ch_t.append(a1); ch_c.append(a2)
                L.append(f"| {typ} | {', '.join(ts)} | {fpct(statistics.mean(ch_t)) if ch_t else '-'} | {fpct(statistics.mean(ch_c)) if ch_c else '-'} | {na} | {sb} -> {sr} |")
            L.append("")
    # observer effect: set A on Opus, capture on vs off
    if "observer" in res["arms"] and "opus" in res["arms"]:
        L += ["## Observer-effect control (set A, Opus): capture on vs capture off", "",
              "| build | arm | attempts | verified | completion (Wilson 95 %) | cost per attempt (mean) | model turns (mean) | wall s (mean) |", "|---|---|---|---|---|---|---|---|"]
        for b in ("baseline", "research"):
            for arm in ("opus", "observer"):
                x = res["arms"][arm].get("A", {}).get(b if False else "baseline" if b == "baseline" else "research")
                rs = [r for r in runs if r["arm"] == arm and r["set"] == "A" and r["build"] == b]
                a = agg(rs)
                cm = statistics.mean([r["cost"] for r in rs if r["attempt"] and r["cost"] is not None]) if rs else None
                L.append(f"| {b} | {'capture on (opus)' if arm == 'opus' else 'capture off (observer)'} | {a['attempts']} | {a['verified']} | {fmt(a['completion'], 2)} [{fmt(a['wilson_lo'], 2)}-{fmt(a['wilson_hi'], 2)}] | {fmt(cm, 4)} | {fmt(a['model_turns_mean'], 2)} | {fmt(a['wall_s_mean'], 1)} |")
        L.append("")
    # false done claims and failures
    L += ["## Every failed attempt, with the checks that failed", "", "| run | set | task | build | arm | status | claim | outcome | failed checks |", "|---|---|---|---|---|---|---|---|---|"]
    for r in sorted(runs, key=lambda x: (x["arm"], x["set"], x["task"], x["build"], x["run_id"])):
        if r["attempt"] and not r["verified"]:
            L.append(f"| {r['run_id']} | {r['set']} | {r['task']} | {r['build']} | {r['arm']} | {r['status']} | {r['claim']} | {r['outcome']} | {', '.join(r['failed_criteria'])} |")
    L.append("")
    L += ["## Prices fitted from the reported costs (USD per million tokens)", ""]
    for arm, P in res.get("prices_usd_per_token", {}).items():
        if P:
            L.append(f"- {arm}: input {P['input']*1e6:.2f}, output {P['output']*1e6:.2f}, cache write {P['cache_write']*1e6:.2f}, cache read {P['cache_read']*1e6:.2f} (largest residual {P['max_residual_usd']:.2e} USD)")
    L.append("")
    open(os.path.join(out, "TABLES.md"), "w").write("\n".join(L) + "\n")
    with open(os.path.join(out, "RUNS.md"), "w") as f:
        f.write("# Every run (generated by analyze.py)\n\n| " + " | ".join(cols + ["failed_criteria"]) + " |\n|" + "---|" * (len(cols) + 1) + "\n")
        for r in sorted(runs, key=lambda x: x["run_id"]):
            f.write("| " + " | ".join(str(r[c]) if not isinstance(r[c], float) else f"{r[c]:.4f}" for c in cols) + " | " + ", ".join(r["failed_criteria"]) + " |\n")
    print(json.dumps({"runs": len(runs), "attempts": sum(r["attempt"] for r in runs), "out": out}))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
