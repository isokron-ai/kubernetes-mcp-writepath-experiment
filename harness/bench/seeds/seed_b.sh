#!/usr/bin/env bash
# Set B (friction-derived F1-F9) seeds for the Phase 4 benchmark: the native sessions' seeds, run unchanged through
# copies whose only differences are the namespace check (mcp-<bench run id> instead of mcp-nx-JJ / mcp-ny-JJ) and the
# run label (bench-session), so `seed.sh cleanup` and the label select this series' namespaces:
#   bench/seeds/native-seed.sh  = usability/native/seed.sh   (diff: 2 lines; bench/drive.mjs check shows it)
#   bench/seeds/native2-seed.sh = usability/native2/seed.sh  (diff: 2 lines)
# F1b (new in set B) starts from an empty namespace with the kms bindings, as job 01 does.
#   seed_b.sh TASK NS      TASK = F1a F1b F2 .. F9; NS = mcp-<run id> (fresh)
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
case "${1:-}" in
  F1a|F1b) exec "$HERE/native-seed.sh" 01 "$2" ;;
  F2) exec "$HERE/native-seed.sh" 07 "$2" ;;
  F3) exec "$HERE/native-seed.sh" 08 "$2" ;;
  F4) exec "$HERE/native-seed.sh" 10 "$2" ;;
  F5) exec "$HERE/native2-seed.sh" 13 "$2" ;;
  F6) exec "$HERE/native2-seed.sh" 14 "$2" ;;
  F7) exec "$HERE/native2-seed.sh" 15 "$2" ;;
  F8) exec "$HERE/native2-seed.sh" 16 "$2" ;;
  F9) exec "$HERE/native2-seed.sh" 17 "$2" ;;
  *) echo "seed_b.sh: unknown task '${1:-}'" >&2; exit 2 ;;
esac
