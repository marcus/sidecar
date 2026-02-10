package security

import (
	"testing"
)

func TestEmailDetection(t *testing.T) {
	scanner := NewScanner(SensitivityMedium, true)

	tests := []struct {
		name      string
		text      string
		expectPII bool
	}{
		{
			name:      "valid email",
			text:      "Contact me at john.doe@example.com",
			expectPII: true,
		},
		{
			name:      "multiple emails",
			text:      "john@example.com or jane@test.org",
			expectPII: true,
		},
		{
			name:      "no email",
			text:      "Just plain text without contact info",
			expectPII: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := scanner.Scan(tt.text)
			hasPII := len(matches) > 0
			if hasPII != tt.expectPII {
				t.Errorf("Expected PII=%v, got %v. Matches: %v", tt.expectPII, hasPII, matches)
			}
		})
	}
}

func TestPhoneDetection(t *testing.T) {
	scanner := NewScanner(SensitivityMedium, true)

	tests := []struct {
		name      string
		text      string
		expectPII bool
	}{
		{
			name:      "US phone with dashes",
			text:      "Call me at 555-123-4567",
			expectPII: true,
		},
		{
			name:      "US phone with parens",
			text:      "My number is (555) 123-4567",
			expectPII: true,
		},
		{
			name:      "US phone with dots",
			text:      "555.123.4567",
			expectPII: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := scanner.Scan(tt.text)
			hasPII := len(matches) > 0
			if hasPII != tt.expectPII {
				t.Errorf("Expected PII=%v, got %v. Matches: %v", tt.expectPII, hasPII, matches)
			}
		})
	}
}

func TestSSNDetection(t *testing.T) {
	scanner := NewScanner(SensitivityMedium, true)

	tests := []struct {
		name      string
		text      string
		expectPII bool
	}{
		{
			name:      "SSN with dashes",
			text:      "My SSN is 123-45-6789",
			expectPII: true,
		},
		{
			name:      "SSN without dashes",
			text:      "SSN: 123456789",
			expectPII: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := scanner.Scan(tt.text)
			hasPII := len(matches) > 0
			if hasPII != tt.expectPII {
				t.Errorf("Expected PII=%v, got %v", tt.expectPII, hasPII)
			}
		})
	}
}

func TestCreditCardDetection(t *testing.T) {
	scanner := NewScanner(SensitivityMedium, true)

	tests := []struct {
		name      string
		text      string
		expectPII bool
	}{
		{
			name:      "valid credit card",
			text:      "Card: 4111111111111111",
			expectPII: true,
		},
		{
			name:      "credit card without dashes",
			text:      "4111111111111111",
			expectPII: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := scanner.Scan(tt.text)
			hasPII := len(matches) > 0
			if hasPII != tt.expectPII {
				t.Errorf("Expected PII=%v, got %v", tt.expectPII, hasPII)
			}
		})
	}
}

func TestAPIKeyDetection(t *testing.T) {
	scanner := NewScanner(SensitivityHigh, true)

	tests := []struct {
		name      string
		text      string
		expectPII bool
	}{
		{
			name:      "API key with colon",
			text:      `api_key: "sk-1234567890abcdefghij"`,
			expectPII: true,
		},
		{
			name:      "AWS key",
			text:      "Key: AKIAIOSFODNN7EXAMPLE",
			expectPII: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := scanner.Scan(tt.text)
			hasPII := len(matches) > 0
			if hasPII != tt.expectPII {
				t.Errorf("Expected PII=%v, got %v. Matches: %v", tt.expectPII, hasPII, matches)
			}
		})
	}
}

