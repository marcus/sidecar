# Herdr detection sync report

Generated 2026-10-05T14:42:36Z by `go run ./internal/tools/herdrsync`.

| Field | Value |
| --- | --- |
| Herdr repository | https://github.com/herdrdev/herdr |
| Ref vendored | `master` |
| Commit | `b06480673bee704d143f48962c288aa1512c316d` |
| Pinned release for the differential harness | `v0.9.3` |
| Catalog | https://herdr.dev/agent-detection/index.toml |
| Catalog ETag | `W/"e73307bc29c98eec95cac5cf610317c7"` |
| Sidecar manifest engine version | 3 |
| Manifests vendored | 22 |

## Version changes

| Agent | Before | After | Change |
| --- | --- | --- | --- |
| `codex` | 2026.09.23.1 | 2026.10.01.1 | bumped |
| `pi` | 2026.09.14.1 | 2026.10.01.1 | bumped |

## File changes

2 file(s) changed, 20 unchanged.

- `upstream/codex.toml` (2026.10.01.1, 3851 bytes, 9 rules)
- `upstream/pi.toml` (2026.10.01.1, 624 bytes, 2 rules)

## Published versus bundled

Each row is the copy a Herdr client would load, and why.

| Agent | Vendored from | Bundled | Published | Reason |
| --- | --- | --- | --- | --- |
| `agy` | published | 2026.06.24.1 | 2026.06.24.1 | published and bundled are both 2026.06.24.1; a Herdr client prefers the remote copy |
| `amp` | published | 2026.07.09.1 | 2026.07.09.1 | published and bundled are both 2026.07.09.1; a Herdr client prefers the remote copy |
| `claude` | published | 2026.09.11.1 | 2026.09.11.1 | published and bundled are both 2026.09.11.1; a Herdr client prefers the remote copy |
| `cline` | published | 2026.09.11.1 | 2026.09.11.1 | published and bundled are both 2026.09.11.1; a Herdr client prefers the remote copy |
| `codex` | published | 2026.10.01.1 | 2026.10.01.1 | published and bundled are both 2026.10.01.1; a Herdr client prefers the remote copy |
| `copilot` | published | 2026.08.29.1 | 2026.08.29.1 | published and bundled are both 2026.08.29.1; a Herdr client prefers the remote copy |
| `cursor` | published | 2026.08.03.1 | 2026.08.03.1 | published and bundled are both 2026.08.03.1; a Herdr client prefers the remote copy |
| `devin` | published | 2026.06.15.1 | 2026.06.15.1 | published and bundled are both 2026.06.15.1; a Herdr client prefers the remote copy |
| `droid` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `gemini` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `grok` | bundled | 2026.09.18.2 | 2026.09.18.1 | bundled 2026.09.18.2 is newer than published 2026.09.18.1; a Herdr client ignores the older remote copy |
| `hermes` | published | 2026.07.24.1 | 2026.07.24.1 | published and bundled are both 2026.07.24.1; a Herdr client prefers the remote copy |
| `kilo` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `kimi` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `kiro` | published | 2026.09.19.1 | 2026.09.19.1 | published and bundled are both 2026.09.19.1; a Herdr client prefers the remote copy |
| `letta` | published | 2026.08.24.1 | 2026.08.24.1 | published and bundled are both 2026.08.24.1; a Herdr client prefers the remote copy |
| `maki` | published | 2026.07.09.2 | 2026.07.09.2 | published and bundled are both 2026.07.09.2; a Herdr client prefers the remote copy |
| `muse` | published | 2026.08.26.1 | 2026.08.26.1 | published and bundled are both 2026.08.26.1; a Herdr client prefers the remote copy |
| `opencode` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `pi` | published | 2026.10.01.1 | 2026.10.01.1 | published and bundled are both 2026.10.01.1; a Herdr client prefers the remote copy |
| `qodercli` | published | 2026.06.10.1 | 2026.06.10.1 | published and bundled are both 2026.06.10.1; a Herdr client prefers the remote copy |
| `qwen` | published | 2026.08.14.1 | 2026.08.14.1 | published and bundled are both 2026.08.14.1; a Herdr client prefers the remote copy |

## Regex compatibility

3 pattern(s) that Rust's `regex` crate accepts cannot compile under Go's RE2. The vendored files keep them verbatim; an overlay carries the rewrite. See `docs/reference/herdr-detection-parity.md`.

- `upstream/antigravity.toml` rule `spinner_working` line_regex: `^\s*[\u2800-\u28FF]+\s+\p{Alphabetic}+\w*ing\b`
  - error parsing regexp: invalid character class range: `\p{Alphabetic}`
- `upstream/cursor.toml` rule `spinner_working` line_regex: `^\s*(⬡|⬢|[\u2800-\u28FF]+)\s+\p{Alphabetic}+\w*ing\b`
  - error parsing regexp: invalid character class range: `\p{Alphabetic}`
- `upstream/qodercli.toml` rule `spinner_working` line_regex: `^\s*[\u2800-\u28FF]\s+.*\p{Alphabetic}`
  - error parsing regexp: invalid character class range: `\p{Alphabetic}`

