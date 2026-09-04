# Sidecar plugin protocol

**Status:** draft — implemented by the host, not yet frozen
**Protocol identifier:** `sidecar.plugin/v1-draft`
**Related:** [Terminal resource provider protocol](terminal-resource-provider-protocol.md) is the frozen v1 this grows from · [Creating a Sidecar plugin](../guides/active/creating-plugins.md) is the authoring guide · [the plan set](../plans/active/plugin-ecosystem/README.md) holds the design rationale and the revisions still pending

A Sidecar plugin is an explicitly configured local executable that gives Sidecar content to render and typed actions to offer. Sidecar owns rendering, keys, focus, tabs, persistence, theme, and safety. The plugin owns its data, its rules, its credentials, and its network access.

This document is the contract, and it is the only authority for it. It is language-agnostic: any executable that can read one JSON object from stdin and write one JSON object to stdout can be a plugin.

The plugin never sends a user interface. It sends content in a small declarative vocabulary Sidecar knows how to draw well: collections of rows, documents with fields and sections, search results with excerpts, and actions with a few typed inputs. The vocabulary is deliberately domain-shaped rather than a generic widget tree; the reasoning is in the plan's [Why not a generic UI catalog](../plans/active/plugin-ecosystem/protocol.md#why-not-a-generic-ui-catalog).

Every response is data. Plugin text never becomes ANSI, never binds a key, never chooses a colour, and never opens a URL except through the separately validated `sourceUrl`.

## Draft status: what is settled and what is not

Everything described here is implemented and enforced by the host today, and `sidecar plugin check` will hold a plugin to it. What is not settled is the identifier: while the protocol is a draft it is `sidecar.plugin/v1-draft`, and a host may refuse that value at any point. It freezes as `sidecar.plugin/v1` the way `sidecar.terminal-resource/v1` did — the host has implemented it, one real external plugin (recall) implements it against a live tool, and both revise from what the other found.

Revisions the M0 mockups and the recall implementation surfaced are listed, with their proposed shapes, in the plan README's [pending revisions table](../plans/active/plugin-ecosystem/README.md#protocol-revisions-pending-from-the-m0-recall-mockup). The four that milestone M4b applied — `filters[]`, `page.omitted`, the `failed` outcome, and `page.coverage[]` — are described here and implemented by the host; everything still in that table is not, and nothing in this document describes it. A plugin written against what is here keeps working when the rest land, because each is an additive field.

A plugin that answers only `sidecar.terminal-resource/v1` is not affected by any of this. That protocol is frozen, its contract is [its own reference](terminal-resource-provider-protocol.md), and a provider written against it keeps working unchanged.

## Invocation model

Unchanged from the resource protocol, restated for one reason: a plugin author reading this document alone must not be surprised.

