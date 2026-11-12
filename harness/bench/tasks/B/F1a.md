# F1 part a: job 01, deploy an app from one multi-document manifest (set B, friction-derived: favours the research build by construction)
Source: usability/native/prompts/01.md; the text between the PROMPT markers is copied unchanged (bench/drive.mjs check compares it).
Seed: bench/seeds/seed_b.sh F1a; checker: bench/checkers/check_b.py (the native done-check of job 01). Namespace mcp-{{RUN_ID}}, fresh per run.

<!-- PROMPT -->
Please deploy a small web app called `ux-web` in the `mcp-{{RUN_ID}}` namespace: two replicas of `docker.io/library/nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de`, a ConfigMap `ux-web-config` holding `greeting: hello` that the container gets as the environment variable `GREETING`, and a Service `ux-web` on port 80 in front of it. Let me know once it is actually serving.
<!-- /PROMPT -->
