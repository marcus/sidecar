package app

import (
	"github.com/marcus/sidecar/internal/notify"
	"github.com/marcus/sidecar/internal/terminallink"
)

// A notification's calls to action — the numbered targets `enter` and the
// digit keys act on — are reconciled by notify.CallsToAction, which is
// state-free. The shell adds the two things it alone knows: the project root a
// file token is verified against, and a memo so that verification is not I/O on
// every frame.

// notificationMaxTargetDigits is how many targets a keyboard can reach. Nine,
// because the jump keys are 1–9; a tenth target is still activatable through
// the first-target `enter` if it happens to be first, and nothing pretends
// otherwise in the rendered list.
const notificationMaxTargetDigits = 9

// notificationCallsToAction answers the numbered target list for one
// notification, memoized by id.
//
// The memo is what makes existence-verified underlines affordable: a file
// token is stat'ed once per notification rather than once per render. A record
// is immutable in everything this depends on (title, body, targets), so the id
// is a sufficient key; refreshNotifications prunes ids that have left the
// store.
func (m Model) notificationCallsToAction(n notify.Notification) []notify.CallToAction {
	// The memo is only valid for the checkout it was verified against. A
	// project or worktree switch keeps the same (global) notifications but
	// changes what "internal/app/model.go" means and whether it exists, so a
	// memo from the previous root is answered fresh and not written back —
	// pruneNotificationCTAs drops it on the next refresh.
	if m.notificationCTAs == nil || m.notificationCTARoot != m.ui.WorkDir {
		return notify.CallsToAction(n, m.notificationScanOptions())
	}
	if list, ok := m.notificationCTAs[n.ID]; ok {
		return list
	}
	list := notify.CallsToAction(n, m.notificationScanOptions())
	m.notificationCTAs[n.ID] = list
	return list
}

// notificationScanOptions is the project-scoped verification the centre can
// afford. Files are resolved against the current checkout, so a path that does
// not exist here is never underlined and never numbered — the plan's
// verified-underline invariant for same-project targets. No diff resolver and
// no resource matchers: both need a live snapshot the centre does not hold, and
// a git existence check per notification is not worth a lit-up commit id.
func (m Model) notificationScanOptions() terminallink.Options {
	root := m.ui.WorkDir
	if root == "" {
		return terminallink.Options{}
	}
	return terminallink.Options{
		Resolve: func(raw string) (string, terminallink.Extra, bool) {
			display, _, ok := terminallink.ResolveFile(root, raw)
			if !ok {
				return "", terminallink.Extra{}, false
			}
			return display, terminallink.Extra{Raw: raw}, true
		},
	}
}

// pruneNotificationCTAs drops memo entries for notifications the store no
// longer returns, and is where the map is created. Called from
// refreshNotifications, the one seam the cache is written at.
func (m *Model) pruneNotificationCTAs() {
	if m.notificationCTAs == nil || m.notificationCTARoot != m.ui.WorkDir {
		// A new checkout re-verifies everything: the store is global, so the
		// same record can be looked at from two projects in one session.
		m.notificationCTAs = make(map[string][]notify.CallToAction)
		m.notificationCTARoot = m.ui.WorkDir
		return
	}
	live := make(map[string]bool, len(m.notificationCache))
	for _, n := range m.notificationCache {
		live[n.ID] = true
	}
	for id := range m.notificationCTAs {
		if !live[id] {
			delete(m.notificationCTAs, id)
		}
	}
}

// selectedNotificationTargets is the numbered list the keys act on: the calls
// to action of the entry under the cursor.
func (m Model) selectedNotificationTargets() []notify.CallToAction {
	selected, ok := m.selectedNotification(m.notificationCentreItems())
	if !ok {
		return nil
	}
	return m.notificationCallsToAction(selected)
}