func TestSensitivityLevels(t *testing.T) {
	tests := []struct {
		name          string
		sensitivity   SensitivityLevel
		text          string
		expectedCount int
	}{
		{
			name:          "low sensitivity with email",
			sensitivity:   SensitivityLow,
			text:          "Contact: john@example.com",
			expectedCount: 1,
		},
		{
			name:          "medium sensitivity with multiple PII",
			sensitivity:   SensitivityMedium,
			text:          "Email: john@example.com, Phone: 555-123-4567, SSN: 123-45-6789",
			expectedCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := NewScanner(tt.sensitivity, true)
			matches := scanner.Scan(tt.text)
			if len(matches) != tt.expectedCount {
				t.Errorf("Expected %d matches, got %d. Matches: %v", tt.expectedCount, len(matches), matches)
			}
		})
	}
}

func TestDisabledScanner(t *testing.T) {
	scanner := NewScanner(SensitivityHigh, false)

	text := "Email: john@example.com, SSN: 123-45-6789"
	matches := scanner.Scan(text)

	if len(matches) > 0 {
		t.Errorf("Expected no matches when disabled, got %d", len(matches))
	}
}

func TestHasSensitivePII(t *testing.T) {
	scanner := NewScanner(SensitivityHigh, true)

	tests := []struct {
		name       string
		text       string
		expectSens bool
	}{
		{
			name:       "email is not sensitive",
			text:       "john@example.com",
			expectSens: false,
		},
		{
			name:       "SSN is sensitive",
			text:       "SSN: 123-45-6789",
			expectSens: true,
		},
		{
			name:       "API key is sensitive",
			text:       `api_key: "sk-1234567890abcdefghij"`,
			expectSens: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasSens := scanner.HasSensitivePII(tt.text)
			if hasSens != tt.expectSens {
				t.Errorf("Expected sensitive=%v, got %v", tt.expectSens, hasSens)
			}
		})
	}
}

func TestMaskPII(t *testing.T) {
	scanner := NewScanner(SensitivityHigh, true)

	tests := []struct {
		name     string
		text     string
		contains string
		notContains string
	}{
		{
			name:        "mask SSN",
			text:        "My SSN is 123-45-6789",
			contains:    "[SSN]",
			notContains: "123-45-6789",
		},
		{
			name:        "mask API key",
			text:        `api_key: "sk-1234567890abcdefghij"`,
			contains:    "[API_KEY]",
			notContains: "sk-1234567890abcdefghij",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masked := scanner.MaskPII(tt.text)
			if tt.contains != "" && !contains(masked, tt.contains) {
				t.Errorf("Expected mask to contain '%s', got: %s", tt.contains, masked)
			}
			if tt.notContains != "" && contains(masked, tt.notContains) {
				t.Errorf("Expected mask to NOT contain '%s', got: %s", tt.notContains, masked)
			}
		})
	}
}

func TestMultilineScanning(t *testing.T) {
	scanner := NewScanner(SensitivityMedium, true)

	text := `User: john@example.com
Phone: 555-123-4567
SSN: 123-45-6789`

	matches := scanner.Scan(text)

	if len(matches) < 3 {
		t.Errorf("Expected at least 3 matches, got %d", len(matches))
	}

	// Check line numbers are set correctly
	for _, m := range matches {
		if m.Line < 0 || m.Line > 2 {
			t.Errorf("Invalid line number: %d", m.Line)
		}
	}
}

func TestSetSensitivity(t *testing.T) {
	scanner := NewScanner(SensitivityLow, true)

	text := "SSN: 123-45-6789"
	matches := scanner.Scan(text)

	if len(matches) > 0 {
		t.Errorf("Expected no SSN detection at low sensitivity, got %d matches", len(matches))
	}

	// Change to high sensitivity
	scanner.SetSensitivity(SensitivityHigh)
	matches = scanner.Scan(text)

	if len(matches) == 0 {
		t.Errorf("Expected SSN detection at high sensitivity, got no matches")
	}
}

func TestSetEnabled(t *testing.T) {
	scanner := NewScanner(SensitivityHigh, true)

	text := "Email: john@example.com"
	matches := scanner.Scan(text)

	if len(matches) == 0 {
		t.Errorf("Expected matches when enabled")
	}

	scanner.SetEnabled(false)
	matches = scanner.Scan(text)

	if len(matches) > 0 {
		t.Errorf("Expected no matches when disabled")
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