## Alias table

24 agents in Herdr's `lookup_agent`; generic runtimes: `bash`, `bun`, `cmd`, `fish`, `node`, `powershell`, `pwsh`, `sh`, `tmux`, `zsh` (plus python, or python<segment>[.<segment>...] where every dot-separated segment after the prefix is a non-empty run of ASCII digits (is_python_runtime)).

Every Herdr alias for a family Sidecar already claims appears literally in `internal/agentactivity/activity.go`.

## Authority gaps

Herdr's published authority is a *target*. Sidecar tiers are earned by traces and are never copied.

**Below target** marks an agent Herdr gives lifecycle authority to *through hooks* and Sidecar has not proved `full` for. That is the same rule `TestHerdrAuthorityGaps` prints, so this table and `go test ./internal/agentlifecycle/` name the same set.

| Agent | Herdr authority | Sidecar tier | Below target |
| --- | --- | --- | --- |
| `agy` | session_identity | session-identity |  |
| `amp` | none | screen-fallback |  |
| `claude` | session_identity | session-identity |  |
| `cline` | none | — |  |
| `codex` | session_identity | session-identity |  |
| `copilot` | session_identity | session-identity |  |
| `cursor` | session_identity | session-identity |  |
| `devin` | session_identity | screen-fallback |  |
| `droid` | session_identity | screen-fallback |  |
| `gemini` | none | — |  |
| `grok` | session_identity | session-identity |  |
| `hermes` | session_identity | session-identity |  |
| `kilo` | hooks | advisory | yes |
| `kimi` | hooks | advisory | yes |
| `kiro` | none | — |  |
| `letta` | session_identity | — |  |
| `maki` | none | — |  |
| `mastracode` | hooks | advisory | yes |
| `muse` | none | screen-fallback |  |
| `omp` | hooks | advisory | yes |
| `opencode` | hooks | full |  |
| `pi` | hooks | advisory | yes |
| `qodercli` | session_identity | screen-fallback |  |
| `qwen` | session_identity | session-identity |  |

## Integration assets

Vendored verbatim from `src/integration/assets` into `internal/agentintegration/upstream/`, pinned by `upstream.lock.json` there. They are reference material: Sidecar installs its own assets and these exist so a re-port is a diff.

| Agent | Asset directory | Version | Previous | Change | Sidecar port |
| --- | --- | --- | --- | --- | --- |
| `agy` | `antigravity_cli` | 3 | 3 | unchanged | `antigravity` from version 3 |
| `claude` | `claude` | 10 | 10 | unchanged | `claude` from version 9 |
| `codex` | `codex` | 9 | 9 | unchanged | `codex` from version 8 |
| `copilot` | `copilot` | 3 | 3 | unchanged | `copilot` from version 3 |
| `cursor` | `cursor` | 1 | 1 | unchanged | `cursor` from version 1 |
| `devin` | `devin` | 2 | 2 | unchanged | `devin` from version 2 |
| `droid` | `droid` | 3 | 3 | unchanged | `droid` from version 3 |
| `grok` | `grok` | 2 | 2 | unchanged | `grok` from version 1 |
| `hermes` | `hermes` | 5 | 5 | unchanged | `hermes` from version 5 |
| `kilo` | `kilo` | 4 | 4 | unchanged | `kilo` from version 4 |
| `kimi` | `kimi` | 7 | 7 | unchanged | `kimi` from version 7 |
| `letta` | `letta` | 1 | 1 | unchanged | not ported |
| `mastracode` | `mastracode` | 2 | 2 | unchanged | `mastracode` from version 2 |
| `omp` | `omp` | 10 | 10 | unchanged | `omp` from version 9 |
| `opencode` | `opencode` | 13 | 13 | unchanged | `opencode` from version 10 |
| `pi` | `pi` | 9 | 9 | unchanged | `pi` from version 8 |
| `qodercli` | `qodercli` | 3 | 3 | unchanged | `qodercli` from version 3 |
| `qwen` | `qwen` | 1 | 1 | unchanged | `qwen` from version 1 |

### Upstream changes since each Sidecar port

`ported-from` is recorded in `internal/agentintegration/portedfrom.go`, not in an asset header: two of the three Sidecar assets are Go values with no header to carry it. A comparison is made on bytes rather than on the version number, so a file upstream edited without bumping still shows here.

#### `opencode` — ported from herdr `opencode` version 10

Compared against `4a3b04f5`; upstream is now at version 13.

`upstream/opencode/herdr-agent-state.js` (diff):

