"""Runtime harness for the bundled Hermes asset.

Everything pinned here is invisible to a test that drives the pure gate, and the
whole runtime half of the asset sits in that blind spot: a replay test can call
report_session and build_args directly, so it never touches which hook names the
plugin registers, never touches the shape Hermes hands a callback, and would
pass unchanged if the asset registered a hook called "on_sessions_start", or
read `sessionId`, or stopped reading `platform` at all. Each of those is a silent
failure in production: the plugin installs, loads, and reports nothing.

So this harness calls the real register(), drives real hook payloads through the
callbacks it registered, and reports three things:

  - the hook names the asset actually registered;
  - the exact argv every report process was spawned with;
  - the order those processes completed in.

The payloads are the ones Hermes really passes, taken from a recorded trace of
hermes 0.17.0 rather than from its documentation: on_session_start arrives with
model, platform and session_id, and pre_llm_call arrives a few milliseconds
later with the same session_id. An asset that did not suppress the repeat would
record two spawns for one session here, and the recorded order would say so.

Usage: argv-harness.py <stub-path> <order-log-path> <argv-dir>
"""

from __future__ import annotations

import importlib.util
import json
import os
import stat
import sys
from pathlib import Path

STUB = """#!/bin/sh
label=""
prev=""
for a in "$@"; do
  case "$prev" in
    --id) label="$a" ;;
  esac
  prev="$a"
done
if [ -z "$label" ]; then label="no-id"; fi
printf '%s\\n' "$@" > "$SIDECAR_ARGV_DIR/$label"
echo "$label" >> "$SIDECAR_ORDER_LOG"
"""


class StubContext:
    """The minimum of Hermes's PluginContext the asset touches.

    Hermes hands register() a facade with two dozen register_* methods; this
    asset calls exactly one of them, and a harness that offered more would be
    asserting a contract the asset does not depend on.
    """

    def __init__(self):
        self.hooks = {}

    def register_hook(self, name, callback):
        self.hooks.setdefault(name, []).append(callback)


def main(argv):
    if len(argv) != 3:
        print("usage: argv-harness.py <stub-path> <order-log-path> <argv-dir>", file=sys.stderr)
        return 2
    stub, order_log, argv_dir = argv
    Path(argv_dir).mkdir(parents=True, exist_ok=True)
    Path(stub).write_text(STUB, encoding="utf-8")
    os.chmod(stub, os.stat(stub).st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    os.environ["SIDECAR_MANAGED_SHELL"] = "1"
    os.environ["SIDECAR_BIN"] = stub
    os.environ["SIDECAR_ORDER_LOG"] = order_log
    os.environ["SIDECAR_ARGV_DIR"] = argv_dir

    here = Path(__file__).resolve().parent
    spec = importlib.util.spec_from_file_location("sidecar_hermes_asset", here / "__init__.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)

    ctx = StubContext()
    module.register(ctx)
    if not ctx.hooks:
        print("register() subscribed to nothing; Hermes would never call the asset", file=sys.stderr)
        return 2

    def fire(name, **kwargs):
        for callback in ctx.hooks.get(name, []):
            callback(**kwargs)

    # One session, started and then observed at its first turn. Upstream reports
    # both; this asset reports the first and suppresses the second.
    fire("on_session_start", session_id="ses_argvharness", platform="cli", model="a-model")
    fire(
        "pre_llm_call",
        session_id="ses_argvharness",
        platform="cli",
        model="a-model",
        user_message="never read by the asset",
        conversation_history=[],
        is_first_turn=True,
    )
    # A gateway turn, which the platform gate refuses outright.
    fire("pre_llm_call", session_id="ses_gateway", platform="telegram", model="a-model")
    # /new rotates the session in place, and the binding has to move with it.
    fire("on_session_reset", session_id="ses_rotated", platform="cli", reason="new_session")

    order = []
    if Path(order_log).exists():
        order = [line for line in Path(order_log).read_text(encoding="utf-8").split("\n") if line]
    recorded = {}
    for label in order:
        path = Path(argv_dir) / label
        recorded[label] = (
            [a for a in path.read_text(encoding="utf-8").split("\n") if a] if path.exists() else []
        )

    sys.stdout.write(json.dumps({"order": order, "argv": recorded, "hooks": sorted(ctx.hooks)}))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
