# Terminal resource provider protocol

**Status:** v1 — frozen 2026-08-17
**Protocol identifier:** `sidecar.terminal-resource/v1`
**Related:** [the plan](../plans/implemented/terminal-resource-providers.md)

A terminal resource provider is an explicitly configured local executable that
teaches Sidecar to recognize a resource key in terminal output and to turn that
key into a typed, read-only document. Sidecar owns matching, link safety, the
pane, and rendering. The provider owns service-specific rules, authentication,
and network access.

This document is the contract. It is language-agnostic: any executable that can
read one JSON object from stdin and write one JSON object to stdout can be a
provider.

This version was drafted, implemented twice — once by the host, once by the
reference `sidecar-jira` provider against a live Jira Cloud site — and revised
from what both implementations found before being frozen. Changes after this
point require a new protocol identifier.

**This contract is a subset of the Sidecar plugin protocol.** The plugin
protocol, `sidecar.plugin/v1-draft`, is this one grown rather than replaced: the
same invocation model, environment allowlist, process-group rules, sanitization,
limits, error codes, and matcher rules, plus three methods (`list`, `get`,
`act`), a `sections` field on the resource object, and declarations of the
collections and actions a plugin offers. One host runs both — an instance
configured under `terminalResources.providers` is dispatched with the identifier
above, one configured under `plugins.external` with the newer one, and the
dialect comes from the config section rather than from anything the executable
says. A provider that answers only this protocol keeps working exactly as
described here and needs no change. Its contract is
[docs/reference/plugin-protocol.md](plugin-protocol.md), and `sidecar plugin` is
its CLI; `sidecar terminal-links` remains the surface for this section.

## Invocation model

Sidecar runs the configured argv directly — no shell, no `PATH` interpolation of
arguments — and:

1. writes exactly one JSON request object to the child's stdin, then closes it;
2. reads stdout to completion, expecting exactly one JSON object;
3. drains stderr to EOF concurrently and discards it, retaining only a byte
   count;
4. waits for the child and reaps it.

A valid typed success **or** typed failure response exits `0`. Any of the
following is a *transport* failure attributed to the provider, not to the
service:

- non-zero exit;
- malformed JSON, no JSON, or more than one top-level JSON value on stdout —
  including a trailing log line after the object;
- stdout exceeding the response byte limit;
- exceeding the timeout;
- a missing or mismatched `protocol` field;
- a `resource` object missing `identity` or `title`.

Every invocation runs in its own process group. **The group ID is the child's
PID by construction** — Sidecar sets it when spawning and never looks it up
later, because by the time a forked descendant is holding the invocation open
the direct child is typically a zombie and `getpgid` is unreliable. On timeout
or cancellation Sidecar kills the group, so forked descendants die with it,
finishes draining, and waits. Sidecar never signals a process outside the group
it created.

Providers are short-lived in v1. There is no handshake, no framing, no
long-running server, and no request multiplexing.

### Execution environment

- **Working directory:** a neutral Sidecar config directory. Never the selected
  repository.
