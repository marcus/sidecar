package tty

import "testing"

// csiBytes is a local []byte-based type that mimics BubbleTea's
// unexported unknownCSISequenceMsg.
type csiBytes []byte

func TestExtractUnknownCSIBytes_CSIu(t *testing.T) {
	msg := csiBytes("\x1b[13;2u")
	got := ExtractUnknownCSIBytes(msg)
	if got == nil {
		t.Fatal("expected non-nil")
	}
}

func TestExtractUnknownCSIBytes_ModifyOtherKeys(t *testing.T) {
	msg := csiBytes("\x1b[27;2;13~")
	got := ExtractUnknownCSIBytes(msg)
	if got == nil {
		t.Fatal("expected non-nil")
	}
}

func TestExtractUnknownCSIBytes_TooShort(t *testing.T) {
	if ExtractUnknownCSIBytes(csiBytes("\x1b[u")) != nil {
		t.Error("expected nil for too-short sequence")
	}
}

func TestExtractUnknownCSIBytes_NotByteSlice(t *testing.T) {
	if ExtractUnknownCSIBytes("string") != nil {
		t.Error("expected nil for string")
	}
	if ExtractUnknownCSIBytes(42) != nil {
		t.Error("expected nil for int")
	}
}

func TestNormalizeToCSIu_CSIu_Passthrough(t *testing.T) {
	// Already CSI u — pass through unchanged
	got := NormalizeToCSIu([]byte("\x1b[13;2u"))
	if got != "\x1b[13;2u" {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestNormalizeToCSIu_ModifyOtherKeys_ShiftEnter(t *testing.T) {
	// modifyOtherKeys: ESC[27;2;13~ → CSI u: ESC[13;2u
	got := NormalizeToCSIu([]byte("\x1b[27;2;13~"))
	if got != "\x1b[13;2u" {
		t.Errorf("expected \\x1b[13;2u, got %q", got)
	}
}

func TestNormalizeToCSIu_ModifyOtherKeys_CtrlShiftEnter(t *testing.T) {
	// modifier 6 = ctrl+shift
	got := NormalizeToCSIu([]byte("\x1b[27;6;13~"))
	if got != "\x1b[13;6u" {
		t.Errorf("expected \\x1b[13;6u, got %q", got)
	}
}

func TestNormalizeToCSIu_UnknownFinal(t *testing.T) {
	// Not CSI u and not modifyOtherKeys — return empty
	got := NormalizeToCSIu([]byte("\x1b[1;2A"))
	if got != "" {
		t.Errorf("expected empty for known CSI sequence, got %q", got)
	}
}

func TestNormalizeToCSIu_TooShort(t *testing.T) {
	if NormalizeToCSIu([]byte("\x1b[u")) != "" {
		t.Error("expected empty for too-short")
	}
}

func TestNormalizeToCSIu_NotModifyOtherKeys(t *testing.T) {
	// Tilde-terminated but not 27;... prefix
	got := NormalizeToCSIu([]byte("\x1b[5;2~"))
	if got != "" {
		t.Errorf("expected empty for non-modifyOtherKeys tilde sequence, got %q", got)
	}
}
