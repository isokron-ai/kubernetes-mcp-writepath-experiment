#!/usr/bin/env bash
# Build the Kubernetes MCP server (containers/kubernetes-mcp-server v0.1.2) from this repository alone.
#
#   harness/build/build.sh baseline     the pinned upstream tree (tag upstream-v0.1.2), unmodified
#   harness/build/build.sh fixed        baseline + experiment-patches/bugfix/ in its SERIES order (16 patches)
#   harness/build/build.sh research     fixed + experiment-patches/usability/ in its SERIES order (19 patches)
#
# Output: harness/build/out/<name>/kubernetes-mcp-server and harness/build/out/<name>/BUILD.json (pin, every patch with
# its sha256, Go version, target platform, flags, binary sha256). Work copies go to harness/build/work/<name>/.
# Both directories are git-ignored. A patch that does not apply stops the build; a fixed or research binary identical
# to the baseline's is refused; an existing output is refused (move it away first).
#
# Source: the tree of this repository's tag upstream-v0.1.2, which must equal upstream's 66553fed tree (KMS_TREE in
# PIN). With KMS_SOURCE=github the script instead makes a blobless clone of upstream at the pin in harness/build/src/
# and takes the tree from there (read-only use of GitHub). Either way the patches come from experiment-patches/.
#
# Toolchain: the `go` on PATH, with GOTOOLCHAIN=local (no automatic toolchain download). go.mod asks for 1.26.10; the
# measured builds used go1.26.11 (darwin/arm64), e.g. PATH=/opt/homebrew/opt/go@1.26/bin:$PATH on macOS with
# Homebrew's go@1.26. A different Go version builds, but gives different binary hashes. GOOS and GOARCH are honoured
# for cross-builds (CGO_ENABLED=0). The first build downloads the Go modules (go.sum verified, -mod=readonly).
#
# Flags as the measured builds (upstream's `make build` minus its tidy/format/lint step): -trimpath, CGO_ENABLED=0,
# -mod=readonly, the same -ldflags (version string v0.1.2-<name>, BuildTime = the pin's commit date) -- plus
# -buildvcs=false. The measured binaries carried the VCS stamp of IsoKron's internal harness repository, in which they
# were built (DRV-3: `go build` stamped vcs.revision, vcs.time and vcs.modified of that enclosing repository), so their
# sha256 cannot be reproduced outside it.
# This script embeds no VCS stamp at all, so its output depends only on the source, the Go version and the platform:
# anyone with the same Go version and GOOS/GOARCH gets the same hash (EXPERIMENT.md lists ours). The code is the same
# as in the measured builds; only the embedded build information differs.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"   # repository root
HB="$ROOT/harness/build"
refuse() { echo "REFUSED: $*" >&2; exit 2; }
# shellcheck disable=SC1091
. "$HB/PIN"
name="${1:-}"
case "$name" in baseline|fixed|research) ;; *) refuse "usage: harness/build/build.sh baseline|fixed|research" ;; esac
command -v go >/dev/null || refuse "no go on PATH"
command -v git >/dev/null || refuse "no git on PATH"

work="$HB/work/$name"
out="$HB/out/$name"
[ ! -e "$out/kubernetes-mcp-server" ] || refuse "$out already holds a build (move it away first; builds are never overwritten)"
rm -rf "$work"; mkdir -p "$work" "$out"

# 1. The pinned upstream tree, as plain files in a throwaway git repository of its own (so `git apply` works relative
#    to the server's root and never to this repository's).
case "${KMS_SOURCE:-repo}" in
  repo)
    tree="$(git -C "$ROOT" rev-parse 'upstream-v0.1.2^{tree}' 2>/dev/null)" || refuse "tag upstream-v0.1.2 not found (clone with tags, or KMS_SOURCE=github)"
    [ "$tree" = "$KMS_TREE" ] || refuse "upstream-v0.1.2 has tree $tree, the pin's tree is $KMS_TREE"
    git -C "$ROOT" archive upstream-v0.1.2 | tar -x -C "$work"
    source_desc="this repository's tag upstream-v0.1.2 (tree $tree)" ;;
  github)
    src="$HB/src/kubernetes-mcp-server"
    [ -d "$src/.git" ] || git clone --filter=blob:none --no-checkout "$KMS_REPO" "$src"
    git -C "$src" fetch --tags origin
    tag_sha="$(git -C "$src" rev-parse "refs/tags/$KMS_TAG^{commit}")"
    [ "$tag_sha" = "$KMS_SHA" ] || refuse "tag $KMS_TAG is $tag_sha upstream, the pin is $KMS_SHA"
    tree="$(git -C "$src" rev-parse "$KMS_SHA^{tree}")"
    [ "$tree" = "$KMS_TREE" ] || refuse "upstream $KMS_SHA has tree $tree, expected $KMS_TREE"
    git -C "$src" archive "$KMS_SHA" | tar -x -C "$work"
    source_desc="$KMS_REPO at $KMS_SHA (tree $tree)" ;;
  *) refuse "KMS_SOURCE must be repo or github" ;;
