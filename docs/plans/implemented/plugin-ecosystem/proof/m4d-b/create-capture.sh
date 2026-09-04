#!/bin/bash
# One Create Workspace capture on the global Workspaces surface, from a fresh
# isolated run: BIN is the sidecar under test, RUNDIR its private run root.
set -euo pipefail
BIN="$1"; RUNDIR="$2"; OUT="$3"
cd /Users/marcus/code/sidecar-plugin-ux-m4d-b
rm -rf "$RUNDIR"
mkdir -p "$RUNDIR/config"
cp /tmp/m4db-proof/run/config/config.json "$RUNDIR/config/config.json"
D() { SIDECAR_DRIVE_RUN_DIR="$RUNDIR" ./scripts/tmux-drive.sh "$@"; }
SIDECAR_DRIVE_RUN_DIR="$RUNDIR" SIDECAR_BIN="$BIN" ./scripts/tmux-drive.sh start 160 45 >/dev/null
sleep 6
D keys 8 >/dev/null; sleep 3
D keys n >/dev/null; sleep 3
D snap create >/dev/null
cp "$RUNDIR/out/create.txt" "$OUT"
D stop >/dev/null
