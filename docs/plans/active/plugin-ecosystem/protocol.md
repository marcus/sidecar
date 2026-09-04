# Why the plugin protocol has the shape it has

**Status:** design rationale for `sidecar.plugin/v1`, opened 2026-09-02, frozen 2026-09-03. **The contract itself is [docs/reference/plugin-protocol.md](../../../reference/plugin-protocol.md)** — one authority, as the frozen [terminal resource provider protocol](../../../reference/terminal-resource-provider-protocol.md) already is for resource v1. This file keeps only the arguments behind that contract, so a rule and its reason cannot drift apart in two places. **Controlling document:** [README.md](README.md). **Host architecture:** [host.md](host.md). **Authoring guide:** [docs/guides/active/creating-plugins.md](../../../guides/active/creating-plugins.md).

The revisions the M0 mockups and recall's implementation surfaced are the [pending table in README.md](README.md#protocol-revisions-pending-from-the-m0-recall-mockup). M4b applied four of them — `filters[]`, `page.omitted`, the `failed` outcome and `page.coverage[]` — and they now live in [the reference](../../../reference/plugin-protocol.md); what is still in the table is unapplied and waits for M4d's freeze.

## Why not a generic UI catalog

A2UI and its Bubble Tea renderer (a2tea) were considered as the vocabulary. They share this protocol's posture: a pre-approved component catalog, no code across the trust boundary, an action loop with IDs and responses. Three things are borrowed: that posture, the action/response shape (`act` returns an outcome addressed to the surface that asked), and the principle that the host renders the catalog in its own theme with monochrome-safe fallbacks.

What is not borrowed is the catalog itself. A2UI's components (buttons, text fields, sliders, images, modals in a tree) describe a form an agent generated for one turn of a chat. Sidecar's plugins are browsers over a tool's data that live for a session: they need a cursor, sorting, views, paging, tabs, persistence across relaunch, content links, and live refresh, all owned by the host so they behave identically across every plugin and both workspace projections. A generic widget tree pushes those into each plugin and makes parity a per-plugin promise. Domain-shaped objects (collection, row, resource, section, action) keep them host-owned.

If a plugin ever needs a layout the vocabulary cannot express, the answer is one new typed object with host rendering, or the embedded class. The `body` field's `format` is the extension point if a block vocabulary is ever wanted: a `blocks` format could carry an A2UI-style adjacency list without changing any envelope.

## Why one-shot invocation

The user's bar is that one-shot invocation is acceptable only if live search, mutations, and background updates are still reachable. Each is, from one-shot calls plus a host mechanism Sidecar already had — debounce and process-group cancellation for search, `act` plus a `refresh` list for mutations, `livewatch` and a poll interval for background change, and the `uirequest` bus for a plugin that wants to poke Sidecar itself. The reference's [Freshness](../../../reference/plugin-protocol.md#freshness-live-behaviour-without-a-resident-process) table is the result.

Nothing there needs a socket, and paying for a resident transport before a measurement says process startup is the cost would be paying for it twice.

## Deferred to evidence

- **Resident mode.** `describe` gains `"transport": ["oneshot", "resident"]`; the host keeps one process per instance speaking newline-delimited JSON-RPC with the same method and result objects, plus a `changed` notification from plugin to host. Added only when measured startup cost, not tool latency, is the problem on a real plugin.
- **Nested trees and boards.** A `children` cursor on rows, and grouped columns. Added when a real plugin needs them.
- **Plugin-declared content links inside bodies.** Bodies are sanitized to plain text links today; body-link activation arrives only through host-owned hit regions over validated destinations.
- **Selection context on remote surfaces**, streaming pages, and binary attachments.

## Fixtures

The fixture plugin exists because the properties this protocol has to hold — process groups, timeouts, stdout discipline, cancellation, bounded hostile input — are exactly the ones an in-memory fake cannot have. So `internal/pluginhost/testdata/fixtureprovider` is a real executable the test binary builds, it speaks both protocol identifiers from one binary (itself the property under test), and every hostile case is a mode it can be asked for rather than a hand-written response. The conformance suite drives it through the real manager.

Its mode list, the canonical JSON under `internal/pluginhost/testdata/protocol/`, and the note that the published path moved there from `internal/resourceprovider/` are in the reference's [Fixtures](../../../reference/plugin-protocol.md#fixtures) section. The smallest complete plugin an author can copy is `docs/guides/examples/hello-plugin/`.