```diff
--- src/integration/assets/opencode/herdr-agent-state.js @ 4a3b04f5
+++ src/integration/assets/opencode/herdr-agent-state.js @ b0648067
@@
 // managed by herdr; reinstalling or updating the integration overwrites this file.
 // add custom hooks/plugins beside this file instead of editing it.
 // HERDR_INTEGRATION_ID=opencode
-// HERDR_INTEGRATION_VERSION=10
+// HERDR_INTEGRATION_VERSION=13
 
 import net from "node:net";
 
@@
 let reportedRootSessionID;
 
 // Track child sessions so their events cannot replace the pane's root session.
-// Their user prompts still project state without attaching the child session id.
-const childSessions = new Set();
+// User prompts carry the root id to preserve its identity and cross-talk guard.
+const childSessions = new Map();
 const CHILD_EVENT_STATES = new Map([
   ["permission.asked", "blocked"],
   ["question.asked", "blocked"],
@@
   return request("pane.report_agent", params);
 }
 
+function ownsLocalLifecycle() {
+  const args = process.argv.slice(2);
+  const separator = args.indexOf("--");
+  if (separator !== -1) args.splice(separator);
+  if (args.some((arg) => arg === "--attach" || arg.startsWith("--attach="))) return false;
+  while (args[0] === "--print-logs" || args[0] === "--log-level" || args[0]?.startsWith("--log-level=")) {
+    args.splice(0, args[0] === "--log-level" ? 2 : 1);
+  }
+  // These local clients have no TUI plugin. Shared servers and the TUI worker
+  // cannot identify their attached panes; their lifecycle belongs to each TUI.
+  return args[0] === "run" ||
+    (!["serve", "web", "attach"].includes(args[0]) && args.includes("--mini"));
+}
+
 export const HerdrAgentStatePlugin = async () => {
   if (
+    !ownsLocalLifecycle() ||
     process.env.HERDR_ENV !== "1" ||
     !process.env.HERDR_SOCKET_PATH ||
     !process.env.HERDR_PANE_ID
@@
 
       const info = properties.info;
       if (info?.id && info.parentID) {
-        childSessions.add(info.id);
+        childSessions.set(info.id, info.parentID);
       }
       if (sessionID && childSessions.has(sessionID)) {
         const state = CHILD_EVENT_STATES.get(type);
         if (state) {
-          await reportState(state);
+          let rootSessionID = sessionID;
+          while (childSessions.has(rootSessionID)) {
+            rootSessionID = childSessions.get(rootSessionID);
+          }
+          await reportState(state, rootSessionID);
         }
         return;
       }
@@
     },
   };
 };
+
+// V1 local run/Mini retain their server hooks. V1/V2 full TUIs own both
+// selection and lifecycle, including when attached to a shared remote server.
+export default {
+  id: "herdr.opencode",
+  server: HerdrAgentStatePlugin,
+  setup() {},
+};
```

`upstream/opencode/herdr-agent-state.test.ts` (diff):

```diff
--- src/integration/assets/opencode/herdr-agent-state.test.ts @ 4a3b04f5
+++ src/integration/assets/opencode/herdr-agent-state.test.ts @ b0648067
-import { beforeEach, expect, mock, test } from "bun:test";
+import { afterEach, beforeEach, expect, mock, test } from "bun:test";
 
+const originalArgv = process.argv;
+afterEach(() => { process.argv = originalArgv; });
+
 const requests: unknown[] = [];
 const clients: FakeClient[] = [];
 const requestWaiters: Array<() => void> = [];
@@
   clients.length = 0;
   requestWaiters.length = 0;
   autoAcknowledge = true;
+  process.argv = ["bun", "/$bunfs/root/src/index.js", "run"];
   process.env.HERDR_ENV = "1";
   process.env.HERDR_SOCKET_PATH = "test.sock";
   process.env.HERDR_PANE_ID = "test:p1";
@@
     event: {
       type: "session.created",
       properties: {
-        sessionID: "child-session",
         info: { id: "child-session", parentID: "root-session" },
       },
     },
@@
     "working",
   ]);
   expect(requests.map(requestSessionID)).toEqual([
-    undefined,
-    undefined,
-    undefined,
-    undefined,
-    undefined,
+    "root-session",
+    "root-session",
+    "root-session",
+    "root-session",
+    "root-session",
   ]);
 });
 
+test("routes nested child prompts to their own root, not the last active root", async () => {
+  const plugin = await loadPlugin();
+  for (const info of [
+    { id: "child-session", parentID: "root-session" },
+    { id: "nested-session", parentID: "child-session" },
+  ]) {
+    await plugin.event({ event: { type: "session.created", properties: { info } } });
+  }
+  await plugin["chat.message"]({ sessionID: "other-root" });
+  await plugin.event({
+    event: { type: "permission.asked", properties: { sessionID: "nested-session" } },
+  });
+  await plugin.event({
+    event: { type: "permission.replied", properties: { sessionID: "nested-session" } },
+  });
+  await plugin.event({
+    event: { type: "session.idle", properties: { sessionID: "nested-session" } },
+  });
+  await plugin["chat.message"]({ sessionID: "nested-session" });
+
+  expect(requests.map(requestState)).toEqual(["working", "blocked", "working"]);
+  expect(requests.map(requestSessionID)).toEqual([
+    "other-root",
+    "root-session",
+    "root-session",
+  ]);
+});
+
+test("only local run and Mini own server lifecycle, never shared servers or TUI workers", async () => {
+  for (const args of [
+    ["run"], ["run", "--session", "existing"], ["--mini"], ["--mini", "--session", "existing"],
+    ["--print-logs", "--log-level", "DEBUG", "run"], ["run", "--", "--attach"],
+  ]) {
+    process.argv = ["bun", "/$bunfs/root/src/index.js", ...args];
+    expect((await loadPlugin()).event).toBeFunction();
+  }
+  for (const args of [
+    [], ["--session", "existing"], ["serve"], ["web"], ["attach", "http://localhost:4096"],
+    ["run", "--attach", "http://localhost:4096"], ["--mini", "--attach=http://localhost:4096"],
+    ["serve", "--", "--mini"],
+  ]) {
+    process.argv = ["bun", "/$bunfs/root/src/index.js", ...args];
+    expect(await loadPlugin()).toEqual({});
+  }
+  process.argv = ["bun", "/$bunfs/root/src/cli/tui/worker.js"];
+  expect(await loadPlugin()).toEqual({});
+  expect(requests).toHaveLength(0);
+});
+
 function requestMethod(request: unknown): unknown {
   return isRecord(request) ? request.method : undefined;
 }
 
+test("dual server entrypoint keeps V1 hooks and never reports from the V2 shared server", async () => {
+  const module = await import(`./herdr-agent-state.js?test=${++importCounter}`);
+  expect(module.default.server).toBe(module.HerdrAgentStatePlugin);
+  expect(await module.default.setup({})).toBeUndefined();
+  expect(requests).toHaveLength(0);
+  const hooks = await module.default.server();
+  await hooks["chat.message"]({ sessionID: "v1-root" });
+  expect(requests.map(requestState)).toEqual(["working"]);
+});
+
 function requestState(request: unknown): unknown {
   return requestParam(request, "state");
 }
```

