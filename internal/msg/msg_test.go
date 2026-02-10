package msg

import (
	"testing"
	"time"
)

func TestToastMsg_Creation(t *testing.T) {
	tests := []struct {
		name    string
		message string
		duration time.Duration
		isError  bool
	}{
		{
			name:    "success toast",
			message: "File saved successfully",
			duration: 2 * time.Second,
			isError:  false,
		},
		{
			name:    "error toast",
			message: "Failed to save file",
			duration: 3 * time.Second,
			isError:  true,
		},
		{
			name:    "empty message",
			message: "",
			duration: 1 * time.Second,
			isError:  false,
		},
		{
			name:    "long message",
			message: "This is a very long message that should still be handled correctly by the toast system",
			duration: 5 * time.Second,
			isError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toast := ToastMsg{
				Message:  tt.message,
				Duration: tt.duration,
				IsError:  tt.isError,
			}

			if toast.Message != tt.message {
				t.Errorf("Message mismatch: got %q, want %q", toast.Message, tt.message)
			}
			if toast.Duration != tt.duration {
				t.Errorf("Duration mismatch: got %v, want %v", toast.Duration, tt.duration)
			}
			if toast.IsError != tt.isError {
				t.Errorf("IsError mismatch: got %v, want %v", toast.IsError, tt.isError)
			}
		})
	}
}

func TestShowToast(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		duration time.Duration
	}{
		{
			name:     "normal toast",
			message:  "Test message",
			duration: 2 * time.Second,
		},
		{
			name:     "long duration",
			message:  "Important message",
			duration: 10 * time.Second,
		},
		{
			name:     "short duration",
			message:  "Quick message",
			duration: 500 * time.Millisecond,
		},
		{
			name:     "zero duration",
			message:  "No timeout",
			duration: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := ShowToast(tt.message, tt.duration)

			if cmd == nil {
				t.Fatal("ShowToast should return a non-nil command")
			}

			// Execute the command to get the message
			msg := cmd()

			toast, ok := msg.(ToastMsg)
			if !ok {
				t.Fatalf("command should return ToastMsg, got %T", msg)
			}

			if toast.Message != tt.message {
				t.Errorf("Message mismatch: got %q, want %q", toast.Message, tt.message)
			}
			if toast.Duration != tt.duration {
				t.Errorf("Duration mismatch: got %v, want %v", toast.Duration, tt.duration)
			}
			if toast.IsError {
				t.Errorf("IsError should be false for ShowToast (success toast), got true")
			}
		})
	}
}

func TestShowToast_MultipleInvocations(t *testing.T) {
	// Ensure ShowToast can be called multiple times independently
	cmd1 := ShowToast("Message 1", 1*time.Second)
	cmd2 := ShowToast("Message 2", 2*time.Second)

	msg1 := cmd1()
	msg2 := cmd2()

	toast1, ok1 := msg1.(ToastMsg)
	toast2, ok2 := msg2.(ToastMsg)

	if !ok1 || !ok2 {
		t.Fatal("both commands should return ToastMsg")
	}

	if toast1.Message == toast2.Message {
		t.Errorf("Messages should be different")
	}
	if toast1.Duration == toast2.Duration {
		t.Errorf("Durations should be different")
	}
}

func TestShowToast_ErrorFlag(t *testing.T) {
	// ShowToast creates success toasts (IsError=false)
	cmd := ShowToast("Error message", 3*time.Second)
	msg := cmd()
	toast := msg.(ToastMsg)

	if toast.IsError {
		t.Errorf("ShowToast should create success toast (IsError=false), got true")
	}
}