esac
git -C "$work" init -q
[ "$(git -C "$work" rev-parse --show-toplevel)" = "$(cd "$work" && pwd -P)" ] || refuse "$work is not its own git top level"
git -C "$work" add -A

# 2. The patch series, in SERIES order. Every SERIES entry must exist and every .patch must be listed.
series=()
case "$name" in
  fixed) series=(bugfix) ;;
  research) series=(bugfix usability) ;;
esac
applied_json="["; sep=""
for s in ${series[@]+"${series[@]}"}; do
  dir="$ROOT/experiment-patches/$s"
  [ -f "$dir/SERIES" ] || refuse "experiment-patches/$s/SERIES missing"
  listed="$(sed 's/#.*//; s/[[:space:]]//g' "$dir/SERIES" | grep -v '^$')"
  for p in "$dir"/*.patch; do grep -qxF "$(basename "$p")" <<<"$listed" || refuse "experiment-patches/$s/$(basename "$p") is not in its SERIES"; done
  while IFS= read -r p; do
    [ -f "$dir/$p" ] || refuse "experiment-patches/$s/SERIES names $p, which does not exist"
    git -C "$work" apply --index "$dir/$p" || refuse "experiment-patches/$s/$p does not apply"
    echo "applied $s/$p"
    applied_json+="$sep{\"file\":\"experiment-patches/$s/$p\",\"sha256\":\"$(shasum -a 256 "$dir/$p" | awk '{print $1}')\"}"
    sep=","
  done <<<"$listed"
done
applied_json+="]"
[ "$name" = baseline ] || [ "$applied_json" != "[]" ] || refuse "$name has no patches"
patched_tree="$(git -C "$work" write-tree)"
# The patched tree must equal the tree of this repository's tag for that build, when the tag is there.
tag=""; case "$name" in baseline) tag=upstream-v0.1.2 ;; fixed) tag=fixed-v0.1.2 ;; research) tag=research-v0.1.2 ;; esac
if want="$(git -C "$ROOT" rev-parse "$tag^{tree}" 2>/dev/null)"; then
  [ "$patched_tree" = "$want" ] || refuse "patched tree $patched_tree differs from tag $tag ($want)"
  echo "tree $patched_tree = tag $tag"
fi

# 3. Build.
go_version="$(cd "$work" && GOTOOLCHAIN=local go version)"
[ "$go_version" = "$MEASURED_GO" ] || echo "note: $go_version is not the measured toolchain ($MEASURED_GO); the binary hash will differ from the one in EXPERIMENT.md" >&2
goos="$(cd "$work" && GOTOOLCHAIN=local go env GOOS)"; goarch="$(cd "$work" && GOTOOLCHAIN=local go env GOARCH)"
pkg="$(cd "$work" && GOTOOLCHAIN=local go list -m)"
ldflags="-s -w -X '$pkg/pkg/version.CommitHash=$KMS_SHA' -X '$pkg/pkg/version.Version=$KMS_TAG-$name' -X '$pkg/pkg/version.BuildTime=$KMS_COMMIT_DATE' -X '$pkg/pkg/version.BinaryName=kubernetes-mcp-server' -X '$pkg/pkg/version.WebsiteURL=https://github.com/containers/kubernetes-mcp-server'"
(cd "$work" && GOTOOLCHAIN=local GOFLAGS=-mod=readonly CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$out/kubernetes-mcp-server" ./cmd/kubernetes-mcp-server)

bin_sha="$(shasum -a 256 "$out/kubernetes-mcp-server" | awk '{print $1}')"
if [ "$name" != baseline ] && [ -f "$HB/out/baseline/BUILD.json" ]; then
  grep -q "\"binary_sha256\": \"$bin_sha\"" "$HB/out/baseline/BUILD.json" && refuse "$name is byte-identical to baseline"
fi
cat > "$out/BUILD.json" <<EOF
{
  "name": "$name",
  "repo": "$KMS_REPO",
  "tag": "$KMS_TAG",
  "commit": "$KMS_SHA",
  "source": "$source_desc",
  "patched_tree": "$patched_tree",
  "patches": $applied_json,
  "go_version": "$go_version",
  "go_mod_go": "$KMS_GO",
  "goos": "$goos",
  "goarch": "$goarch",
  "flags": {"GOTOOLCHAIN": "local", "GOFLAGS": "-mod=readonly", "CGO_ENABLED": "0", "trimpath": true, "buildvcs": false, "build_time": "$KMS_COMMIT_DATE", "version_string": "$KMS_TAG-$name"},
  "binary_sha256": "$bin_sha",
  "built_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
rm -rf "$work"
echo "ok: harness/build/out/$name/kubernetes-mcp-server ($goos/$goarch) sha256 $bin_sha"