`upstream/opencode/herdr-tui-session.js` (diff):

```diff
--- src/integration/assets/opencode/herdr-tui-session.js @ 4a3b04f5
+++ src/integration/assets/opencode/herdr-tui-session.js @ b0648067
 // installed by herdr
 // managed by herdr; reinstalling or updating the integration overwrites this file.
 // HERDR_INTEGRATION_ID=opencode-tui
-// HERDR_INTEGRATION_VERSION=10
+// HERDR_INTEGRATION_VERSION=13
 
 import net from "node:net";
 
@@
 const ROUTE_POLL_INTERVAL_MS = 100;
 const SELECTION_RETRY_DELAYS_MS = [100, 400, 1_000];
 
-function requestOnce(sessionID) {
+function requestOnce(sessionID, state, seq, isCurrent = () => true) {
   const paneId = process.env.HERDR_PANE_ID;
   const socketPath = process.env.HERDR_SOCKET_PATH;
   if (!paneId || !socketPath) {
-    return Promise.resolve();
+    return Promise.resolve(true);
   }
 
   const socketEndpoint =
@@
     id: `${SOURCE}:tui:${Date.now()}:${Math.floor(Math.random() * 1_000_000)
       .toString()
       .padStart(6, "0")}`,
-    method: "pane.report_agent_session",
+    method: state === undefined ? "pane.report_agent_session" : "pane.report_agent",
     params: {
       pane_id: paneId,
       source: SOURCE,
       agent: AGENT,
       agent_session_id: sessionID,
-      session_start_source: "select",
+      ...(state === undefined ? { session_start_source: "select" } : { state, seq }),
     },
   };
 
   return new Promise((resolve) => {
+    let settled = false;
+    let timer;
+    const settle = (delivered) => {
+      if (settled) return;
+      settled = true;
+      clearTimeout(timer);
+      client.destroy();
+      resolve(delivered);
+    };
     const client = net.createConnection(socketEndpoint, () => {
+      if (!isCurrent()) {
+        settle(false);
+        return;
+      }
       client.write(`${JSON.stringify(request)}\n`);
     });
-    const finish = () => {
-      client.destroy();
-      resolve();
-    };
 
-    client.setTimeout(500, finish);
-    client.on("data", finish);
-    client.on("error", finish);
-    client.on("end", finish);
-    client.on("close", resolve);
+    // A plain timer, not socket.setTimeout, so a connection that never finishes
+    // connecting still settles and cannot block later reports behind the queue.
+    timer = setTimeout(() => settle(false), 500);
+    timer.unref?.();
+    client.on("data", () => settle(true));
+    client.on("error", () => settle(false));
+    client.on("end", () => settle(false));
+    client.on("close", () => settle(false));
   });
 }
 
 export default {
   id: "herdr.opencode.session-selection",
-  tui: async (api) => {
-    if (
-      process.env.HERDR_ENV !== "1" ||
-      !process.env.HERDR_SOCKET_PATH ||
-      !process.env.HERDR_PANE_ID
-    ) {
+  // Keep this plain object dependency-free: V1 and V2 expose different SDK
+  // packages, but both loaders accept their own lifecycle entry on this object.
+  setup,
+  tui,
+};
+
+async function tui(api) {
+  if (process.env.HERDR_ENV !== "1" || !process.env.HERDR_SOCKET_PATH || !process.env.HERDR_PANE_ID) return;
+
+  let disposed = false;
+  let context;
+  let sequence = Date.now() * 1000;
+  let chain = Promise.resolve();
+
+  function routeID() {
+    const route = api.route.current;
+    return route?.name === "session" ? route.params?.sessionID : undefined;
+  }
+
+  function current(ctx) {
+    return !disposed && context === ctx && routeID() === ctx.route;
+  }
+
+  async function read(ctx, request) {
+    const result = await request({
+      signal: AbortSignal.any([ctx.controller.signal, AbortSignal.timeout(5_000)]),
+      throwOnError: true,
+    });
+    if (!current(ctx) || result?.data === undefined) throw new Error("session data unavailable");
+    return result.data;
+  }
+
+  // The first selected session owns this pane's subtree. Browsing descendants
+  // keeps that boundary; directly attaching to a child does not claim siblings.
... diff truncated at 120 of 666 lines; run `git diff` between the two commits named above in a Herdr checkout for the rest.
```

