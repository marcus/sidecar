"""Replays a fixture through the bundled Hermes asset and prints the argv it builds.

This is the equivalence half of the Hermes suite. The Go mirror in hermes.go is
what every other test in the package drives, and a mirror that has drifted from
the shipped plugin is worse than no mirror at all: it agrees with itself while
the file a user actually runs does something else. So this harness runs the real
register(), fires the real callbacks, and reports the exact argv each spawn would
have used. TestBundledHermesAssetBehavesLikeTheGate requires the two lists to
match element for element.

Nothing is spawned. SIDECAR_BIN is set to a path that does not exist and the
subprocess call is replaced with a recorder, because the point here is the argv
rather than the process; assets/hermes/argv-harness.py is the one that really
spawns, and it is what proves the runtime half.

Fixture rows are `hook<TAB>platform<TAB>session`, with `-` for a kwarg Hermes
does not pass. Comment lines start with `#`.

Usage: replay-harness.py <fixture-path>
"""

from __future__ import annotations

import importlib.util
import json
import os
import sys
from pathlib import Path


def main(argv):
    if len(argv) != 1:
        print("usage: replay-harness.py <fixture-path>", file=sys.stderr)
        return 2
    fixture = Path(argv[0])

    os.environ["SIDECAR_MANAGED_SHELL"] = "1"
    os.environ["SIDECAR_BIN"] = "/nonexistent/sidecar"

    here = Path(__file__).resolve().parent
    spec = importlib.util.spec_from_file_location("sidecar_hermes_asset", here / "__init__.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)

    spawned = []
    module._spawn = lambda bin_path, args: spawned.append(list(args))

    class StubContext:
        def __init__(self):
            self.hooks = {}

        def register_hook(self, name, callback):
            self.hooks.setdefault(name, []).append(callback)

    ctx = StubContext()
    module.register(ctx)

    for line in fixture.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        cols = line.split("\t")
        if len(cols) != 3:
            print("malformed fixture row: %r" % (line,), file=sys.stderr)
            return 2
        hook, platform, session = (c.strip() for c in cols)
        kwargs = {}
        if platform != "-":
            kwargs["platform"] = platform
        if session != "-":
            kwargs["session_id"] = session
        for callback in ctx.hooks.get(hook, []):
            callback(**kwargs)

    sys.stdout.write(json.dumps({"argv": spawned, "hooks": sorted(ctx.hooks)}))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
