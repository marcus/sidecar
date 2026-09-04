---
sidebar_position: 1
title: Plugins
---

# Plugins

Put another tool's data inside Sidecar as a searchable tab, rows you open, and panes beside a terminal, without writing a terminal UI.

## What a plugin is

Sidecar hosts two classes of plugin through one descriptor. Which class a plugin belongs to decides who draws it, and nothing else: both are listed by the same command, enabled by the same config key, and placed in the same tabs and panes.

| | **Protocol plugin** | **Embedded plugin** |
|---|---|---|
| What it is | An executable, in any language, that answers JSON on stdout | A Go package compiled into Sidecar with its own Bubble Tea UI |
| Who renders | Sidecar, from what the plugin declares | The plugin |
| Ships how | One config entry you write. Sidecar needs no release | A Sidecar release |
| Good for | Browsing a tool's data: lists, search, documents, a few typed actions | A layout nothing else has: a board, a queue, a canvas |

Tasks and the td monitor are embedded plugins. Everything else on this page is about the protocol class, because that is the one you can add yourself.

A protocol plugin answers up to five one-shot methods (`describe`, `list`, `get`, `act`, and `resolve`) under the protocol identifier `sidecar.plugin/v1`. Sidecar writes one JSON request to the plugin's stdin, reads one JSON object back, and renders the result itself: the table, the cursor, the search box, the detail card, the keys, the theme, the persistence, and the live refresh. The plugin never draws a frame and never binds a key.

Only `describe` is required. A plugin that declares one collection and answers `list` is already a working tab.

## Turn the protocol on

Protocol plugins are behind the `plugin_protocol` feature flag.

```bash
sidecar --enable-feature=plugin_protocol
```

To keep it on, set it in `~/.config/sidecar/config.json`:

```json
{
  "features": {
    "flags": {
      "plugin_protocol": true
    }
  }
}
```

Every `sidecar plugin` verb that runs or configures a `plugins.external` plugin needs the flag; without it they exit `4` and say so. `sidecar plugin list` is the exception: it still lists those plugins, reporting each as inactive and naming the flag that is off. In the app, press `,` for Configuration and open **Feature Flags** under System.

## Configure one

There is no discovery. Sidecar never scans a directory, never runs every `sidecar-*` binary on your `PATH`, never auto-enables anything, and never lets a repository declare a plugin by being opened. Adding a plugin is one command that writes one config entry.

```bash
sidecar plugin add recall --command recall sidecar-plugin
```

Everything after `--command` is the argv, executed directly with no shell, so put it last. Nothing is started: `add` prints exactly what will run — every argv element on its own line, the working directory, and the variables it will pass by name — and asks you to confirm. `--yes` skips the question, which is what a script or an agent uses.

```bash
# pass one variable's current value through, and place its content in panes only
sidecar plugin add dex --pass-env DEX_PROFILE --placement panes --yes --command dex sidecar-plugin
```

The entry lands in `plugins.external`:

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

- `id` is the persisted key. Changing it orphans saved panes.
- `passEnv` names variables whose current values are inherited. Names only; a value is never written to config and never printed.
- `placements` is `tab`, `panes`, or both, and decides where the plugin's content can appear.
- `timeout` is the per-call timeout, clamped to 1s–60s.
- `claimHosts` are hostnames whose URLs this plugin may claim in terminal output.

Enablement is `plugins.<id>.enabled` for every plugin, embedded or protocol, and it is read at startup, so turning a plugin on or off needs a restart.

### What configuring a plugin means

**A process boundary is crash isolation, not a sandbox.** Configuring a plugin trusts that executable with your full OS privileges, exactly as running it in your shell would. Sidecar does not install, upgrade, verify, or restrict it; it only decides when to run it and what to send.

Configuring the plugin is the trust act, and it is the only one. There is no second prompt, no per-call grant, and no permission dialog. What the plugin can read from Sidecar is a declaration it makes in `describe` (`project` and `selection` are the only kinds this version has), and the host filters what it sends against that declaration at the process boundary. `sidecar plugin check ID` prints the kinds a plugin declared, so you can read what it asks for before you put it on a surface.

Plugin text is data, never markup: it can never become ANSI, a body is sanitized before and after Markdown rendering, and the only thing a plugin can supply that opens a URL is a validated `http(s)` `sourceUrl` you activate yourself.

## What appears

### A navbar tab

A plugin with the `tab` placement gets a global tab of its own, after Sessions and Activity. The first plugin-provided global tab is keyed `0`; a second takes no number key and is reached with `[` and `]`, a click, or the palette command `focus-<id>`.

The tab shows a loading state until `describe` lands, and a setup card if it fails, naming what went wrong rather than showing an empty list.