`upstream/opencode/herdr-tui-session.test.ts` (diff):

```diff
--- src/integration/assets/opencode/herdr-tui-session.test.ts @ 4a3b04f5
+++ src/integration/assets/opencode/herdr-tui-session.test.ts @ b0648067
@@
 const requests: unknown[] = [];
 const activeDisposers: Array<() => void> = [];
 const requestWaiters: Array<() => void> = [];
+const stateWaiters: Array<() => void> = [];
 let importCounter = 0;
+let holdConnections = false;
+let failConnections = false;
+const connections: Array<() => void> = [];
 
 mock.module("node:net", () => ({
   default: {
     createConnection(_path: string, onConnect: () => void) {
       const handlers = new Map<string, () => void>();
       const client = {
+        destroyed: false,
         write(input: string) {
-          requests.push(JSON.parse(input.trim()));
+          if (client.destroyed) return;
+          const request = JSON.parse(input.trim());
+          requests.push(request);
+          if (isRecord(request) && isRecord(request.params) && request.params.state !== undefined) {
+            stateWaiters.shift()?.();
+          }
           requestWaiters.shift()?.();
           queueMicrotask(() => client.emit("data"));
         },
@@
         on(event: string, handler: () => void) {
           handlers.set(event, handler);
         },
-        destroy() {},
+        destroy() {
+          client.destroyed = true;
+        },
         emit(event: string) {
           handlers.get(event)?.();
         },
       };
-      queueMicrotask(onConnect);
+      if (holdConnections) connections.push(onConnect);
+      else if (failConnections) queueMicrotask(() => client.emit("error"));
+      else queueMicrotask(onConnect);
       return client;
     },
   },
@@
 beforeEach(() => {
   requests.length = 0;
   requestWaiters.length = 0;
+  stateWaiters.length = 0;
+  holdConnections = false;
+  failConnections = false;
+  connections.length = 0;
   process.env.HERDR_ENV = "1";
   process.env.HERDR_SOCKET_PATH = "test.sock";
   process.env.HERDR_PANE_ID = "test:p1";
@@
 
 function fakeApi() {
   const sessions = new Map<string, { id: string; parentID?: string }>();
+  const statuses: Record<string, { type: string }> = {};
+  const permissions: Array<{ id: string; sessionID: string; tool?: { messageID: string; callID: string } }> = [];
+  const questions: typeof permissions = [];
+  const messages = new Map<string, { info?: { error?: { name: string } }; parts: Array<object> }>();
+  const listeners = new Map<string, Set<(event: object) => void>>();
+  const calls: string[] = [];
   let current: { name: string; params?: { sessionID: string } } = { name: "home" };
   let dispose: (() => void) | undefined;
   activeDisposers.push(() => dispose?.());
 
   return {
+    statuses, permissions, questions, messages, listeners, calls,
+    emit(type: string, properties: object) {
+      for (const receive of listeners.get(type) ?? []) receive({ type, properties });
+    },
     api: {
+      client: {
+        session: {
+          async get({ sessionID }: { sessionID: string }) {
+            calls.push(`get:${sessionID}`);
+            const data = sessions.get(sessionID);
+            if (!data) throw new Error("session not found");
+            return { data };
+          },
+          async status() { calls.push("status"); return { data: { ...statuses } }; },
+          async message({ messageID }: { sessionID: string; messageID: string }) {
+            calls.push(`message:${messageID}`);
+            const data = messages.get(messageID);
+            if (!data) throw new Error("message unavailable");
+            return { data };
+          },
+        },
+        permission: { async list() { return { data: [...permissions] }; } },
+        question: { async list() { return { data: [...questions] }; } },
+      },
+      event: {
+        on(type: string, receive: (event: object) => void) {
+          if (!listeners.has(type)) listeners.set(type, new Set());
+          listeners.get(type)!.add(receive);
+          return () => listeners.get(type)!.delete(receive);
+        },
+      },
       route: {
         get current() {
           return current;
@@
     select(sessionID: string) {
       current = { name: "session", params: { sessionID } };
     },
+    home() { current = { name: "home" }; },
     dispose() {
       dispose?.();
     },
@@
   return new Promise((resolve) => requestWaiters.push(resolve));
 }
 
... diff truncated at 120 of 626 lines; run `git diff` between the two commits named above in a Herdr checkout for the rest.
```

`upstream/opencode/tui.js` (current upstream file):

