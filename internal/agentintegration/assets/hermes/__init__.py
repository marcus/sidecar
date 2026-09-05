# sidecar-integration: id=sidecar.hermes.plugin schema=1 version=1
#
# The line above is what makes this file Sidecar's. The installer identifies an
# asset it may replace or remove by that marker and by nothing else -- not by
# its name, and not by where it sits. A directory called sidecar-agent-state
# whose __init__.py lacks the marker is somebody else's, and Sidecar refuses to
# touch it.
#
# Sidecar session-identity integration for Hermes Agent.
#
# Hermes discovers directory plugins under <hermes home>/plugins/<name>/, loads
# the ones named in `plugins.enabled` in config.yaml, and calls register(ctx)
# on each. A plugin subscribes to lifecycle hooks through ctx.register_hook, and
# the agent core calls invoke_hook(name, **kwargs) at the matching points.
#
# WHAT THIS SENDS
#
# One thing: Hermes's own session identifier, so a cold restart can offer
# `hermes --resume <that session>`. It sends no lane, no state, no reason code,
# no prompt text, no response text, no tool arguments, no file paths, and no
# environment values. There is no code path here that reads message content --
# `user_message` and `conversation_history` arrive in pre_llm_call's kwargs and
# are never touched.
#
# WHY SESSION IDENTITY ONLY
#
# Because that is what the upstream integration this was ported from does, and
# because a state ladder claimed without traces is a claim nobody qualified.
# Hermes has a wide hook surface -- pre_tool_call, post_tool_call,
# pre_approval_request, post_approval_response, on_session_end and a dozen more
# -- and every one of them is a ceiling rather than a promise until it has been
# captured. See internal/agentlifecycle/capabilities.json for the recorded gaps.
#
# PROVENANCE
#
# The provider half -- which hooks carry a session, the interactive-platform
# gate, and the session-id validation -- is ported from Herdr's hermes
# integration at HERDR_INTEGRATION_VERSION=5
# (internal/agentintegration/upstream/hermes/__init__.py). The transport half is
# Sidecar's own. See internal/agentintegration/portedfrom.go for the recorded
# provenance and the deliberate differences, each of which is also named where
# it happens below.

from __future__ import annotations

import os
import subprocess
import threading

SOURCE = "sidecar.hermes.plugin"
PROVIDER = "hermes"

# VERSION is the bundled asset version. It is NOT sent on a session report --
# report-session takes no --source-version -- and it is here because the marker
# line above and this constant have to move together, and a reader comparing an
# installed copy with the bundled one should not have to trust a comment.
VERSION = "1"

# REPORT_TIMEOUT_SECS bounds one report subprocess.
#
# THE FIRST DELIBERATE DIFFERENCE, and the reason is the transport. Upstream
# bounds its own spawn at one second, which is generous for a process that
# writes to a socket. Sidecar's `agent report-session` takes an exclusive lock
# on the managed-shell record before it writes, so it can legitimately wait on
# another pane's write, and one second would turn ordinary contention into a
# lost binding. Five seconds is what the three JavaScript assets Sidecar ships
# already bound a report at, for the same reason.
REPORT_TIMEOUT_SECS = 5

# INTERACTIVE_PLATFORMS is upstream's platform gate, kept verbatim.
#
# Hermes runs the same agent behind a Telegram, Slack, Discord or WhatsApp
# gateway as it does behind a terminal, and a gateway session is not the pane's
# conversation: binding it would offer to resume somebody's chat message as the
# pane's own work. A hook whose kwargs carry no platform at all is refused by
# the same rule, which is why the tool hooks are unreachable here even though
# they carry a session id -- see report_session.
INTERACTIVE_PLATFORMS = frozenset({"cli", "tui", "desktop", "acp"})


def build_args(session_id):
    """Return the exact argv one binding becomes.

    Direct argv, never a shell string: nothing Hermes produces is interpolated
    into a command line, and the session id is re-validated by Sidecar on the
    way in.

    NO --seq IS SENT, and there is no sequence parameter to pass. The verb does
    not take one; a session binding is not a lifecycle report and never reaches
    the sequenced store. The state reports Sidecar's other assets send omit it
    too, because the store assigns under the exclusive lock it already takes.
    """
    return [
        "agent",
        "report-session",
        "--kind",
        PROVIDER,
        "--source",
        SOURCE,
        "--id",
        session_id,
    ]


