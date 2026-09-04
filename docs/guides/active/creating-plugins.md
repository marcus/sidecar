# Creating a Sidecar plugin

You have a tool with a CLI. You want its data in Sidecar — a tab you can search, rows you can open, panes beside a terminal — and you do not want to write a terminal UI to get it.

That is what a **protocol plugin** is. You add five (or three) JSON methods to your existing CLI, write one config entry, and Sidecar renders the rest: the tab, the table, the cursor, the detail card, the search box, the keys, the theme, the persistence, the live refresh. Your plugin never draws a frame and never binds a key.

This guide takes you from nothing to a plugin that passes `sidecar plugin check`. It uses a complete working example, [`docs/guides/examples/hello-plugin/hello_plugin.py`](../examples/hello-plugin/hello_plugin.py), which is 200 lines of Python with no dependencies and which a test in `internal/pluginhost` runs on every build, so what you read here is what runs.

The contract itself — every field, every bound, every rule — is [docs/reference/plugin-protocol.md](../../reference/plugin-protocol.md). This guide is the path through it.

## Before you start

The plugin host is governed by the `plugin_protocol` feature flag, which is on by default — an install with nothing configured starts nothing, so it costs you nothing until you configure a plugin. Every `sidecar plugin` command below needs it; with `features.flags.plugin_protocol: false` in `~/.config/sidecar/config.json` they exit `4` and say so, which is also how you stop every plugin process while leaving the configuration in place. The identifier is `sidecar.plugin/v1-draft` until the protocol freezes; see [the reference's draft status](../../reference/plugin-protocol.md#draft-status-what-is-settled-and-what-is-not) for what that does and does not mean for your plugin. The identifier is `sidecar.plugin/v1` and it is frozen; see [what the freeze promises](../../reference/plugin-protocol.md#frozen-what-that-promises), including the one rule that keeps your plugin working with a Sidecar released before it.

## Which class of plugin do you want?

Sidecar hosts two kinds, and choosing wrong costs weeks.

| | **Protocol plugin** | **Embedded plugin** |
| --- | --- | --- |
| What it is | Any executable, any language, answering JSON on stdout | A Go package compiled into Sidecar, with its own Bubble Tea UI |
| Who renders | Sidecar, from your declarations | You |
| Ships how | A config entry the user writes. Sidecar needs no release | A Sidecar release |
| Good for | Browsing a tool's data: lists, search, documents, a few typed actions | A layout nothing else has — a board, a queue, a canvas |
| Cost | An afternoon | A plugin, a keymap, a theme pass, and a place in the repository |

**Choose the protocol class unless your screen is the point.** A protocol plugin will never render as richly as an embedded one, and that is deliberate: a vocabulary big enough to express an arbitrary screen would be a worse Bubble Tea. In exchange, every protocol plugin gets focus, tabs, pointer parity, persistence, live refresh, and theme awareness for free, and inherits every improvement Sidecar makes to them without changing a line of your own.

If you do need the embedded class, stop here and read [`.claude/skills/create-plugin/SKILL.md`](../../../.claude/skills/create-plugin/SKILL.md) instead — it covers `plugin.Plugin`, the registry, keymap contexts, and the app shell.

## The five methods

| Method | Runs when | You need it if |
| --- | --- | --- |
| `describe` | at startup, on config change, on recheck | always |
| `list` | a collection is shown, searched, sorted, refreshed, or paged | you have rows |
| `get` | a row is opened, or a document tab refreshes | your rows expand into documents |
| `act` | the user confirms an action | you offer mutations |
| `resolve` | a matcher span in terminal output is clicked | you recognise keys in terminal text (this is the frozen resource protocol's method, unchanged) |

`describe` is the only one you must implement. The example implements `describe`, `list` and `get`; it declares no matchers and no actions, so it never sees `resolve` or `act`, and it answers `invalid_request` if something asks for them anyway.

Each call is one process: Sidecar writes one JSON object to your stdin and closes it, you write exactly one JSON object to stdout and exit `0`. Nothing else on stdout — not a banner, not a progress line, not a trailing log. stderr is drained and discarded.

**A typed error is a success at the process level.** Exiting non-zero, printing two objects, or timing out is a *transport failure*: the user gets an opaque card. Printing `{"error": {"code": "not_found", ...}}` and exiting `0` gets them a real message and a working Retry.

## Write the plugin

The example's skeleton is the whole shape:

```python
PROTOCOL = "sidecar.plugin/v1"

def answer(request):
    if request.get("protocol") != PROTOCOL:
        return error("invalid_request", f"hello speaks {PROTOCOL}")
    method = request.get("method", "")
    params = request.get("params") or {}
    if method == "describe":
        return describe()
    if method == "list":
        return list_page(params)
    if method == "get":
        return get(params, request.get("context"))
    return error("invalid_request", f"hello does not implement {method!r}")

def main():
    request = json.loads(sys.stdin.read() or "{}")
    response = answer(request)
    response["protocol"] = PROTOCOL
    print(json.dumps(response, ensure_ascii=False))
```

In your own tool this is one subcommand — `mytool sidecar-plugin` — beside the ones you have. Sidecar sets `SIDECAR_PLUGIN=1` in the child environment, so a binary whose ordinary CLI and whose plugin subcommand share code can tell which one it is running as.

### `describe`: what you have

```python
def describe():
    return {
        "plugin": {"kind": "hello", "name": "Hello", "version": "1.0.0"},
        "context": ["project"],
        "collections": [{
            "id": "greetings",
            "title": "Greetings",
            "search": "optional",
            "columns": [
                {"id": "name", "label": "Greeting", "primary": True},
                {"id": "language", "label": "Language", "width": 12},
                {"id": "updated", "label": "Updated", "kind": "timestamp"},
                {"id": "note", "label": "Note", "secondary": True},
            ],
            "sort": [{"id": "name", "label": "Name", "default": "asc"},
                     {"id": "updated", "label": "Updated"}],
            "filters": [{"id": "language", "label": "Language", "kind": "choice",
                         "choices": [{"id": "any", "title": "Any"}, ...],
                         "default": "any"}],
            "detail": True,
        }],
    }
```

Five decisions are in there, and each one is a thing the host will draw:

- **`primary`** names the row. Exactly one column has it, and it is the cell that survives when the pane is narrow. **`secondary`** is the line that folds under it there.
- **`kind`** tells the host how to present a cell: `timestamp` is rendered relatively ("2 weeks ago"), `status` as a pill, `number` right-aligned. It never changes validation.
- **`sort`** is offered in the View modal on `v`. You get the chosen key back in `list.params.sort`; sorting is still yours to do.
- **`filters`** is offered in the same modal, and the first one you declare is the collection's *scope* — the host always shows its current value in the View pill. See [Filters, and which one is your scope](#filters-and-which-one-is-your-scope).
- **`search: "optional"`** puts a query line above the table. `"required"` means the collection is empty until there is a query — and the host answers the empty case itself, without starting your process. `"none"` means no query line at all.

`describe` must be local and fast: no network, no credential prompt. If your tool is installed but not configured, return `invalid_config` with a one-line `setupHint` — the user gets a setup card saying what to run.

**`describe` is validated all or nothing.** A 13-column collection, an action naming a collection you did not declare, a `choice` input with no choices, or a watch path outside the home directory refuses the whole declaration, not just the bad part. That is deliberate: publishing half of a broken declaration would hide the bug while changing what the scanner recognises and what Sidecar holds open on disk.

### `list`: the rows

```python
    return {"page": {
        "outcome": "answered" if matched else "abstained",
        "items": [{"id": g["id"],
                   "cells": {"name": g["name"], "language": g["language"], ...},
                   "status": {"label": g["language"], "tone": "info"}}
                  for g in matched],
        "total": len(matched),
    }}
```

- Every row needs an `id`. It is what `get` and item actions receive, and a row without one is dropped, because it could be neither opened nor acted on.
- `cells` is keyed by *column ID*. A missing cell renders blank; a cell keyed by a column you never declared is dropped, because the host has nowhere to paint it.
- `nextCursor` is opaque and yours. The host pages on demand, never eagerly.
- `notices[]` is up to four single lines, for things like "1 of 4 sources did not answer".
- `omitted` is `{suppressed, dropped}` — rows you held back below your own relevance floor, and rows past your budget. Send counts rather than writing them into a notice: the host renders them as data on the summary row ("8 shown · 1 below floor · 6 over budget").
- `coverage[]` is one row per source you asked, `{source, state, reason?, elapsedMs?}` with `state` in `answered`, `timeout`, `unhealthy`, `skipped`, `failed`. It is read only by the host's coverage modal, so send the whole ledger — thirteen sources' states do not fit in four notices — and keep the notice as the one-line summary.

### `outcome`: say which claim you are making

This is the field authors skip and then regret, so it gets its own section.

An empty list means three completely different things, and the host says all three differently:

| `outcome` | What it claims | What the user sees over an empty list |
| --- | --- | --- |
| `answered` | I asked everything and this is the answer | "no matches" — a fact about the data |
| `abstained` | Nothing matched, and every source was healthy | "no matches" — the same words, honestly earned |
| `degraded` | Some source that should have answered could not | "no matches, and coverage was incomplete" |
| `failed` | Every source you asked failed, so the page says nothing at all | an error card — never the words "no matches" |

If your CLI would have exited non-zero for one of these, it still exits `0` here and says so in `outcome`. An absent `outcome` reads as `answered`, which is what a plugin that never thinks about coverage means. A value this host does not recognise reads as `degraded`, because of the two ways to be wrong about a claim it cannot understand, that is the one that does not invent a guarantee on your behalf.

**`outcome` describes the row set of this page and nothing else.** Do not use it for the health of the *things* the rows describe. A collection of six failing builds, all six listed, is `answered`: the page is complete. The builds' state belongs in a `status` cell. Getting this wrong is the one mistake that makes an honest plugin look broken — every page reads `degraded` and the reader can no longer tell "I could not look" from "what I found is in a bad way".

When a page is `degraded` or `failed`, send `coverage[]` with it. The host's coverage modal — `c`, or a click on the outcome word or a notice — is where a reader finds out *which* source could not answer and why, and without the ledger it can only repeat the one-line notice back to them.

### `get`: the document

`get` returns the same `resource` object `resolve` returns, plus `sections`:

```python
    return {"resource": {
        "identity": g["id"], "title": g["name"], "subtitle": g["language"],
        "status": {"label": "known", "tone": "success"},
        "fields": [{"label": "Language", "value": g["language"]},
                   {"label": "Updated", "value": g["updated"], "kind": "timestamp"},
                   {"label": "Asked from", "value": project.get("name", "no project")}],
        "body": {"format": "markdown", "text": g["note"]},
        "sections": [{"title": "History", "items": [{"when": ..., "title": ...}]}],
        "updatedAt": g["updated"],
    }}
```

`identity` and `title` are required. Each section is exactly one of a `body`, a `fields[]` grid, or an `items[]` timeline; up to eight of them, in the order you declare. Bodies are Markdown, sanitized before and after rendering — links become plain visible text, because the Markdown renderer is not a trust boundary. The only thing of yours that can open a URL is the separately validated `sourceUrl`.

Do not repeat `updatedAt` as a field: the host owns rendering it, and renders it relatively.

**`get.params.filters` is the scope the row was found in**, sent exactly as the `list` that produced the row sent it. Read it the same way you read it on `list`. If a filter of yours changes what a row *means* — a profile, a sensitivity level, an account — then resolving the row without it is resolving a different row, and a plugin that ignores it will eventually answer a document the user cannot explain, or refuse one they could see a moment ago.

## Configure it, and what that means

```bash
sidecar plugin add hello --command python3 /path/to/hello_plugin.py
```

There is no discovery. Sidecar never scans a directory, never runs every `sidecar-*` binary on `PATH`, never auto-enables anything, and never lets a repository declare a plugin. One config entry, written by the user, is the whole install flow — which is why `add` shows exactly what it is about to trust before it writes:

```text
Configure "hello" as an external plugin.

  Sidecar will run, directly and with no shell:
    argv[0]  python3
    argv[1]  /path/to/hello_plugin.py
  Working directory: /Users/you/.config/sidecar
  Protocol:          sidecar.plugin/v1
  Scope:             global
  Placements:        tab, panes
  Timeout:           10s

  A process boundary is crash isolation, not a sandbox: this executable
  will run with your full OS privileges.

Configure it? [y/N]
```

**That confirmation is the trust act.** There is no second permission prompt later, and no per-field grant: the context kinds your `describe` declares are shown in `sidecar plugin list` and on the settings page precisely because configuring you is where the user decides. `--yes` skips the question, which is what a script or an agent uses; without a readable stdin, `add` refuses and says to pass it.

Everything after `--command` is the argv, executed with no shell, so it goes last. `--pass-env NAME` names an environment variable whose current value should be passed through (names only — never inline secrets). `--placement tab|panes` decides whether your content can be a navbar tab, pane content, or both.

## Check it

`sidecar plugin check` runs your plugin with the same environment, working directory, and timeouts the app uses, and prints **only what the host kept** — everything below has already been through the sanitization and the bounds a pane would apply:

```console
$ sidecar plugin check hello
hello  [enabled, ready]  plugins.external
  protocol  sidecar.plugin/v1
  command   python3 /path/to/hello_plugin.py
  resolves  /opt/homebrew/bin/python3
  scope     global
  places    tab,panes
  timeout   10s
  describe  ok in 23ms — Hello 1.0.0, 0 matcher(s), 1 collection(s), 0 action(s)
            reads context project
            collection greetings "Greetings" search=optional columns=name*,language,updated,note^ sort=name (asc),updated detail
              filter language "Language" kind=choice scope choices=any,English,French,Japanese default=any
```

`name*` is your primary column and `note^` your secondary — the check is showing you the two layout decisions it read out of your declaration.

`describe` always runs; calling `list` or `get` is opt-in, because either can reach the network and print private data:

```console
$ sidecar plugin check hello --list greetings --filter language=French
  list      ok in 23ms — greetings, answered, 1 row(s)
            filters   language=French
            fr  Bonjour

$ sidecar plugin check hello --get greetings ja
  get       ok in 23ms — ja  こんにちは
            section History (1 timeline item(s))
```

When you want the raw shape rather than the summary, `sidecar plugin call` sends one method through the host's envelope and prints the object the host kept:

```bash
sidecar plugin call hello list --params '{"collection":"greetings","query":"bon"}' --json
```

That loop — write a response, call it, see what survives — is the fastest way to learn what the bounds do to your data.

If your declaration is refused, `check` reports the instance as `incompatible` with the outcome `invalid-describe`, and the way to find the offending line today is to run your own `describe` and read the JSON against [the reference's limits](../../reference/plugin-protocol.md#limits).

Then restart Sidecar. Enablement and configuration are read at startup.

## What the host draws for what you declared

Read [the M0 mockups](../../plans/active/plugin-ecosystem/mockups/README.md) once; they are rendered from the same components the real app paints with, so they are an accurate picture of what your declarations become.

- **A `tab` placement** is a navbar surface: your collection's table on the left, the detail card on the right, a query line above the table when `search` is not `none`, the notices and a summary row below it, and a View control on the title row.
- **A `panes` placement** puts one collection, or one document, in a Resource leaf beside a terminal — one shape at a time, because a pane is usually too small for both, and because that is what makes a collection and a row two *tabs* of one leaf.
- **Rows reflow** when the pane is narrow: the `primary` cell on line one, the short columns and the `secondary` cell folded into a dimmed line two.
- **`status` gets a reserved column** on the right. You do not declare it as a column, and you cannot choose where it goes.
- **Themes are free.** You send tones and kinds; the host maps them to the active theme. A theme change re-renders your plugin the way it re-renders anything else, and there is nothing for you to do.

## The keys are the host's

Every protocol plugin has the same keys, and you declare none of them: `j`/`k` move, `Enter` opens (and a second `Enter` focuses what it opened), `/` edits the query, `v` opens the View modal, `r` refreshes, `a` opens the action menu, `c` opens the coverage modal for the page's outcome, `o` opens a `sourceUrl` through the host's confirmed path, `+`/`-` move the split between the table and the document, `Esc` closes an overlay. `Tab` is the app's focus ring and `n` is the pane switcher; the browser deliberately takes neither.

The pointer is the host's too, on the app's rule everywhere: nothing a click does is reachable only by click. Clicking a row selects it and a second click opens it, exactly as `Enter` twice does. Clicking anywhere in a box focuses it. The wheel scrolls the box under the pointer rather than the focused one, and the browser tells the host when a notch would move nothing, so trackpad inertia at the end of a list is dropped instead of repainting a stationary surface. Each box draws a scrollbar when its content overflows, and the bar is a target: a press on the track jumps, a drag on the thumb follows. The divider between the two boxes is a drag rail, and the split it leaves is remembered per plugin across relaunches — the same move `+`/`-` make. The query line is a click target, and so is the View control on the title row. The outcome word on the query row, and any notice under the table, opens the coverage modal that says what the page's claim means, and lists your `coverage[]` as a table — the same modal `c` opens, and both are offered only where the page has something to explain. None of this is anything you declare. In a `panes` placement there is one box rather than two, so the rail and its keys are simply not there.

The one key you can ask for is an action's optional `key`: a single lowercase letter, granted only if the browser's own keys, the host's reserved set, the global keys, and every other action leave it free. It is never guaranteed and never persisted, so an action must always be reachable from the action menu too.

## Views, sort, and filters

A collection declares `views[]` (named presets) and `sort[]` (sortable keys). The host puts both in one View modal opened with `v`, and sends the chosen values back in `list.params` as `view` and `sort: {key, dir}`. Applying them is yours: the host does not reorder or filter your rows, because only you know what your keys mean.

Declare nothing you do not read. A plugin that declares a view it ignores has told the user about a control that does nothing.

### Filters, and which one is your scope

`filters[]` is a collection's own choosers, drawn in the same View modal under the sort list:

```json
"filters": [
  {"id": "profile", "label": "Profile", "kind": "choice",
   "choices": [{"id": "home", "title": "home"}, {"id": "docs", "title": "docs"}],
   "default": "home"},
  {"id": "source", "label": "Source", "kind": "choice",
   "choices": [{"id": "any", "title": "Any"}, {"id": "notes", "title": "notes"}]},
  {"id": "since", "label": "Since", "kind": "text"}
]
```

- `kind: "choice"` is a radio group and needs `choices`; `kind: "text"` is an input and must not have any. Any other kind refuses the whole `describe`.
- `default` on a choice names one of its own choice ids — anything else refuses the describe — and an absent one means the first choice you listed. On a text filter it is the initial text.
- **The first filter you declare is the collection's scope.** Its current value is always folded into the host's View pill (`⇅ rank · docs`), and any other applied filter is counted beside it (`· 2 filters`). Declare the one that changes what a page *is* first, and the ones that merely narrow it after.

What comes back in `list.params.filters` is **only what is applied**: a filter sitting on its default is not sent, and a missing key means the default. Read it that way — `filters.get("profile", "home")`, not `filters["profile"]`. Keys you never declared are dropped by the host before your process starts, so you only ever read names you published yourself.

`sidecar plugin check <plugin> --list <collection> --filter id=value` prints the set that was actually sent, not the one you asked for, which is how a dropped key or a misspelled id shows itself as an absence rather than as silence. The example plugin declares one, so `--filter language=French` above is a round trip you can run.

The host persists the applied set with the tab and with a global tab's remembered query, so a scope survives a relaunch. Bounds: 8 filters per collection, 64 choices each, 32-character ids and titles, 64-character text values.

## Context: ask for what you read

```json
"context": ["project"]
```

Declaring `project` means every `list`, `get` and `act` from a surface that has one carries it:

```json
{"root": "/path/to/checkout", "workDir": "/path/to/worktree", "name": "sidecar", "branch": "main", "hostId": ""}
```

Two kinds exist: `project` and `selection` (the text the user had selected when they invoked an action). Nothing else — not terminal lines, not scrollback, not file contents, not environment. An undeclared kind is never sent, enforced at the process boundary, so a plugin that has never described successfully receives nothing at all.

Three rules worth building against:

- **`root` is the truth, not `name`.** Restrict by path, not by comparing the project's name against your own labels. Recall shipped a bug of exactly that shape: it matched `project.name` against a field that held something else, and every search in a real project answered empty while claiming to be honest.
- **A non-empty `hostId` means another machine.** The paths are that host's paths. If you cannot answer about it, return `unavailable` naming the host rather than answering about this one.
- **If you cannot apply a restriction, say so.** A `degraded` page with a notice is honest; a successful empty page is not.

## Freshness: staying true without a resident process

Your plugin is one-shot: it starts, answers, exits. Live behaviour comes from four host mechanisms.

| You want | Declare or run |
| --- | --- |
| Rows to update when your store changes | `refresh: {"watch": ["~/.local/share/mytool/db"]}` on the collection. Up to 8 paths per plugin, absolute, under the home directory but not the home directory itself. Sidecar watches them only while a tab of yours is on screen. |
| A poll instead, because you cannot name a path | `refresh: {"everySeconds": 120}`, clamped to [15, 900] and polled only while visible. |
| To poke Sidecar yourself, from a hook or a daemon | `sidecar plugin changed mytool --collection rows`. It starts nothing and reads no configuration. |
| Search as you type | Nothing. The host debounces 250 ms and kills the superseded process group; you only ever see the query the user stopped on. |

Nothing refreshes while no tab of yours is visible, so a watch path is not a background cost.

## Reaching your plugin from a terminal

Once configured, your collections are addressable from any shell, which is what makes a plugin usable by an agent as well as a person:

```bash
sidecar open --plugin hello --collection greetings --split right
sidecar open --plugin hello --collection greetings --query bon
sidecar open --plugin hello --collection greetings --filter language=French   # one of your filters
sidecar open --plugin hello --collection greetings fr        # one row's document
```

The same shapes exist in the layout spec, so a whole screen can be composed in one call:

```json
{"kind": "resource", "provider": "hello", "collection": "greetings", "query": "bon",
 "filters": {"language": "French"}}
```

`sidecar layout get --json` reports the active tab's collection and query, so get → edit → apply is a round trip. A collection tab's identity is `{plugin, collection}` and excludes the query, so re-running `open` with a new query re-lists the tab that is open rather than forking a second one.

## Conformance: what to test against

Sidecar's own fixture plugin is the hostile-case reference. It lives at `internal/pluginhost/testdata/fixtureprovider` and simulates what the host has to survive: an `act` that never returns, a cursor that loops, over-limit pages, undeclared cells, strings trying to escape the table, watch paths outside the home directory. Reading its modes is the fastest way to see which of your own edges are already handled and which are yours.

Canonical request and response JSON for every method lives at `internal/pluginhost/testdata/protocol/` (`plugin-*.json`) and is meant to be vendored into your own tests. (It was published under `internal/resourceprovider/testdata/protocol/` before the package was renamed; that path is gone.)

Your own suite needs three things and not much more: that each method emits exactly one JSON object, that a failure is a typed error rather than a non-zero exit, and that every cell key is a column you declared. `sidecar plugin call --json` is the fixture for the last one.

## A checklist before you ship

- [ ] `describe` is local, fast, and answers `invalid_config` with a `setupHint` when unconfigured.
- [ ] Every collection has exactly one `primary` column, and a `secondary` one if a row's second line is worth reading.
- [ ] Every row has a stable `id`, and every cell key is a declared column.
- [ ] `outcome` is set deliberately, describes the ROW SET and nothing else, and an empty page says which of the four claims it is making.
- [ ] A `degraded` or `failed` page carries `coverage[]`, so the coverage modal can say which source could not answer.
- [ ] Every declared filter is read from `params.filters` with its default as the fallback, and the first one you declare is the scope you want shown.
- [ ] Every failure path prints a typed error and exits `0`.
- [ ] stdout carries one JSON object and nothing else — no logs, no banner, no progress.
- [ ] You declare only the context kinds you read, and only the views and sort keys you apply.
- [ ] Long work respects `deadlineMs` and returns `unavailable` rather than being killed.
- [ ] `sidecar plugin check <id> --list <collection>` and `--get` both come back `ok`.

## Where to go next

- [docs/reference/plugin-protocol.md](../../reference/plugin-protocol.md) — the contract: every field, every bound, every rule.
- [docs/reference/cli.md](../../reference/cli.md#sidecar-plugin) — the `sidecar plugin` verbs in full, with exit codes.
- [docs/reference/terminal-resource-provider-protocol.md](../../reference/terminal-resource-provider-protocol.md) — the frozen subset, if all you want is to recognise a key in terminal output.
- [The plugin ecosystem plan](../../plans/active/plugin-ecosystem/README.md) — what is settled, what is pending, and why the vocabulary is shaped the way it is.
- [`.claude/skills/create-plugin/SKILL.md`](../../../.claude/skills/create-plugin/SKILL.md) — the embedded class, if you decided you need a screen of your own.
