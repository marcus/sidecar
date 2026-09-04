// Reports the asset's export surface.
//
// OMP's extension loader takes a module's default export and drops the module
// entirely when it is not a function, with no error logged anywhere. The
// extension then installs cleanly, loads, and reports nothing at all, which is
// invisible to every offline test that does not run the module.
//
// OpenCode's loader is stricter still: EVERY export must be a plugin factory
// there. Sidecar's OMP asset holds itself to the stricter rule as well, because
// the cost is zero and every Sidecar asset then has one export convention
// between them rather than four.
import mod, * as namespace from "./sidecar-omp-lifecycle.js"

const names = Object.keys(namespace)
const nonFunctions = names.filter((k) => typeof namespace[k] !== "function")
process.stdout.write(
  JSON.stringify({
    names,
    nonFunctions,
    defaultIsFunction: typeof mod === "function",
    defaultName: typeof mod === "function" ? mod.name : "",
  }),
)