```diff
upstream had no src/integration/assets/opencode/tui.js at 4a3b04f5; this file is new since the port.
```

#### `codex` — ported from herdr `codex` version 8

Compared against `4a3b04f5`; upstream is now at version 9.

`upstream/codex/herdr-agent-state.ps1` (diff):

```diff
--- src/integration/assets/codex/herdr-agent-state.ps1 @ 4a3b04f5
+++ src/integration/assets/codex/herdr-agent-state.ps1 @ b0648067
@@
 # managed by herdr; reinstalling or updating the integration overwrites this file.
 # add custom hooks beside this file instead of editing it.
 # HERDR_INTEGRATION_ID=codex
-# HERDR_INTEGRATION_VERSION=8
+# HERDR_INTEGRATION_VERSION=9
 
 param([string]$Action = "")
 
-if ($Action -ne "session") { exit 0 }
+if ($Action -notin @("session", "working", "idle")) { exit 0 }
 if ($env:HERDR_ENV -ne "1") { exit 0 }
 if ([string]::IsNullOrWhiteSpace($env:HERDR_PANE_ID)) { exit 0 }
 
@@
     exit 0
 }
 
-if ($payload.hook_event_name -and $payload.hook_event_name -ne "SessionStart") { exit 0 }
+$expectedEvents = @{ session = @("SessionStart"); working = @("UserPromptSubmit"); idle = @("Stop", "Interrupt") }[$Action]
+if ($payload.hook_event_name -and $payload.hook_event_name -notin $expectedEvents) { exit 0 }
 
 $sessionId = $payload.session_id
 if ([string]::IsNullOrWhiteSpace($sessionId)) { exit 0 }
-if ([string]::IsNullOrWhiteSpace($payload.transcript_path)) { exit 0 }
+if ($Action -eq "session" -and [string]::IsNullOrWhiteSpace($payload.transcript_path)) { exit 0 }
 if (-not [string]::IsNullOrWhiteSpace($env:CODEX_THREAD_ID) -and $env:CODEX_THREAD_ID -ne $sessionId) { exit 0 }
 
-$seq = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
+$seq = [DateTimeOffset]::UtcNow.Ticks
 $herdr = if ([string]::IsNullOrWhiteSpace($env:HERDR_BIN_PATH)) { "herdr" } else { $env:HERDR_BIN_PATH }
 try {
     $args = @(
         "pane",
-        "report-agent-session",
+        $(if ($Action -eq "session") { "report-agent-session" } else { "report-agent" }),
         $env:HERDR_PANE_ID,
         "--source",
         "herdr:codex",
@@
         "--agent-session-id",
         "$sessionId"
     )
+    if ($Action -ne "session") { $args += @("--state", $Action) }
     if ($payload.hook_event_name -eq "SessionStart" -and $payload.source -is [string] -and -not [string]::IsNullOrWhiteSpace($payload.source)) {
         $args += @("--session-start-source", "$($payload.source)")
     }
```

`upstream/codex/herdr-agent-state.sh` (diff):

```diff
--- src/integration/assets/codex/herdr-agent-state.sh @ 4a3b04f5
+++ src/integration/assets/codex/herdr-agent-state.sh @ b0648067
@@
 # managed by herdr; reinstalling or updating the integration overwrites this file.
 # add custom hooks beside this file instead of editing it.
 # HERDR_INTEGRATION_ID=codex
-# HERDR_INTEGRATION_VERSION=8
+# HERDR_INTEGRATION_VERSION=9
 
 set -eu
 
@@
 cat >"$hook_input_file" 2>/dev/null || true
 
 case "$action" in
-  session) ;;
+  session|working|idle) ;;
   *) exit 0 ;;
 esac
 
@@
         hook_input = {}
 
 hook_event_name = str(hook_input.get("hook_event_name") or "")
-if hook_event_name and hook_event_name != "SessionStart":
+expected_events = {"session": ("SessionStart",), "working": ("UserPromptSubmit",), "idle": ("Stop", "Interrupt")}[action]
+if hook_event_name and hook_event_name not in expected_events:
     raise SystemExit(0)
 
 request_id = f"{source}:{int(time.time() * 1000)}:{random.randrange(1_000_000):06d}"
 report_seq = time.time_ns()
 session_id = hook_input.get("session_id")
 agent_session_id = session_id if isinstance(session_id, str) and session_id else None
-transcript_path = hook_input.get("transcript_path")
-if not isinstance(transcript_path, str) or not transcript_path.strip():
-    raise SystemExit(0)
+if action == "session":
+    transcript_path = hook_input.get("transcript_path")
+    if not isinstance(transcript_path, str) or not transcript_path.strip():
+        raise SystemExit(0)
 inherited_session_id = os.environ.get("CODEX_THREAD_ID")
 if inherited_session_id and inherited_session_id != agent_session_id:
     raise SystemExit(0)
-session_start_source = hook_input.get("source") if hook_event_name == "SessionStart" else None
+session_start_source = hook_input.get("source") if action == "session" else None
 if not isinstance(session_start_source, str) or not session_start_source:
     session_start_source = None
 if agent_session_id:
@@
         "seq": report_seq,
         "agent_session_id": agent_session_id,
     }
-    if session_start_source:
-        params["session_start_source"] = session_start_source
+    if action == "session":
+        if session_start_source:
+            params["session_start_source"] = session_start_source
+        method = "pane.report_agent_session"
+    else:
+        params["state"] = action
+        method = "pane.report_agent"
     request = {
         "id": request_id,
-        "method": "pane.report_agent_session",
+        "method": method,
         "params": params,
     }
 else:
```

