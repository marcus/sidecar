package app

import "time"

// UIState holds header/footer state information.
type UIState struct {
	Clock        time.Time
	LastRefresh  time.Time
	ToastMessage string
	ToastExpiry  time.Time
	WorkDir      string
}

// NewUIState creates a new UI state.
func NewUIState() *UIState {
	now := time.Now()
	return &UIState{
		Clock:       now,
		LastRefresh: now,
	}
}

// UpdateClock updates the current clock time.
func (u *UIState) UpdateClock() {
	u.Clock = time.Now()
}

// SetToast sets a toast message with expiry.
func (u *UIState) SetToast(msg string, duration time.Duration) {
	u.ToastMessage = msg
	u.ToastExpiry = time.Now().Add(duration)
}

// ClearExpiredToast clears toast if it has expired.
func (u *UIState) ClearExpiredToast() {
	if u.ToastMessage != "" && time.Now().After(u.ToastExpiry) {
		u.ToastMessage = ""
	}
}

// HasToast returns true if there's an active toast message.
func (u *UIState) HasToast() bool {
	return u.ToastMessage != "" && time.Now().Before(u.ToastExpiry)
}

// MarkRefresh updates the last refresh timestamp.
func (u *UIState) MarkRefresh() {
	u.LastRefresh = time.Now()
}
