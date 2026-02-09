package kiro

import (
	"github.com/marcus/sidecar/internal/adapter"
)

// SearchMessages searches message content within a session.
// Implements adapter.MessageSearcher interface.
func (a *Adapter) SearchMessages(sessionID, query string, opts adapter.SearchOptions) ([]adapter.MessageMatch, error) {
	messages, err := a.Messages(sessionID)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}

	return adapter.SearchMessagesSlice(messages, query, opts)
}