Inside the tab is the shared plugin browser: a query row, a list, and a detail box beside it. Every protocol plugin gets the same one, so the keys below are the same everywhere.

| Key | Action |
|-----|--------|
| `j` `k` `up` `down` | Move the cursor; the detail follows it |
| `pgup` `pgdn` `home` `end` | Page and jump |
| `enter` | Open the row; a second `enter` focuses the detail |
| `/` | Edit the query |
| `v` | View: sort keys, named views, and the collection's own filters |
| `c` | Coverage: what this page is claiming |
| `a` | Actions applicable to the row or the list |
| `o` | Open the row's source URL |
| `r` | Refresh |
| `+` `-` | Move the split between list and detail |

A key does nothing where the collection declared nothing behind it, and the footer stops advertising it: an inert control is not drawn.

### Collection and row panes

A plugin with the `panes` placement can put a collection into the Resource pane of a workspace, beside a terminal, on the project workspace and on the global Sessions surface alike.

```bash
# a collection tab beside the terminal, opened searched
sidecar open --plugin recall --collection results --query dex --split right

# the same tab, narrowed by one of the collection's own filters
sidecar open --plugin recall --collection results --query dex --filter profile=docs

# one row's document, as its own tab
sidecar open --plugin ongoing --collection projects recall

# a matched locator, through the plugin's matchers
sidecar open --plugin jira-work CASH-1245
```

`enter` on a row of a collection pane opens that row as a second tab in the same pane, re-keyed to the row's identity; a second `enter` focuses it and costs no process. The tabs are persisted, so a collection pane comes back after a relaunch with its query, view, sort, and filters intact.

The pane owns the keyboard while it is taking text: with the query row focused or an overlay open, the tab digits, `` ` ``, `[`, and `]` stay with the pane instead of switching tabs underneath your typing.

Layouts carry the same fields, so `layout get` → edit → `layout apply` is a round trip:

```bash
sidecar layout apply --pane '{"kind":"resource","provider":"recall","collection":"results","query":"dex"}'
```

## Filters, views, and the View modal

A collection declares its own choosers, and Sidecar draws them. Press `v` for the View modal: the sort keys the collection declared, its named views, and its filters as radio groups and text inputs. What you choose is persisted with the tab and sent back on every `list`.

The **first filter a collection declares is its scope**, the one that changes what a page *is* rather than merely narrowing it, and its current value is always folded into the View pill, because a page gathered under a scope nobody can see is a page whose emptiness means nothing. The pill reads as sort, then scope, then a count of the rest:

```
⇅ Relevance · This project · 2 filters
```

It sheds from the right as the pane narrows, down to a bare `⇅`, rather than being clipped mid-word.

Only the applied set is ever sent: a filter left at its own default is not sent at all, and a key the collection never declared (or a choice that is not one of its declared options) is dropped before the process starts. A plugin only ever reads names it published itself.

## Coverage, and what the outcome words mean

Every page a plugin returns carries an outcome, and Sidecar renders each one differently because they mean different things. An empty list is not one fact.

| Outcome | An empty page reads as |
|---------|------------------------|
| `answered` | Rows are what they are; nothing to explain |
| `abstained` | "No matches." Every source answered, so this is a fact about the query |
| `degraded` | "No matches, and coverage was incomplete." Some source could not answer, so this is not a fact about the query |
| `failed` | "Nothing could be asked." Every source needed failed, so the page says nothing about the query at all: an error card, never the words "no matches" |

An outcome Sidecar does not recognise reads as `degraded`, because of the two ways to be wrong about a claim it cannot understand, that is the one that does not invent a guarantee on the plugin's behalf.

The outcome describes the row set and nothing else. A complete list of thirteen sources, five of them unhealthy, is `answered`; the health of what the rows describe belongs on the rows, in their own status pills.

Press `c`, or click the outcome cell or a notice, for the **coverage modal**: the outcome word, Sidecar's own sentence for what it means, every notice untruncated, and, when the plugin sent one, a per-source table of Source, State, Elapsed, and the plugin's own reason. Rows the plugin held back appear as data rather than free text: "8 shown · 1 below floor · 6 over budget". Retry is a button, bound to `r`.

A collection whose search is required and that nobody has typed into is waiting for a query, not abstaining, and says so; the coverage modal is unavailable there, because the page has made no claim to explain.

## Staying fresh without a resident process

Every call is one process, and nothing about that is visible in use.

| What happens | How |
|---|---|
| You type in the query row | Sidecar debounces 250 ms, sends one `list`, and kills the superseded call's whole process group |
| The plugin's data changes on disk | The plugin declares `refresh.watch` paths; Sidecar watches them while a tab of that plugin is on screen and re-lists when they change |
| The plugin cannot name a path | `refresh.everySeconds`, clamped to 15–900 and polled only while a tab is visible |
| The tool wants to poke Sidecar | `sidecar plugin changed <id> [--collection C]` from a shell hook or the tool's own daemon |
| The tool is slow | The previous page stays under a refreshing badge; a list is never blanked to wait |

Nothing refreshes while no tab from that plugin is on screen, so a plugin you are not looking at costs nothing.

## Remote surfaces

Which machine runs the plugin depends on which surface asked, and the rule is the same one every other content kind follows: the machine that owns the pane answers for it.

**A collection or row pane bound to a [remote host](remote-hosts.md) asks that host's plugins, not this machine's.** Sidecar goes over the connection it already holds open, the host runs its own configured plugin, and what comes back is the page *it* kept, already sanitized and already bounded. So the plugin has to be configured on the host, in the host's own `plugins.external`; a pane that quietly listed local data while saying it was showing a remote workspace would be answering a question nobody asked.

**A plugin's navbar tab runs on the machine you are typing into.** When the project it is asked about is bound to a remote host, the project context carries that host's ID and that host's paths — Sidecar never substitutes a local path for a remote one. A plugin that only knows this machine should say so with a typed `unavailable` error naming the host, which is the same rule Sidecar's own content sources follow.

## Inspect and troubleshoot from a terminal

Hosting plugins is something Sidecar owns, so every part of it has a non-interactive path. See the [CLI reference](cli-reference.md#sidecar-plugin) for the full surface.

```bash
# configuration only: no running Sidecar, no PATH lookup, no subprocess
sidecar plugin list --json

