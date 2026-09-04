package agentcatalog

import (
	"os/exec"
	"sync/atomic"
)

// installed is the answer to "which of these commands is on PATH", computed
// once per process and never on a render path.
//
// nil means nobody has asked yet, and that is a distinct answer from "none of
// them": an unprimed catalog offers every family rather than an empty picker,
// so a surface that renders before PrimeInstalled has run shows too much rather
// than too little. Hiding an agent the user has is the expensive mistake here;
// offering one they do not have costs them a "command not found".
var installed atomic.Pointer[map[string]bool]

// PrimeInstalled resolves every family's command on PATH and caches the result.
//
// It is I/O -- one PATH walk per family, which on a machine running an endpoint
// security agent is a measurable number of file opens -- so it belongs on a
// startup seam or in a tea.Cmd, never in View or Init. Calling it again
// recomputes, which is what a catalog reloaded from an overlay needs.
func PrimeInstalled() {
	primeInstalled(exec.LookPath)
}

func primeInstalled(lookPath func(string) (string, error)) {
	c := current()
	seen := make(map[string]bool, len(c.launch)+len(c.detection)+len(c.legacy))
	probe := func(family Family) {
		if family.Command == "" {
			return
		}
		if _, done := seen[family.Command]; done {
			return
		}
		_, err := lookPath(family.Command)
		seen[family.Command] = err == nil
	}
	for _, family := range c.launch {
		probe(family)
	}
	for _, family := range c.legacy {
		probe(family)
	}
	installed.Store(&seen)
}

// InstalledKnown reports whether the PATH probe has run in this process.
//
// A caller that filters by installation must ask this first, because "not
// installed" and "not asked" are the same zero value and must not be the same
// decision.
func InstalledKnown() bool {
	return installed.Load() != nil
}

// Installed reports whether a family's command resolves on PATH. It is false
// for a family with no command and for every family before PrimeInstalled runs;
// use InstalledKnown to tell those apart from a genuine absence.
func Installed(id string) bool {
	probed := installed.Load()
	if probed == nil {
		return false
	}
	family, ok := FindLaunch(id)
	if !ok || family.Command == "" {
		return false
	}
	return (*probed)[family.Command]
}

// resetInstalledForTest forgets the PATH probe.
func resetInstalledForTest() {
	installed.Store(nil)
}