- Sidecar runs the configured argv directly. No shell, no `PATH` interpolation of arguments. The working directory is a neutral Sidecar config directory, never the selected repository.
- One JSON request object on stdin, then EOF. Exactly one JSON object on stdout. stderr is drained to EOF and discarded, retaining only a byte count; it never reaches a pane, a toast, or a log.
- Every invocation is its own process group, killed as a group on timeout or cancel, so a forked descendant dies with it. Sidecar never signals a process outside the group it created.
- A typed success **or** typed failure exits `0`. Non-zero exit, malformed JSON, no JSON, more than one top-level JSON value (a trailing log line counts), oversize stdout, timeout, or a missing or mismatched `protocol` field is a *transport* failure attributed to the plugin rather than to the service behind it.
- The environment is the documented allowlist plus the instance's `passEnv`, plus `SIDECAR_PLUGIN=1`. The marker is set by the host rather than inherited, so a tool whose ordinary CLI and whose plugin subcommand share a binary can tell which one it is running as. It is added only on this protocol; the frozen one publishes its child environment exactly. The allowlist itself, and what is deliberately excluded from it, is in [the frozen reference's execution environment](terminal-resource-provider-protocol.md#execution-environment).
- **Which identifier an instance is asked on comes from the config section it is configured in, never from anything the executable says.** `plugins.external` entries are asked on `sidecar.plugin/v1-draft`; `terminalResources.providers` entries on `sidecar.terminal-resource/v1`. A plugin must answer on the identifier it was asked on; answering with the other one is a protocol failure, not a silent upgrade.

Plugins are one-shot. There is no resident process, no framing, no multiplexing. Live behaviour — search-as-you-type, background refresh, mutations — is built from one-shot calls plus the host-side mechanisms under [Freshness](#freshness-live-behaviour-without-a-resident-process).

## Methods

| Method | Runs when | Network | Mutates |
| --- | --- | --- | --- |
| `describe` | after the first ready frame; on config change; on recheck | no | no |
| `resolve` | a matcher span is activated (unchanged from resource v1) | may | no |
| `list` | a collection is shown, refreshed, re-sorted, re-viewed, searched, or paged | may | no |
| `get` | a collection row is opened, or a document tab is refreshed | may | no |
| `act` | the user confirms an action | may | yes |

`describe` is the only method whose absence is an error. A plugin that declares no collections and no actions is exactly a resource provider, and a resource provider keeps working unchanged: the host sends the old protocol identifier to entries that came from the old config section and translates the response into the same host types.

Matching itself never starts a process and never performs I/O.

### Request envelope

| Field | Present on | Meaning |
| --- | --- | --- |
| `protocol` | all | `sidecar.plugin/v1-draft`. A plugin that does not support the value must return `invalid_request` naming what it does support. This is the version-negotiation seam. |
| `method` | all | One of the five. An unrecognised method returns `invalid_request`, not a crash. |
| `instance` | all | The configured instance ID. Informational: a plugin may use it to select its own configuration, but argv selection takes precedence and a plugin must behave correctly if it ignores `instance` entirely. |
| `deadlineMs` | all | Milliseconds the host waits before killing the process group. Advisory but accurate. Budget inside it and return a typed `unavailable` rather than be killed: a typed timeout gives the user a real error card and a working Retry, where a SIGKILL gives them an opaque transport failure. |
| `host` | `describe` only | `{name, version}`. |
| `context` | `list`, `get`, `act`, `resolve` | Present only for context kinds the plugin declared in `describe` **and** the host has. See [Context](#context). |
| `params` | all but `describe` | Method-specific. |

### Response shape

A response is **exactly one of**: a describe result, a `resource` (from `resolve` and `get`), a `page` (from `list`), an `outcome` (from `act`), or an `error`. Every response also carries `protocol`. Unknown JSON fields are ignored, for forward compatibility.

## `describe`

Reports identity, what context the plugin reads, what it can recognise in terminal output, what collections it offers, and what actions it exposes. It must be local, fast, and non-interactive: no network, no credential prompt. A plugin that is installed but unconfigured returns a typed `invalid_config` with a single-line `setupHint` — not an empty declaration and not a crash.

The identity block is spelled `plugin`. A response that spells it `provider`, the resource protocol's name for the same object, is accepted too — an author who started from a resource provider is describing the same thing under the older name — and `plugin` wins if both are present.

```json
{
  "protocol": "sidecar.plugin/v1-draft",
  "plugin": {"kind": "recall", "name": "Recall", "version": "0.4.0", "docsUrl": "https://example.test/recall/sidecar"},
  "context": ["project"],
  "matchers": [
    {"id": "locator", "pattern": "\\brc:[a-z0-9]+:[A-Za-z0-9/_.-]+\\b", "priority": 50}
  ],
  "collections": [
    {
      "id": "results",
      "title": "Results",
      "search": "required",
      "columns": [
        {"id": "rank", "label": "#", "width": 3, "align": "right", "kind": "number"},
        {"id": "title", "label": "Title", "primary": true},
        {"id": "source", "label": "Source", "width": 14},
        {"id": "excerpt", "label": "Excerpt", "secondary": true}
      ],
      "views": [],
      "sort": [],
      "filters": [
        {"id": "profile", "label": "Profile", "kind": "choice",
         "choices": [{"id": "home", "title": "home"}, {"id": "docs", "title": "docs"}],
         "default": "home"},
        {"id": "source", "label": "Source", "kind": "choice",
         "choices": [{"id": "any", "title": "Any"}, {"id": "notes", "title": "notes"}]},
        {"id": "since", "label": "Since", "kind": "text"}
      ],
      "detail": true,
      "refresh": {}
    },
    {
      "id": "sources",
      "title": "Sources",
      "search": "none",
      "columns": [
        {"id": "name", "label": "Source", "primary": true},
        {"id": "health", "label": "Health", "kind": "status"},
        {"id": "fresh", "label": "Fresh", "kind": "timestamp"}
      ],
      "refresh": {"everySeconds": 120}
    }
  ],
  "actions": [
    {"id": "refresh-source", "title": "Refresh source", "on": "item", "collection": "sources", "mutates": true, "confirm": true}
  ]
}
```

### `plugin`

As `provider` in resource v1: informational display strings, bounded, never able to rename or collide with the configured instance ID. `docsUrl` passes the same `http`/`https` validation as `sourceUrl` and is the only executable-declared setup action Sidecar follows, after confirmation.

### `context`

The kinds of host context the plugin reads. Declared here so the settings page and `sidecar plugin list` can show them before anything runs. Configuring the plugin is the trust act; there is no second per-field grant. Kinds in v1:

| Kind | What the host sends | Why a plugin would want it |
| --- | --- | --- |
| `project` | `{root, workDir, name, branch, hostId}` for the surface the request came from; absent on a global surface with no project | `ongoing show <this project>`, `recall --scope project=` |
| `selection` | `{text}` when the user activated an action over selected text | search the selection, log it as a note |

Nothing else. Terminal lines, scrollback, tmux targets, file contents, and environment are not context kinds, and adding one is a protocol revision, not a field.

A kind this host does not recognise is dropped rather than refused: it is the one forward-compatible declaration in `describe`, and since the host only ever sends kinds it knows, an unknown one costs nothing.

**An undeclared kind is never sent.** That is enforced in the host at the process boundary, so a plugin that has never described successfully receives no context at all. A declared kind is sent whenever the host has it.

### `matchers`

Unchanged from resource v1: Go/RE2 patterns, the whole match is the locator, no plugin code runs in the scanner, bounded, and validated all-or-nothing. Precedence, overlap resolution, and `claimHosts` are described in [the frozen reference](terminal-resource-provider-protocol.md#matcher-rules-enforced-by-sidecar).

### `collections`

A collection is a named, listable set of rows the host can show as a table with a cursor.

| Field | Meaning |
| --- | --- |
| `id`, `title` | Stable ID (persisted in pane state, so changing it orphans saved tabs) and display title. A missing title falls back to the ID. |
| `search` | `none`, `optional`, or `required`. `required` means the collection is empty until there is a query, which is recall's shape. `optional` filters. An unrecognised value coerces to `none`. |
| `columns[]` | Ordered, at least one. `{id, label, width?, align?, kind?, primary?, secondary?}`. Exactly one `primary` column names the row; an optional `secondary` column is rendered under it when the pane is too narrow for a table. `kind` is `text` (default), `status`, `timestamp`, `user`, `number`, or `badge`; `align` is `left`, `right`, or `center`. `width` is a hint in cells and the host reflows. |
| `views[]` | Named preset filters: `{id, title}`. The host offers them in its View modal and sends the chosen ID back in `list`. |
| `sort[]` | Sortable keys: `{id, label, default?: "asc"\|"desc"}`. Offered in the same modal; the chosen key and direction go back in `list`. |
| `filters[]` | The collection's own choosers: `{id, label, kind: "choice"\|"text", choices?: [{id, title}], default?}`. See [filters](#filters). |
| `detail` | Whether `get` is meaningful for rows. Absent reads as `true`. `false` means Enter does nothing, though a row's `sourceUrl` can still open. |
| `refresh` | `{everySeconds?, watch?[]}`. See [Freshness](#freshness-live-behaviour-without-a-resident-process). |
| `context` | Optional narrowing: `["project"]` means this collection is meaningful only when project context exists, so a global surface hides it. |

Two things inside a collection are repaired rather than refused, because neither can be wrong in a way the user needs to act on: a second `primary` or `secondary` column is dropped so exactly one of each survives (a collection that declares no primary gets its first column), and a second default sort key loses its default. Everything else in `describe` is refused whole — see [Limits](#limits).

### `filters`

A collection's own choosers. The plugin declares them; the host draws them in its View modal, persists what the user chose with the tab, and sends the applied set back on every `list`.

| Field | Meaning |
| --- | --- |
| `id` | Stable, at most 32 characters. It is persisted with the tab and it is the key in `list.params.filters`, so changing it drops a saved choice. |
| `label` | The control's name, at most 32 characters. A missing label falls back to the id. |
| `kind` | `choice` (a radio group) or `text` (an input). Any other value refuses the whole describe: a control drawn as the wrong type collects the wrong value and the plugin filters on it. |
| `choices[]` | `{id, title}`, required for `choice` and refused for `text`. At most 64, ids and titles at most 32 characters, ids unique. A missing title falls back to the id. |
| `default` | For `choice`, a choice id — one this filter declares, or the describe is refused. Absent means the FIRST declared choice, because a radio group has to open on something and the plugin's own order says which. For `text`, the initial text, at most 64 characters. |

**The first declared filter is the collection's scope.** Its current value is always folded into the host's View pill, because a page gathered under a scope nobody can see is a page whose emptiness means nothing. Declare the one that changes what a page *is* first, and the ones that merely narrow it after.

Bounds: 8 filters per collection. Everything in `filters[]` is validated all-or-nothing with the rest of `describe`.

### `actions`

A typed operation the user can invoke. The plugin declares it; the host decides how it is reached.

| Field | Meaning |
| --- | --- |
| `id`, `title` | Stable ID and display title. |
| `on` | `item` (a collection row), `collection` (the whole list, e.g. "capture"), `resource` (a matcher-resolved document, e.g. "transition ticket"), or `global` (no subject). |
| `collection` | Required for `item` and `collection`, naming a collection this same describe declares. Absent for `resource` and `global`. |
| `inputs[]` | Up to 8 typed inputs the host collects before calling `act`: `{id, label, kind, required?, choices?[], default?}` with `kind` in `text`, `multiline`, `choice`, `confirm`. A `choice` input with no choices refuses the describe. |
| `mutates` | Whether it changes the plugin's data. A mutating action with no inputs gets a confirm step unless `confirm: false` is stated explicitly. An action that declares inputs never also confirms: the form is the confirm step. |
| `key` | Optional single lowercase letter the plugin would like. Honoured only if the browser's own keys, `keymap.HostReservedKeys`, `keymap.GlobalKeys`, and every other action leave it free; otherwise the action is reachable through the action menu and the palette. Never guaranteed, never persisted, and rebuilt on every describe. |

Actions never carry code, keys the host did not grant, or colours.

## `list`

```json
{
  "protocol": "sidecar.plugin/v1-draft",
  "method": "list",
  "instance": "recall",
  "deadlineMs": 10000,
  "context": {"project": {"root": "/path/to/checkout", "workDir": "/path/to/checkout", "name": "sidecar", "branch": "main"}},
  "params": {
    "collection": "results",
    "query": "dex",
    "view": "",
    "sort": {"key": "", "dir": ""},
    "filters": {"profile": "docs", "since": "2026-08-01"},
    "cursor": "",
    "limit": 100
  }
}
```

```json
{
  "protocol": "sidecar.plugin/v1-draft",
  "page": {
    "outcome": "degraded",
    "items": [
      {
        "id": "rc:notes:2026-08-14-dex-design",
        "cells": {"rank": "1", "title": "DEX schema notes", "source": "notes", "excerpt": "…people are tiered, and the tier drives what brief shows…"},
        "status": {"label": "exact", "tone": "success"},
        "sourceUrl": ""
      }
    ],
    "nextCursor": "",
    "total": 7,
    "notices": [
      {"tone": "warning", "text": "1 of 4 sources did not answer (mail: checkpoint stale)"}
    ],
    "omitted": {"suppressed": 2, "dropped": 6},
    "coverage": [
      {"source": "notes", "state": "answered", "elapsedMs": 12},
      {"source": "mail", "state": "unhealthy", "reason": "checkpoint stale since 2026-08-30", "elapsedMs": 2}
    ]
  }
}
```

| Field | Meaning |
| --- | --- |
| `outcome` | `answered`, `abstained` (nothing matched, sources fine), `degraded` (some eligible source could not answer), or `failed` (every source that was asked failed, so the page says nothing at all — the host renders an error card over the empty list and never the words "no matches"). These are recall's exit states lifted into data: a plugin whose CLI would have exited non-zero for one of them still exits `0` here and says it in `outcome`. The host renders each honestly — an empty list under `abstained` is "no matches", under `degraded` it is "no matches, and coverage was incomplete", under `failed` it is an error. An absent outcome reads as `answered`, which is what a plugin that never thinks about coverage means. **A value this host does not recognise reads as `degraded`**, because of the two ways to be wrong about a claim it cannot understand, that is the one that does not invent a guarantee on the plugin's behalf. |
| `items[]` | Bounded rows. `id` is what `get` and item actions receive, and a row without one is dropped: it could be neither opened nor acted on. `cells` is keyed by column ID; a missing cell renders blank, and **a cell keyed by a column the collection never declared is dropped**, because the host has nowhere to paint it. `status` is an optional `{label, tone}` pill rendered in a reserved right-hand column. `sourceUrl` is an optional validated http(s) URL. |
| `nextCursor` | Opaque; empty means no more. The host pages on demand, never eagerly. |
| `total` | Optional count for the summary row. |
| `notices[]` | Up to 4 single-line `{tone, text}` rows the host shows with the list. Where a coverage note or a scan-health line goes. |
| `omitted` | Optional `{suppressed, dropped}`: rows held back below the plugin's own relevance floor, and rows past its budget. The host renders both as data in the summary row ("8 shown · 1 below floor · 6 over budget") rather than leaving them to free text. Negative counts are dropped. |
| `coverage[]` | Optional per-source ledger, `{source, state, reason?, elapsedMs?}` with `state` in `answered`, `timeout`, `unhealthy`, `skipped`, `failed`. Read only by the host's coverage modal; the notices stay the one-line summary, because thirteen sources' states do not fit in four notice rows. A row naming no source is dropped; an unrecognised state reads as `failed`, for the same reason an unrecognised outcome reads as `degraded`. The host owns the colour each state is painted in. Bounded to 64 rows and 200-character reasons, truncated and marked past that. |

**`outcome` describes the row set of this page and nothing else.** A collection whose rows are all present answers `answered` even when what those rows describe is unhealthy: a list of thirteen sources, five of them stale, is a *complete* list. The health of the subject belongs on the rows, in their own `status` pills. Conflating the two makes an honest plugin report `degraded` for a page that was in fact complete, and leaves the reader unable to tell "I could not look" from "what I found is in a bad way".

`params.filters` is the applied set, and only the applied set: **a key whose value equals that filter's `default` is not sent, and a missing key means the default.** A key the collection did not declare — or a `choice` value that is not one of that filter's declared options — is dropped by the host before the process starts, so a plugin only ever reads names it published itself.

A query on a `search: required` collection with an empty string is answered by the host without calling the plugin: an `abstained` page and a prompt. No process is started, which is what keeps a required-search collection free once per keystroke rather than once per keystroke plus a spawn.

`list` is refused before any process starts if the collection is not one the newest successful `describe` declared. The declaration is what says which columns exist, and a page sanitized against no declaration would carry cells the host has nowhere to paint.

Status tones are the frozen protocol's and mean the same thing across every plugin: `neutral`, `info`, `success`, `warning`, `danger`, with unknown tones coercing to `neutral`. Map from the service's own *category* rather than its display label.

## `get`

```json
{"method": "get", "params": {"collection": "results", "id": "rc:notes:2026-08-14-dex-design"}}
```

Returns a `resource`, the same object `resolve` returns, extended with `sections`:

```json
{
  "protocol": "sidecar.plugin/v1-draft",
  "resource": {
    "identity": "rc:notes:2026-08-14-dex-design",
    "title": "DEX schema notes",
    "subtitle": "notes · 2026-08-14",
    "status": {"label": "exact", "tone": "success"},
    "fields": [{"label": "Source", "value": "notes"}, {"label": "Locator", "value": "rc:notes:2026-08-14-dex-design"}],
    "body": {"format": "markdown", "text": "…"},
    "sections": [
      {"title": "Evidence", "body": {"format": "markdown", "text": "…"}},
      {"title": "Timeline", "items": [
        {"when": "2026-08-14T10:02:00Z", "title": "Note added", "text": "…"},
        {"when": "2026-08-20T16:40:00Z", "title": "Linked from td-3fa2c1", "text": ""}
      ]}
    ],
    "sourceUrl": "",
    "updatedAt": "2026-08-20T16:40:00Z",
    "freshForSeconds": 60
  }
}
```

`identity` and `title` are required; a response missing either is a transport failure rather than a blank card. The other resource fields, their kinds, and the rule against duplicating `updatedAt` as a field are in [the frozen reference](terminal-resource-provider-protocol.md#success-response).

`sections[]` is bounded to 8, and each section is exactly one of `{body}`, `{fields[]}`, or `{items[]}` — a timeline, whose `when` is RFC 3339 and is rendered relatively. A section that sends more than one keeps the first in that order rather than being refused, so a plugin that sends two still shows the user something. A resource with no `sections` renders exactly as a resource v1 card does today, which is how a frozen-protocol provider keeps working.

`get` shares the resolve cache under a `get`-prefixed key, so a second Enter on the same row costs no process.

## `act`

```json
{
  "method": "act",
  "context": {"project": {"…": "…"}},
  "params": {
    "action": "log-note",
    "collection": "people",
    "id": "p:ada",
    "inputs": {"text": "Met at the conference; follow up about the retrieval eval pack."}
  }
}
```

```json
{
  "protocol": "sidecar.plugin/v1-draft",
  "outcome": {
    "status": "done",
    "message": "Logged a note for Ada",
    "refresh": ["people"],
    "open": {"collection": "people", "id": "p:ada"}
  }
}
```

| Field | Meaning |
| --- | --- |
| `status` | `done` or `failed`. A `failed` outcome is a typed failure with a message, not a transport failure. |
| `message` | Single line, shown as a status flash and echoed in the footer. |
| `refresh[]` | Collections the host re-lists if they are visible. Documents whose `identity` matches an affected item are re-fetched. |
| `open` | Optional `{collection, id}` to open after the action, which is what makes capture-then-show one gesture. |

For `on: "resource"` actions, `params` carries `{action, matcher, locator, inputs}` instead of `{collection, id}`. That is how "transition this Jira ticket" fits without a new method.

Two identical `act` calls are two intentions: `act` shares neither the cache nor the dedupe table that `get` and `resolve` share, because collapsing them would drop a change the user asked for twice on purpose.

## Errors

Unchanged codes and semantics from resource v1: `not_found`, `unauthorized`, `forbidden`, `rate_limited`, `invalid_config`, `invalid_request`, `unavailable`, `internal`. Unknown codes map to `internal`. The per-response `retryable` field is authoritative rather than inferred from the code, and `setupHint` is a single line, rendered as copyable text and never executed. The table of default `retryable` values is in [the frozen reference](terminal-resource-provider-protocol.md#typed-failure-response).

```json
{
  "protocol": "sidecar.plugin/v1-draft",
  "error": {
    "code": "invalid_config",
    "message": "Recall has no index for this machine yet",
    "retryable": false,
    "setupHint": "Run recall index --bootstrap"
  }
}
```

## Freshness: live behaviour without a resident process

One-shot invocation is acceptable only if live search, mutations, and background updates are still reachable. Each is built from one-shot calls plus a host mechanism Sidecar already has.

| Need | Mechanism |
| --- | --- |
| Search as you type | The host debounces 250 ms, sends `list` with the new `query`, and kills the previous in-flight process group for that pane. A page is applied only if it answers the live query and the browser that asked. |
| Edit or transition a document | An `act`; the response's `refresh` re-lists and re-fetches what it names. |
| The plugin's data changed in the background | `refresh.watch[]`: absolute paths (files or directories, `~` expanded, under the user's home but not the home directory itself) the host watches while a collection or document from that plugin is on screen. A change re-lists visible collections and re-fetches visible documents. |
| The plugin cannot name a path | `refresh.everySeconds`, clamped to [15, 900] and polled only while a tab from that plugin is visible. |
| The plugin wants to poke Sidecar itself | `sidecar plugin changed <instance> [--collection ID]` writes one request on the file bus every running Sidecar already watches. A plugin's own daemon or a shell hook can call it with no protocol change. |
| A slow tool | The user keeps the previous page under a refreshing badge; the host never blanks a list to wait. |

Nothing here needs a socket. Nothing refreshes when no tab from that plugin is on screen.

## Context

The host sends `context` on `list`, `get`, `act`, and `resolve`, only for kinds the plugin declared. The `project` object:

```json
{"root": "/path/to/main/checkout", "workDir": "/path/to/worktree", "name": "sidecar", "branch": "feature/x", "hostId": ""}
```

`hostId` is non-empty on a remote-bound surface, and the paths are then paths on that host. A plugin that only knows this machine should say so with a typed `unavailable` naming the host, which is the same rule Sidecar's own plugins follow. Sidecar never substitutes a local path for a remote one.

Project context is republished on every describe pass, because a global plugin outlives every project switch and the context it was constructed with is the wrong answer the moment the user moves.

## Limits

Everything in [resource v1's limits](terminal-resource-provider-protocol.md#limits) — response bytes, body bytes, field counts, title and identity lengths, matcher bounds, the `describe` and `resolve` timeouts — plus:

| Bound | Default |
| --- | --- |
| Collections per plugin | 16 |
| Columns per collection | 12 (at least 1) |
| Views / sort keys per collection | 32 / 16 |
| Actions per plugin / inputs per action / choices per input | 32 / 8 / 64 |
| Items per page, and the `limit` clamp | 500 |
| Cell length | 512 chars |
| Notices per page / notice length | 4 / 200 chars |
| Filters per collection / choices per filter | 8 / 64 |
| Filter and choice ID / label and title / text value | 32 / 32 / 64 chars |
| `coverage[]` rows per page / reason length / source name | 64 / 200 chars / 64 chars |
| Sections per resource / timeline items per section | 8 / 200 |
| Collection ID / title, column ID / label | 64 / 64, 64 / 32 chars |
| Action ID / title, input label / default | 64 / 64, 64 / 512 chars |
| `act` outcome message | 200 chars |
| `refresh.watch` paths per plugin | 8, each absolute and under the user's home directory but not the home directory itself |
| `refresh.everySeconds` | clamped to [15, 900] |
| `list` / `get` / `act` timeout | 10 s, configurable per instance and clamped to 60 s |

Over-limit content in a `list`, `get`, `resolve`, or `act` response is truncated and marked, as in resource v1: a slightly-too-long page still shows the user their rows, where refusing it shows them an error for a page that was almost entirely fine. Only stdout size and the structural violations under [Invocation model](#invocation-model) refuse a response outright.

**`describe` is the exception, and the exception is deliberate.** It is validated all-or-nothing, exactly as resource v1 already validates matchers. A plugin that declares a 13-column collection, a collection with no columns, a duplicate collection or column ID, a nine-filter collection, a `choice` filter with no choices or one whose `default` is not among them, an action naming a collection it never declared, a `choice` input with no choices, or a watch path outside the home directory is refused whole: nothing it declared is published, and `sidecar plugin check` and `sidecar plugin list --describe` report the instance as `incompatible` with the outcome `invalid-describe`. (The host builds a specific reason for the refusal — which collection, which column, which path — and the CLI does not print it yet; until it does, the way to find the offending declaration is to call `describe` from the plugin's own CLI and read the JSON.) Publishing the rest of such a declaration would hide the bug while changing what the scanner recognises and what the host holds open on disk. The two repairs named under [collections](#collections) are the only exceptions.

What happens to a plugin's existing declarations when a describe fails is the frozen protocol's rule, unchanged: a typed error removes them, a transport failure or a validation refusal **keeps** the last good ones for the rest of the process and reports the failure, because the host has no authoritative new answer and does not discard a working one.

All strings must be valid UTF-8; invalid sequences are replaced. Control characters are stripped from single-line values. A `sourceUrl` or `docsUrl` that does not survive validation unchanged is dropped rather than repaired.

## Safety posture

The frozen protocol's [safety posture](terminal-resource-provider-protocol.md#safety-posture) applies in full: plugin text never becomes ANSI, the Markdown renderer is not a trust boundary and bodies are sanitized before and after rendering, `sourceUrl` is the only plugin-supplied thing that can open a URL, and debug logs record instance, method, duration, outcome code, and byte counts — never the locator, title, body, URL, credentials, stdout, or stderr.

Two rules this protocol restates because it widens what a plugin can ask for:

- **A process boundary is crash isolation, not a sandbox.** Configuring a plugin trusts that executable with the user's full OS privileges. Sidecar never discovers, scans, installs, auto-enables, or upgrades a plugin, and a repository can never cause one to run merely by being opened. `sidecar plugin add` prints exactly what will run and writes one config entry; that is the whole install flow.
- **Context is a declaration, not a grant negotiated per call.** The kinds in `describe` are what the settings page and `sidecar plugin list` show the user before anything runs, and the host filters what it sends against them at the process boundary.

## Configuration

```json
{
  "plugins": {
    "external": [
      {
        "id": "recall",
        "command": ["recall", "sidecar-plugin"],
        "passEnv": ["RECALL_PROFILE"],
        "enabled": true,
        "scope": "global",
        "placements": ["tab", "panes"],
        "timeout": "10s",
        "claimHosts": []
      }
    ]
  }
}
```

- `id` is unique, non-empty, stable, at most 64 characters, and is the persisted plugin key.
- `command` is an argv array executed without a shell. The first element may be an absolute path or resolve through `PATH`.
- `passEnv` names variables whose *current values* are inherited. Names only — an entry containing `=` is refused with the value redacted out of the message — and the base environment wins on conflict.
- `scope` defaults to `global`, the only value this version supports. `project` is refused with a message saying to remove the key and read project context per call.
- `placements` is `tab`, `panes`, or both, and decides where the plugin's content can appear. It defaults to both.
- `timeout` is clamped to [1s, 60s]; `claimHosts` means what it means for a resource provider.
- Array order is precedence. At most 16 instances across both sections.

Everything under `plugins.external` is behind the `plugin_protocol` feature flag, which is on by default: nothing is configured on a fresh install, so nothing is described or started. Set `features.flags.plugin_protocol` to false to stop every plugin process while leaving the configuration in place. The flag gates only this section: `terminal_resource_providers` governs the frozen one on its own, so turning this protocol off cannot take a working resource provider down with it.

An ID configured in both sections is one plugin: `plugins.external` wins and the legacy entry is dropped, so a half-finished migration cannot start two child processes under one identity.

## Headless verification

```bash
sidecar plugin list --json                 # config only: no subprocess, no PATH lookup
sidecar plugin list --describe --json      # opt in to running describe on each active plugin
sidecar plugin check hello --json          # describe with the app's own environment and timeouts
sidecar plugin check hello --list greetings --query bon --json
sidecar plugin check hello --get greetings fr --json
sidecar plugin call hello list --params '{"collection":"greetings","query":"bon"}' --json
```

`list` starts nothing without `--describe`, so it is safe in a loop. `--list` and `--get` are separate explicit flags on `check` because either can perform network access and print private data; neither is ever implied. `call` sends one method through the host's own envelope and validation.

**Only what the host kept is printed, never the plugin's raw stdout.** Every string these commands show has been through the same sanitization and bounds a pane would apply, which is what makes them an authoring loop rather than a pretty-printer. The full verb family, its options, and its exit codes are in [the CLI reference](cli.md#sidecar-plugin).

## Fixtures

Canonical request and response JSON lives at `internal/pluginhost/testdata/protocol/` (the `plugin-*.json` files; the unprefixed ones are the frozen protocol's) and may be vendored by plugin authors.

**That path moved.** It was published as `internal/resourceprovider/testdata/protocol/` by the frozen protocol's reference, and the M2a rename to `internal/pluginhost` moved it. The stability promise was broken for anyone who followed it; the new path is the one above, and both references now name it.

The reference fixture executable is `internal/pluginhost/testdata/fixtureprovider`. It speaks both identifiers from one binary — itself a property under test — and simulates every hostile case the resource fixture does, plus: an `act` that never returns, a `list` whose `nextCursor` loops, a collection that declares 13 columns, watch paths that are outside the home directory, are the home directory, are relative, or are too many, an action with an unknown target, an action naming an undeclared collection, a `choice` input with no choices, a page whose every string is trying to escape the table, a page over every count limit (rows, cells, and `coverage[]`), a page carrying an undeclared cell, and a `describe` that answers `sidecar.terminal-resource/v1` only. Its `results` collection declares one filter of each shape — a choice with a default, a choice without, and a text one — and four list queries exercise the M4b fields: `filters` echoes back exactly the filters that reached it, `coverage` returns `omitted` beside a 13-row `coverage[]`, `failed` returns the `failed` outcome, and `future` returns an outcome from a version this host has never heard of. Each is selected with `-mode=NAME` or with a `mode:NAME:` prefix on the request's subject — the locator on `resolve`, the query on `list`, the id on `get`, the action on `act`.

The smallest complete plugin, in Python and checked in, is `docs/guides/examples/hello-plugin/`; [the authoring guide](../guides/active/creating-plugins.md) builds it up method by method, and a test under `internal/pluginhost` runs it through the real host so it cannot rot.

## History

- 2026-09-03: M4b applied four revisions: `filters[]` on a collection, `list.params.filters`, `page.omitted`, `page.coverage[]`, and the `failed` outcome; and stated the rule that `outcome` describes only the row set. Everything still in the plan README's pending table remains unimplemented.
- 2026-09-03: published as the single authority for `sidecar.plugin/v1-draft`, carrying the contract that was drafted in `docs/plans/active/plugin-ecosystem/protocol.md` and implemented in M2a and M2b. Still a draft; the identifier freezes in M4d.