class _Reporter:
    """Holds the last session bound, and serializes the spawns.

    THE SECOND DELIBERATE DIFFERENCE lives here. Upstream reports the session
    again on every hook that carries one, so a single Hermes turn spawns the
    same binding twice: on_session_start fires, and pre_llm_call fires a few
    milliseconds later with the identical id. Measured, not read -- the
    recorded trace in internal/agentlifecycle/testdata/traces/hermes shows the
    pair. On upstream's socket transport a repeat costs a few bytes; here it is
    a process spawn that takes a file lock and tells Sidecar nothing new, so an
    exact repeat is suppressed. Herdr's own opencode asset at version 10
    already suppresses the identical case against a remembered id, and Sidecar's
    kilo and omp assets do the same.

    The lock is what makes the check and the spawn one step. Hermes invokes a
    hook on whichever thread reached the call site, and two threads that both
    read an empty `session` would both spawn.
    """

    def __init__(self):
        self._lock = threading.Lock()
        self._session = ""

    def report(self, session_id):
        """Bind session_id unless it is already bound. Never raises."""
        bin_path = os.environ.get("SIDECAR_BIN")
        if os.environ.get("SIDECAR_MANAGED_SHELL") != "1" or not bin_path:
            return
        with self._lock:
            if session_id == self._session:
                return
            self._session = session_id
            args = build_args(session_id)
        _spawn(bin_path, args)


def _spawn(bin_path, args):
    """Run one report and give up on it. Never raises, never returns anything.

    FAILING OPEN is the whole contract. Nothing in this file may delay, block or
    change what Hermes does beyond the bounded wait above, and a reporting
    failure is diagnostic rather than something the agent should ever see. Every
    exception is swallowed for that reason and for no other.
    """
    try:
        subprocess.run(
            [bin_path] + args,
            check=False,
            timeout=REPORT_TIMEOUT_SECS,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    except Exception:
        pass


_reporter = _Reporter()


def report_session(**kwargs):
    """The gate every hook goes through, kept verbatim from upstream.

    Two conditions, in upstream's order. The platform must be one Hermes drives
    interactively, and the session id must be a non-empty string. Upstream reads
    both off the kwargs and returns silently when either fails, which is what
    makes a hook Hermes invokes with a different payload shape a no-op rather
    than an error inside the agent loop.
    """
    if kwargs.get("platform") not in INTERACTIVE_PLATFORMS:
        return
    session_id = kwargs.get("session_id")
    if not isinstance(session_id, str) or not session_id:
        return
    _reporter.report(session_id)


def _session_started(**kwargs):
    """on_session_start: the first hook of a Hermes run.

    Upstream calls this start source "startup" and Sidecar drops the
    distinction, which is THE THIRD DELIBERATE DIFFERENCE and is forced rather
    than chosen: `report-session` records which conversation a pane is running
    and takes no start-source argument, so the three hooks below differ only in
    when they fire.
    """
    report_session(**kwargs)


def _session_reset(**kwargs):
    """on_session_reset: /new or /clear rotated the session in place.

    This is the hook the suppression above exists to let through: the id really
    has changed, so the pane's binding must move with it.
    """
    report_session(**kwargs)


def _session_observed(**kwargs):
    """pre_llm_call: a turn is about to run.

    Upstream restricts this one to platform == "cli" rather than to the
    interactive set, and the restriction is kept verbatim even though it is
    narrower than the gate above. It is the recovery path: a session Hermes
    started before this plugin loaded, or one whose on_session_start Sidecar
    missed, is still bound at the first turn.
    """
    if kwargs.get("platform") != "cli":
        return
    report_session(**kwargs)


def register(ctx):
    """Hermes's plugin entry point.

    Nothing is registered outside a Sidecar-managed shell. Upstream makes the
    same decision one level lower, inside its send function; doing it here means
    a Hermes running anywhere else carries no Sidecar callback at all, which is
    the strongest form of failing open available to a plugin.
    """
    if os.environ.get("SIDECAR_MANAGED_SHELL") != "1" or not os.environ.get("SIDECAR_BIN"):
        return
    ctx.register_hook("on_session_start", _session_started)
    ctx.register_hook("on_session_reset", _session_reset)
    ctx.register_hook("pre_llm_call", _session_observed)
