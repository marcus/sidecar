// Reports the asset's export surface.
//
// Kilo's plugin loader walks a module's namespace and calls every export that is
// a function, skipping the ones it cannot call: measured on 7.5.9 with a probe
// plugin that exported a string beside a factory, where the factory still ran.
// OpenCode's loader is stricter -- one non-function export and the whole module
// is imported and then never called, silently, with no error anywhere -- and
// Sidecar's Kilo asset holds itself to that stricter rule anyway.
//
// The cost of doing so is zero, the three JavaScript assets Sidecar ships then
// have one export convention between them rather than three, and relying on a
// fork staying laxer than its upstream is a bet on a difference nobody promised
// to keep. This harness is what makes that a checked property rather than an
// intention.
import * as namespace from "./sidecar-lifecycle.js"

const names = Object.keys(namespace)
const nonFunctions = names.filter((k) => typeof namespace[k] !== "function")
process.stdout.write(
  JSON.stringify({
    names,
    nonFunctions,
    factory: typeof namespace.SidecarLifecycle === "function",
  }),
)