#### `claude` — ported from herdr `claude` version 9

Compared against `4a3b04f5`; upstream is now at version 10.

`upstream/claude/herdr-agent-state.ps1` (diff):

```diff
--- src/integration/assets/claude/herdr-agent-state.ps1 @ 4a3b04f5
+++ src/integration/assets/claude/herdr-agent-state.ps1 @ b0648067
@@
 # managed by herdr; reinstalling or updating the integration overwrites this file.
 # add custom hooks beside this file instead of editing it.
 # HERDR_INTEGRATION_ID=claude
-# HERDR_INTEGRATION_VERSION=9
+# HERDR_INTEGRATION_VERSION=10
 
 param([string]$Action = "")
 
```

`upstream/claude/herdr-agent-state.sh` (diff):

```diff
--- src/integration/assets/claude/herdr-agent-state.sh @ 4a3b04f5
+++ src/integration/assets/claude/herdr-agent-state.sh @ b0648067
@@
 # managed by herdr; reinstalling or updating the integration overwrites this file.
 # add custom hooks beside this file instead of editing it.
 # HERDR_INTEGRATION_ID=claude
-# HERDR_INTEGRATION_VERSION=9
+# HERDR_INTEGRATION_VERSION=10
 
 set -eu
 
```

#### `pi` — ported from herdr `pi` version 8

Compared against `d08e4468`; upstream is now at version 9.

`upstream/pi/herdr-agent-state.ts` (diff):

```diff
--- src/integration/assets/pi/herdr-agent-state.ts @ d08e4468
+++ src/integration/assets/pi/herdr-agent-state.ts @ b0648067
@@
 // managed by herdr; reinstalling or updating the integration overwrites this file.
 // add custom hooks/plugins beside this file instead of editing it.
 // HERDR_INTEGRATION_ID=pi
-// HERDR_INTEGRATION_VERSION=8
+// HERDR_INTEGRATION_VERSION=9
 // @ts-nocheck
 
 import net from "node:net";
+import path from "node:path";
 
 const HERDR_ENV = process.env.HERDR_ENV;
 const socketPath = process.env.HERDR_SOCKET_PATH;
@@
   try {
     const file = ctx?.sessionManager?.getSessionFile?.();
     currentAgentSessionPath =
-      typeof file === "string" && file.startsWith("/") ? file : undefined;
+      typeof file === "string" &&
+      (path.posix.isAbsolute(file) || path.win32.isAbsolute(file))
+        ? file
+        : undefined;
   } catch {
     currentAgentSessionPath = undefined;
   }
```

#### `kilo` — ported from herdr `kilo` version 4

Compared against `d08e4468`; upstream is now at version 4.

No upstream change: all 1 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `kimi` — ported from herdr `kimi` version 7

Compared against `d08e4468`; upstream is now at version 7.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `omp` — ported from herdr `omp` version 9

Compared against `d08e4468`; upstream is now at version 10.

`upstream/omp/herdr-agent-state.ts` (diff):

```diff
--- src/integration/assets/omp/herdr-agent-state.ts @ d08e4468
+++ src/integration/assets/omp/herdr-agent-state.ts @ b0648067
@@
 // managed by herdr; reinstalling or updating the integration overwrites this file.
... truncated at 4 of 26 lines; run `git diff` between the two commits named above in a Herdr checkout for the rest.
```

#### `antigravity` — ported from herdr `agy` version 3

Compared against `d08e4468`; upstream is now at version 3.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `copilot` — ported from herdr `copilot` version 3

Compared against `d08e4468`; upstream is now at version 3.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `cursor` — ported from herdr `cursor` version 1

Compared against `d08e4468`; upstream is now at version 1.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `grok` — ported from herdr `grok` version 1

Compared against `d08e4468`; upstream is now at version 2.

`upstream/grok/herdr-agent-state.ps1` (diff) is not shown: this section is capped at 600 lines to keep the report inside GitHub's pull request body limit. Re-run `go run ./internal/tools/herdrsync` locally to read it.

`upstream/grok/herdr-agent-state.sh` (diff) is not shown: this section is capped at 600 lines to keep the report inside GitHub's pull request body limit. Re-run `go run ./internal/tools/herdrsync` locally to read it.

#### `devin` — ported from herdr `devin` version 2

Compared against `d08e4468`; upstream is now at version 2.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `droid` — ported from herdr `droid` version 3

Compared against `d08e4468`; upstream is now at version 3.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `qodercli` — ported from herdr `qodercli` version 3

Compared against `d08e4468`; upstream is now at version 3.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `qwen` — ported from herdr `qwen` version 1

Compared against `d08e4468`; upstream is now at version 1.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `mastracode` — ported from herdr `mastracode` version 2