- **Environment:** only the following, when present in Sidecar's own
  environment, plus any variables named in the instance's `passEnv`:
  - `PATH`, `HOME`, `TMPDIR`
  - locale: `LANG`, `LC_*`
  - `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME`
  - proxy and CA: `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `http_proxy`,
    `https_proxy`, `no_proxy`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `GIT_SSL_CAINFO`

  Deliberately excluded, and not an oversight: `TERM`, `SHELL`, `USER`,
  `LOGNAME`, `XDG_RUNTIME_DIR`, `SSH_AUTH_SOCK`, and everything else. A provider
  is not running in a terminal and must not infer one.

  **The base set wins over `passEnv`.** Naming `PATH` in `passEnv` cannot change
  the child's `PATH`.

  Sidecar never accepts inline secret values in configuration and never logs or
  renders a passed value.
- **stdin:** exactly one JSON object, then EOF.
- **stdout:** exactly one JSON object. Nothing else — not a banner, not a
  progress line, not a trailing log.
- **stderr:** free-form. Sidecar drains it to EOF and discards it, recording
  only a byte count. It is never surfaced in a pane, a toast, a log, a
  diagnostic, or a crash report. Provider authors reproduce failures by running
  the provider CLI or `sidecar terminal-links check` deliberately.

  Sidecar does **not** stop reading stderr at a byte limit. Stopping the drain
  would deadlock a chatty provider against a full pipe.

### Methods

| Method | When it runs | May do network I/O |
| --- | --- | --- |
| `describe` | asynchronously after Sidecar's first ready frame, and whenever provider configuration changes or the user rechecks | no |
| `resolve` | on click, explicit refresh, `sidecar open --provider`, or `terminal-links check --resolve` | yes |

Matching itself never starts a process and never performs I/O.

### Request envelope

Every request carries:

| Field | Present on | Meaning |
| --- | --- | --- |
| `protocol` | both | Exactly `sidecar.terminal-resource/v1`. A provider that does not support the value must return an `invalid_request` error naming what it does support. This is the version-negotiation seam. |
| `method` | both | `describe` or `resolve`. An unrecognized method must return `invalid_request`, not a crash. |
| `instance` | both | The configured instance ID. Informational: a provider **may** use it to select provider-side configuration, but argv selection takes precedence and a provider must behave correctly if it ignores `instance` entirely. |
| `host` | `describe` only | `{name, version}`. Not sent on `resolve`. |
| `deadlineMs` | both | Milliseconds the host will wait before killing the process group. Advisory but accurate. A provider **should** budget its own I/O inside this and return a typed `unavailable` rather than be killed — a typed timeout gives the user a real error card and a working Retry; being SIGKILLed gives them an opaque transport failure. |

### Response shape

A response is **exactly one of** three shapes. Anything else — mixed, empty, or
containing more than one — is a transport failure:

1. a describe result (`provider` + `matchers`),
2. a resource result (`resource`),
3. a typed error (`error`).

Every response also carries `protocol`.

## `describe`

Reports what the provider is and what resource keys it can recognize.
`describe` must be local, fast, and non-interactive. It may read the provider's
own configuration to build instance-specific patterns. It must not prompt for a
credential or contact the network.

### Request

```json
{
  "protocol": "sidecar.terminal-resource/v1",
  "method": "describe",
  "instance": "jira-work",
  "deadlineMs": 5000,
  "host": {"name": "sidecar", "version": "0.0.0"}
}
```

### Response

```json
{
  "protocol": "sidecar.terminal-resource/v1",
  "provider": {
    "kind": "jira",
    "name": "Jira",
    "version": "1.2.0",
    "docsUrl": "https://example.test/sidecar-jira/setup"
  },
  "matchers": [
    {
      "id": "issue-key",
      "pattern": "\\b(?:CASH|GRES|AVATAXUI)-[1-9][0-9]*\\b",
      "priority": 100
    }
  ]
}
```

- `provider.kind` and `provider.name` are informational display strings. They
  cannot rename or collide with a configured instance ID.
- `provider.docsUrl` is optional. It must pass the same `http`/`https`
  validation as `sourceUrl` and is the only executable-declared Setup action
  Sidecar will follow — and only after user confirmation.
- `matchers[].id` is stable and unique within the provider. It is stored in
  persisted resource references, so changing it orphans saved tabs.
- `matchers[].pattern` is a Go/RE2 regular expression. The **whole match** is
  the locator; there are no capture-group templates and no provider code runs in
  the scanner.
- `matchers[].priority` is optional (default `0`). Higher runs earlier within a
  provider.

A provider that is installed but not yet configured returns a typed
`invalid_config` error with a useful `setupHint` — not an empty matcher list and
not a crash.

### Matcher rules enforced by Sidecar

- RE2 syntax only, guaranteeing linear-time matching.
- Matching is case-sensitive unless the pattern opts into RE2 flags.
- Built-in matchers (URL, file, td issue, git diff) keep precedence. External
  matchers run afterward in ascending configured-provider order, then descending
  priority, then matcher ID. The single exception is host-side `claimHosts`
  configuration — see "Configuration".
- Overlaps are resolved first-wins through the same visual-column overlap
  function the existing scanner uses.
- Pattern count, pattern length, matches per line, locator length, and total
  provider count are bounded. See "Limits".

Sidecar validates and compiles the whole matcher set before publishing a new
immutable snapshot. Validation is all-or-nothing per provider: a single invalid
pattern, a duplicate matcher ID, an over-limit set, or a matcher ID that would
not survive sanitization **refuses the entire describe result**. A matcher ID is
never rewritten to make it acceptable, because a rewritten ID would orphan saved
tabs the next time the provider sends the original.

What happens to a provider's existing matchers depends on who has authority:

| Outcome of `describe` | Effect on that provider's live matchers |
| --- | --- |
| Success | Replaced by the new set |
| Typed `error` response, provider disabled, or config entry removed | Removed — the provider authoritatively said it has none |
| Transport failure, or a returned set that fails validation | **Kept** for the remainder of the process, and the failure is reported — the host has no authoritative new answer, so it does not discard a working one |

Relaunch always starts clean. In every case, armed resource tabs the user has
open are preserved; only an explicit close or a confirmed cleanup removes them.

## `resolve`

Turns one locator into one document.

### Request

```json
{
  "protocol": "sidecar.terminal-resource/v1",
  "method": "resolve",
  "instance": "jira-work",
  "deadlineMs": 10000,
  "params": {
    "matcher": "issue-key",
    "locator": "CASH-1245"
  }
}
```

The request deliberately carries no terminal line, no scrollback, no tmux
target, no environment, and no repository path. Widening it requires a named
capability and an explicit per-instance permission, not a silent field addition.

### Success response

```json
{
  "protocol": "sidecar.terminal-resource/v1",
  "resource": {
    "identity": "CASH-1245",
    "title": "Refund totals differ after partial capture",
    "subtitle": "Bug",
    "status": {"label": "In Progress", "tone": "info"},
    "fields": [
      {"label": "Assignee", "value": "Marcus Vorwaller", "kind": "user"},
      {"label": "Priority", "value": "High"}
    ],
    "body": {"format": "markdown", "text": "Ticket description..."},
    "sourceUrl": "https://jira.example.test/browse/CASH-1245",
    "updatedAt": "2026-08-17T17:31:00Z",
    "freshForSeconds": 60
  }
}
```

| Field | Required | Meaning |
| --- | --- | --- |
| `identity` | yes | Provider-stable canonical ID. If it differs from the locator, Sidecar re-keys the tab and merges it with an already-open canonical tab. |
| `title` | yes | Primary display line. |
| `subtitle` | no | Secondary line, e.g. resource type. |
| `status` | no | `{label, tone}`. |
| `fields` | no | Ordered `{label, value, kind}` pairs rendered as a bounded grid. |
| `body` | no | `{format, text}`; `format` is `markdown` or `text`. Unknown formats coerce to `text`. |
| `sourceUrl` | no | `http`/`https` only. The single action that can open a URL in v1. |
| `updatedAt` | no | RFC 3339. Unparseable values are dropped, not an error. |
| `freshForSeconds` | no | Provider's freshness hint, clamped by the host. |

Missing `identity` or `title` is a protocol violation, reported as a transport
failure rather than rendered as a blank card. A response never changes its own
provider instance.

**Do not duplicate `updatedAt` as a field.** The host owns rendering it, and can
render it relatively ("3 days ago") where a raw RFC 3339 string in a bounded
grid helps nobody.

#### Field kinds

`kind` is optional and defaults to `text`. It tells the host how to present a
value; it never changes validation.

| `kind` | Meaning |
| --- | --- |
| `text` | Opaque string, rendered verbatim |
| `timestamp` | RFC 3339; the host may render it relatively |
| `user` | A person's display name |

Unknown kinds coerce to `text`.

#### Status tones

| `tone` | Intended for |
| --- | --- |
| `neutral` | Not started, backlog, unknown |
| `info` | In progress, active |
| `success` | Done, merged, passing |
| `warning` | Flagged, blocked, stale, degraded |
| `danger` | Failed, rejected, incident |

Unknown tones coerce to `neutral`. Tones must mean the same thing across
providers or the colors mean nothing, so map from the service's own *category*
rather than its display label. The reference mapping, from Jira's three status
categories: `new` → `neutral`, `indeterminate` → `info`, `done` → `success`.

`label` passes through verbatim — `In Progress`, not `IN PROGRESS`. Casing is a
host display decision.

### Typed failure response

```json
{
  "protocol": "sidecar.terminal-resource/v1",
  "error": {
    "code": "unauthorized",
    "message": "Jira credentials are missing or expired",
    "retryable": false,
    "setupHint": "Run sidecar-jira configure --profile work"
  }
}
```

Stable v1 codes:

| Code | Meaning | Default `retryable` |
| --- | --- | --- |
| `not_found` | The locator does not exist or is not visible | `false` |
| `unauthorized` | Missing, expired, or rejected credentials | `false` |
| `forbidden` | Authenticated but not permitted | `false` |
| `rate_limited` | Throttled upstream | `true` |
| `invalid_config` | The provider is not configured correctly | `false` |
| `invalid_request` | The host sent something this provider cannot process: unsupported `protocol`, unknown `method`, missing or malformed `params`, unknown matcher ID | `false` |
| `unavailable` | Network or upstream service failure, including the provider's own timeout | `true` |
| `internal` | An unexpected provider-side fault | `true` |

Unknown codes map to `internal`.

**The per-response `retryable` field is authoritative.** The column above is a
default for providers to start from, not a value the host infers from the code.
Set it honestly: offering a Retry button for something that will fail
identically forever is worse than offering none.

`message` is display text, not control flow. `setupHint` must be a **single
line** — it is rendered in a bounded grid — and is displayed as copyable text
only. Sidecar never executes it.

## Limits

Sidecar enforces these before any provider data reaches view state. The
authoritative values live in `internal/resource/limits.go`.

| Bound | Default |
| --- | --- |
| Response bytes (stdout) | 256 KiB |
| Body text bytes | 64 KiB |
| Field count | 24 |
| Field label / value length | 64 / 512 chars |
| Title / subtitle length | 300 / 120 chars |
| Status label length | 64 chars |
| Identity / locator length | 200 chars |
| URL length | 2048 chars |
| Error message / setup hint length | 512 / 512 chars |
| Provider kind / name / version length | 64 chars each |
| Instance ID / matcher ID length | 64 chars each |
| Matchers per provider | 32 |
| Pattern length | 512 chars |
| Matches per terminal line | 32 |
| Configured providers | 16 |
| `freshForSeconds` | default 60 when absent or `0`; clamped to [10, 900] |
| `describe` timeout | 5s |
| `resolve` timeout | 10s (configurable, clamped to 60s) |

### Over-limit behavior: the host truncates, the provider need not

A provider is **not** required to pre-truncate to these numbers, and the exact
values are deliberately not sent in the request. Sidecar truncates rather than
refusing:

- body text over the limit is cut at a UTF-8 boundary and marked as truncated;
- fields beyond the count limit are dropped; over-long labels and values are cut;
- over-long titles, subtitles, status labels, and messages are cut.

Only the total stdout byte limit is a hard refusal, because the host has to
bound what it reads before it can parse anything, and only the structural
violations listed under "Invocation model" reject a response outright.

The reasoning: truncating a slightly-too-long document still shows the user
their ticket. Refusing it shows them an error for a document that was almost
entirely fine.

All strings must be valid UTF-8. Invalid sequences are replaced. C0/C1 control
characters other than `\n` and `\t` in body text are stripped; all control
characters are stripped from single-line fields. A `sourceUrl` or `docsUrl` that
does not survive validation unchanged is **dropped**, not repaired — the
document still renders, just without a source action. Sidecar never opens a
guess at what a provider meant.

Unknown JSON fields are ignored for forward compatibility.

## Safety posture

- Provider text never becomes ANSI. Sidecar strips OSC from provider strings the
  same way it strips OSC from terminal text.
- The shared Markdown renderer is **not** a trust boundary. Two distinct
  problems, both verified empirically:
  1. Parsed Markdown links synthesize OSC-8 hyperlinks even when the input bytes
     contain no escapes. `[x](javascript:alert(1))` renders as an *active*
     hyperlink carrying the raw `javascript:` destination.
  2. The renderer linkifies bare URLs and prints the destination as visible text
     beside the label. So "rewrite links to plain visible text" is necessary but
     **not sufficient** on its own.

  Therefore: before rendering, a resource-specific sanitizer drops raw HTML,
  reduces images to inert alt text, and rewrites links and autolinks to plain
  visible text with no destination node. After rendering, all OSC is stripped
  again. That second strip is load-bearing, not merely defense in depth.
- The separately typed and validated `sourceUrl` is the only resource action
  that can open a URL in v1.
- A process boundary is crash isolation, **not a sandbox**. Enabling a provider
  trusts that executable with the user's full OS privileges. Sidecar never
  discovers, installs, auto-enables, or upgrades a provider, and a repository
  can never cause a provider to run merely by being opened.
- Debug logs record provider instance, method, duration, outcome code, and byte
  counts. Never the locator, title, body, URL, credentials, stdout, or stderr.

## Configuration

Providers are configured explicitly in Sidecar's app-level config:

```json
{
  "terminalResources": {
    "providers": [
      {
        "id": "jira-work",
        "command": ["sidecar-jira", "sidecar-provider", "--profile", "work"],
        "passEnv": ["JIRA_API_TOKEN"],
        "enabled": true,
        "timeout": "10s",
        "claimHosts": ["avalara.atlassian.net"]
      }
    ]
  }
}
```

- `id` is unique, non-empty, and stable; it is the persisted provider key.
- `command` is an argv array executed without a shell. The first element may be
  an absolute path or resolve through `PATH`.
- `passEnv` names variables whose *current values* are inherited. Inline secret
  values are not supported, and the base environment wins on conflict.
- Array order is matcher precedence.
- `claimHosts` is host-side configuration, described below. It is never a
  protocol field — a provider does not know it exists and cannot request it.

### `claimHosts`: letting a provider take back its own links

A URL is a built-in match, and built-ins keep precedence. `claimHosts` is the
one narrow exception: it names the hostnames whose URLs *this instance* may
reclassify into its own resource cards instead of opening the browser.

Both of these must hold before anything is reclassified:

1. The URL's host equals one of this instance's `claimHosts` entries. Entries
   are bare hostnames — no scheme, no port, no path, no wildcard — matched
   case-insensitively and exactly. `github.com` does not claim
   `gist.github.com`.
2. This same instance's matcher matches, **in full**, either the whole URL or —
   on a frame Sidecar's own Markdown renderer drew — the rendered link label. A
   partial or prefix match keeps the browser link.

The label branch is what makes Jira work. `sidecar-jira`'s matcher is issue-key
shaped, so it can never match a whole browse URL, and a brief normally writes
the key as a Markdown link:

```markdown
See [ZMS-37161](https://avalara.atlassian.net/browse/ZMS-37161) for the ticket.
```

With `claimHosts: ["avalara.atlassian.net"]` configured, clicking that label in
a rendered Files preview, a note, or a document pane opens the Resource card.
Remove the entry and it goes back to opening the browser.

The label branch applies **only** to frames Sidecar rendered. A program writing
to a terminal can print any label over any destination, so terminal hyperlinks
always mean what their destination says. A rendered pane too narrow for the
Markdown renderer is showing wrapped source rather than rendered output, and
counts as source for the same reason.

Reclassifying does not take the browser away: the label keeps the emulator
hyperlink to its original destination, so cmd-click still opens it.

`claimHosts` is per instance. Two Jira sites configured as two instances claim
only their own hosts, and an instance with no `claimHosts` claims nothing.

## Headless verification

```bash
sidecar terminal-links list --json               # config + PATH resolution, no subprocess
sidecar terminal-links list --describe --json    # opt in to running describe
sidecar terminal-links check jira-work --json    # config + resolution + describe
sidecar terminal-links check jira-work --resolve CASH-1245 --json
```

`list` deliberately spawns nothing by default, so it stays safe to run in a
loop. `check` always describes. `--resolve` is a separate explicit flag because
it can perform network access and print private resource data. These commands
dispatch before any TUI, tmux, state, or log setup.

## Fixtures

Canonical request/response JSON lives at the stable path
`internal/pluginhost/testdata/protocol/` and may be vendored by external
provider authors implementing this contract.

**That path moved after this document was frozen.** It was published here as
`internal/resourceprovider/testdata/protocol/`; renaming that package to
`internal/pluginhost` moved it, which broke the stability promise for anyone who
had followed it. The path above is the current one and no further move is
planned.

The reference fixture executable is
`internal/pluginhost/testdata/fixtureprovider`, which describes
`CASH|GRES|AVATAXUI` and resolves deterministic synthetic documents with no
network access and no credentials. It also simulates the hostile cases —
malformed output, oversize output, hanging, crashing, extra stdout, incompatible
protocol, duplicate matcher IDs, invalid RE2, stderr flooding, and forking a
descendant that outlives its parent — so the host's bounded-failure guarantees
are tested against a real process rather than an in-memory fake.

Every fixture resolve response carries an unknown top-level field, so
forward-compatibility is exercised on every run rather than in a single test.

The reference provider implementation is
[`sidecar-jira`](https://github.com/marcus/sidecar-jira). It is not bundled with
Sidecar and Sidecar does not depend on it.