# opt in to running describe on each active plugin
sidecar plugin list --describe --json

# is this plugin configured, startable, and speaking the protocol?
sidecar plugin check recall

# ...and actually call it, explicitly, because this can hit the network
sidecar plugin check recall --list results --query dex --filter profile=docs
sidecar plugin check recall --get results rc:notes:1 --json

# one raw method through the host's own envelope and sanitization
sidecar plugin call recall list --params '{"collection":"results","query":"dex"}' --json
```

`check` and `call` print only what the host kept, never the plugin's raw stdout: every string has been through the same sanitization and bounds a pane applies, so what you see is what a pane would draw. `--list` and `--get` are separate explicit flags and never implied, because they can perform network access and print private data.

To turn a plugin off without losing its argv:

```bash
sidecar plugin disable recall
sidecar plugin enable recall
sidecar plugin remove recall
```

## Writing one

- [The protocol reference](https://github.com/marcus/sidecar/blob/main/docs/reference/plugin-protocol.md) is the contract: every method, field, bound, and rule of `sidecar.plugin/v1`.
- [Creating a Sidecar plugin](https://github.com/marcus/sidecar/blob/main/docs/guides/active/creating-plugins.md) is the walkthrough, from choosing a class to a plugin that passes `sidecar plugin check`.
- [`hello-plugin`](https://github.com/marcus/sidecar/tree/main/docs/guides/examples/hello-plugin) is a complete working example: 200 lines of Python, no dependencies, driven through the real host by a test on every build.

## Terminal resource providers

Before the plugin protocol there was `sidecar.terminal-resource/v1`: a provider that recognises ticket keys such as `CASH-1245` in terminal output, agent logs, and Markdown, and answers `describe` and `resolve` so Sidecar can open the ticket as a Resource card in an adjacent pane.

**That protocol is frozen and keeps working, unchanged.** A provider written against it needs no edit and no migration.

Its configuration stays where it is, under `terminalResources.providers`, which is now a read-only alias: Sidecar still reads it and still dispatches those entries on `sidecar.terminal-resource/v1`, and `sidecar plugin list` reports which config section each external plugin was read from, so the answer is verifiable. The section is not rewritten into `plugins.external`, because an entry moved there would be asked the new identifier.

Which protocol an executable is asked comes from the config section it is written in and never from anything the executable says. `plugin remove` will not touch a `terminalResources.providers` entry, and says so.

`sidecar terminal-links` remains the surface for that section:

```bash
sidecar terminal-links list --describe --json
sidecar terminal-links check jira-work --resolve CASH-1245 --json
```

A provider that only declares matchers is already a protocol plugin in every way that matters to the host: it simply declares no collections. When you want lists, search, documents, or actions from the same tool, write `sidecar.plugin/v1` and configure it under `plugins.external`; the matchers you already have carry over unchanged.

The frozen contract is documented in [the terminal resource provider protocol reference](https://github.com/marcus/sidecar/blob/main/docs/reference/terminal-resource-provider-protocol.md).
