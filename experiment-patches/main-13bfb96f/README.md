# The patch series rebased on upstream main 13bfb96f

The same 35 changes as [`../bugfix/`](../bugfix/) and [`../usability/`](../usability/), with the same file names and
order, rebased onto `containers/kubernetes-mcp-server` main `13bfb96f1d97c6ac5ff5c1f2643a821c59af88cf` (10 November 2026,
25 commits after v0.1.2). They are what the upstream issues offer and what pull requests would be cut from.

```sh
git clone https://github.com/containers/kubernetes-mcp-server && cd kubernetes-mcp-server
git checkout 13bfb96f1d97c6ac5ff5c1f2643a821c59af88cf
for s in bugfix usability; do
  sed 's/#.*//; /^[[:space:]]*$/d' /path/to/experiment-patches/main-13bfb96f/$s/SERIES |
    while read -r p; do git am "/path/to/experiment-patches/main-13bfb96f/$s/$p" < /dev/null; done
done
```

All 35 apply in order with `git am`, and the result builds; `go test -race ./...` passed on main plus all 35 (go1.26.11,
envtest). How the v0.1.2 series itself applies to main, patch by patch:

| Result | Patches |
| --- | --- |
| `git apply` clean | 29 of 35 |
| `git am -3` merges without conflict (context moved by upstream #1135's description of `resources_create_or_update`, and the snapshot JSON) | usability 01 (C3, C25), 02 (C24, C26), 11 (C5, K8S-05) |
| conflict, fixed up here | bugfix 09 (K8S-14: docs table, and main's `opt_in_tools` from #1597, which the fix validates too); usability 09 (C13) and 10 (C10): `pkg/toolsets/core/toolset.go` is wrapped in `withAppOverrides` by #1651 |

`corrections/` holds the correction made after the measurement (15 November 2026), rebased the same way: apply it after
the 35 (`for s in bugfix usability corrections`). It applies with `git am` on main plus the 35, and `pkg/kubernetes`,
`pkg/toolsets/core` and `pkg/mcp` pass there (go1.26.11, envtest).

On 12 November 2026 main had moved to `ec37a146`; none of its four new commits touched a file the issues cite.