Compared against `d08e4468`; upstream is now at version 2.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

#### `hermes` — ported from herdr `hermes` version 5

Compared against `d08e4468`; upstream is now at version 5.

No upstream change: all 2 compared file(s) are byte-identical to the copy this port was written against. Nothing to re-port.

## Fixture verdict flips

Every fixture in `internal/agentactivity/testdata` with a `screen:` block, classified against the manifests this sync replaced and against the ones it wrote. A verdict is the state, the matched rule id, and the fallback reason: the same triple `scripts/herdr-diff.sh` compares. The Sidecar overlays are applied to **both** sides, because a sync never touches them and applying them to one side would report every overlay rule as a flip. Sidecar's process gate is not applied: it reads the pane's process name and never the manifest, so its answer is the same on both sides and it cannot create or hide a flip.

**3 of 61 fixture(s) changed verdict.** Each row is a screen Sidecar now reads differently. Read the manifest diff above for the rule that moved, and decide per row whether the new verdict is the better one.

| Agent | Fixture | Before | After |
| --- | --- | --- | --- |
| `codex` | `completed.txt` | idle via `sidecar.composer_idle` | idle via `osc_title_idle` |
| `codex` | `interrupted.txt` | idle via `sidecar.composer_idle` | idle via `osc_title_idle` |
| `codex` | `startup_idle.txt` | idle via `sidecar.composer_idle` | idle via `osc_title_idle` |

## Overlay rules

Each rule is removed on its own from the manifests this sync wrote, and the corpus is reclassified. A rule that changes no verdict has stopped earning its place, which is the signal that upstream has adopted the same idea and the rule can go. Redundancy is judged on the state and the fallback reason alone, never on the rule id: a `sidecar.` id can never equal the upstream id that would win without it, so folding the id in would make the check unreachable.

A rule carrying an **upstream** id is not in that bucket and is never a deletion candidate. It replaces upstream's rule rather than adding one, so removing it leaves a rule that is dead (the `\p{Alphabetic}` rewrites RE2 cannot compile) or differently flagged (the copies that only add `visible_blocker`), which is a regression rather than a cleanup. Those rows are judged on the matched rule id and the visible flags as well, and say when no fixture covers them at all.

| Overlay | Rule | Kind | Effect |
| --- | --- | --- | --- |
| `antigravity` | `spinner_working` | replaces upstream | no fixture covers it; upstream's own rule stands without it |
| `antigravity` | `sidecar.trust_prompt_blocked` | addition | changes 1 fixture(s): `blocked.txt` |
| `antigravity` | `sidecar.permission_prompt_blocked` | addition | changes 1 fixture(s): `permission_prompt.txt` |
| `antigravity` | `sidecar.status_footer_working` | addition | changes 1 fixture(s): `working.txt` |
| `claude` | `sidecar.overlay_retain` | addition | changes 1 fixture(s): `overlay.txt` |
| `claude` | `sidecar.allow_prompt_blocker` | addition | changes 1 fixture(s): `allow_prompt.txt` |
| `claude` | `sidecar.background_agents_waiting` | addition | **changes nothing: deletion candidate**; without it working via `sidecar.background_agents_footer_working` |
| `claude` | `sidecar.background_agents_footer_working` | addition | changes 1 fixture(s): `background_agents_footer.txt` |
| `claude` | `legacy_no_prompt_blocker` | replaces upstream | changes 1 fixture(s): `legacy_permission_wait.txt` |
| `codex` | `sidecar.composer_idle` | addition | **no fixture matches this rule**; nothing here proves what it is for |
| `codex` | `sidecar.approval_blocker` | addition | changes 1 fixture(s): `approval_prompt.txt` |
| `codex` | `weak_blocker` | replaces upstream | changes 1 fixture(s): `weak_blocker.txt` |
| `cursor` | `spinner_working` | replaces upstream | changes 1 fixture(s): `working_spinner.txt` |
| `cursor` | `sidecar.decision_blocked` | addition | changes 1 fixture(s): `blocked_decision.txt` |
| `cursor` | `sidecar.finished_background_tasks_idle` | addition | changes 1 fixture(s): `false_positive_finished_background.txt` |
| `cursor` | `sidecar.background_suffix_working` | addition | changes 1 fixture(s): `working_background.txt` |
| `grok` | `sidecar.overlay_retain` | addition | changes 1 fixture(s): `overlay.txt` |
| `grok` | `sidecar.working_footer` | addition | changes nothing, and the overlay declares it `harness-exempt`: no fixture holds the screen it is for, and its case is a Go test |
| `grok` | `sidecar.idle_footer` | addition | changes 1 fixture(s): `stale_working_scrollback.txt` |
| `muse` | `sidecar.thinking_working` | addition | changes 1 fixture(s): `thinking.txt` |
| `qodercli` | `spinner_working` | replaces upstream | changes 1 fixture(s): `spinner_working.txt` |

2 overlay rule(s) changed no fixture verdict. Delete the rule, or record why it stays and add the fixture that proves it. Deleting one is a separate change with a fixture attached, so this report flags it rather than making it.

