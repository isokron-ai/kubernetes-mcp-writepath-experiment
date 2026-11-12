# Security

This repository is an unsupported research experiment by [IsoKron](https://isokron.ai), based on
`containers/kubernetes-mcp-server` v0.1.2 (commit `66553fed`). It is not an official release, it receives no security
updates, and it is not meant for production use.

**Do not run it against a cluster you care about.** If you run it at all, use a disposable local cluster (for example
kind) and a ServiceAccount whose RBAC limits it to a test namespace.

## Reporting

- **Vulnerabilities in the Kubernetes MCP server itself** (anything that also exists upstream): report them to the
  upstream project through its security policy (the `SECURITY.md` of `containers/kubernetes-mcp-server`), not here.
- **Problems specific to the experiment** (the commits in [CHANGES-EXPERIMENT.md](CHANGES-EXPERIMENT.md), the build
  script or the benchmark files): use this repository's Security tab, or open an issue if the problem is not sensitive.

Reports are read, but no response time or fix is promised.
