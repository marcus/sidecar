package conversations

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marcus/sidecar/internal/app"
	"github.com/marcus/sidecar/internal/security"
)

// ScanSessionForPII scans the currently loaded session for PII
func (p *Plugin) ScanSessionForPII() tea.Cmd {
	if p.piiScanner == nil || !p.piiScanner.IsEnabled() {
		return nil
	}

	if p.selectedSession == "" || len(p.messages) == 0 {
		return func() tea.Msg {
			return app.ToastMsg{
				Message:  "No session loaded to scan",
				Duration: 2 * time.Second,
				IsError:  false,
			}
		}
	}

	return func() tea.Msg {
		var allMatches []security.PIIMatch

		// Scan all messages in the session with message IDs
		for _, msg := range p.messages {
			matches := p.piiScanner.ScanMessageWithID(msg.Content, msg.ID)
			allMatches = append(allMatches, matches...)
		}

		// Cache the results
		p.sessionPIIMatches[p.selectedSession] = allMatches

		// Count sensitive matches
		sensitiveCount := 0
		for _, m := range allMatches {
			if security.SensitiveTypes[m.Type] {
				sensitiveCount++
			}
		}

		if len(allMatches) == 0 {
			return app.ToastMsg{
				Message:  "No PII detected in session",
				Duration: 2 * time.Second,
				IsError:  false,
			}
		}

		msg := fmt.Sprintf("Found %d PII matches", len(allMatches))
		if sensitiveCount > 0 {
			msg = fmt.Sprintf("%s (%d sensitive)", msg, sensitiveCount)
		}

		return app.ToastMsg{
			Message:  msg,
			Duration: 3 * time.Second,
			IsError:  sensitiveCount > 0,
		}
	}
}

// HasPIIInCurrentSession returns whether the current session has sensitive PII
func (p *Plugin) HasPIIInCurrentSession() bool {
	if p.piiScanner == nil || !p.piiScanner.IsEnabled() {
		return false
	}

	matches, ok := p.sessionPIIMatches[p.selectedSession]
	if !ok {
		return false
	}

	for _, m := range matches {
		if security.SensitiveTypes[m.Type] {
			return true
		}
	}
	return false
}

// GetPIIWarningForMessage returns a warning indicator if a message contains sensitive PII
func (p *Plugin) GetPIIWarningForMessage(msgID string) string {
	if p.piiScanner == nil || !p.piiScanner.IsEnabled() || !p.piiShowWarnings {
		return ""
	}

	matches, ok := p.sessionPIIMatches[p.selectedSession]
	if !ok {
		return ""
	}

	// Find PII in this specific message
	hasSensitivePII := false
	for _, m := range matches {
		if m.MessageID == msgID && security.SensitiveTypes[m.Type] {
			hasSensitivePII = true
			break
		}
	}

	if hasSensitivePII {
		return security.PIIIndicator(true)
	}
	return ""
}
