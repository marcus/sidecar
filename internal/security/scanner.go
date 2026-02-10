package security

import (
	"regexp"
	"strings"
)

// Scanner detects PII exposure in text content
type Scanner struct {
	sensitivity SensitivityLevel
	enabled     bool
}

// NewScanner creates a new PII scanner with the given sensitivity level
func NewScanner(sensitivity SensitivityLevel, enabled bool) *Scanner {
	return &Scanner{
		sensitivity: sensitivity,
		enabled:     enabled,
	}
}

// Scan scans text for PII patterns and returns all matches
func (s *Scanner) Scan(text string) []PIIMatch {
	if !s.enabled {
		return nil
	}

	var matches []PIIMatch
	lines := strings.Split(text, "\n")
	enabledPatterns := PatternConfig[s.sensitivity]

	for lineNum, line := range lines {
		// Scan for each enabled pattern
		if enabledPatterns[PIITypeEmail] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypeEmail, EmailPattern)...)
		}
		if enabledPatterns[PIITypePhone] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypePhone, PhonePattern)...)
		}
		if enabledPatterns[PIITypeSSN] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypeSSN, SSNPattern)...)
		}
		if enabledPatterns[PIITypeAPIKey] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypeAPIKey, APIKeyPattern)...)
		}
		if enabledPatterns[PIITypeAWSKey] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypeAWSKey, AWSKeyPattern)...)
		}
		if enabledPatterns[PIITypePrivateKey] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypePrivateKey, PrivateKeyPattern)...)
		}
		if enabledPatterns[PIITypeCreditCard] {
			matches = append(matches, s.scanPatternWithValidation(line, lineNum, PIITypeCreditCard, CreditCardPattern)...)
		}
		if enabledPatterns[PIITypeToken] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypeToken, TokenPattern)...)
		}
		if enabledPatterns[PIITypePassword] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypePassword, PasswordPattern)...)
		}
		if enabledPatterns[PIITypeDatabaseURL] {
			matches = append(matches, s.scanPattern(line, lineNum, PIITypeDatabaseURL, DatabaseURLPattern)...)
		}
	}

	return matches
}

// ScanMessage scans a message for PII and returns matches
func (s *Scanner) ScanMessage(content string) []PIIMatch {
	return s.Scan(content)
}

// HasSensitivePII returns true if any sensitive PII is detected in the text
func (s *Scanner) HasSensitivePII(text string) bool {
	matches := s.Scan(text)
	for _, m := range matches {
		if SensitiveTypes[m.Type] {
			return true
		}
	}
	return false
}

// GetSensitiveMatches returns only sensitive PII matches
func (s *Scanner) GetSensitiveMatches(text string) []PIIMatch {
	matches := s.Scan(text)
	var sensitive []PIIMatch
	for _, m := range matches {
		if SensitiveTypes[m.Type] {
			m.Sensitive = true
			sensitive = append(sensitive, m)
		}
	}
	return sensitive
}

// scanPattern scans a line using a regex pattern
func (s *Scanner) scanPattern(line string, lineNum int, piiType PIIType, pattern *regexp.Regexp) []PIIMatch {
	var matches []PIIMatch
	allMatches := pattern.FindAllStringIndex(line, -1)
	for _, match := range allMatches {
		value := line[match[0]:match[1]]
		matches = append(matches, PIIMatch{
			Type:      piiType,
			Value:     value,
			StartIdx:  match[0],
			EndIdx:    match[1],
			Line:      lineNum,
			Sensitive: SensitiveTypes[piiType],
		})
	}
	return matches
}

// scanPatternWithValidation scans with additional validation (e.g., Luhn check for credit cards)
func (s *Scanner) scanPatternWithValidation(line string, lineNum int, piiType PIIType, pattern *regexp.Regexp) []PIIMatch {
	var matches []PIIMatch
	allMatches := pattern.FindAllStringIndex(line, -1)
	for _, match := range allMatches {
		value := line[match[0]:match[1]]

		// Validate credit card using Luhn algorithm
		if piiType == PIITypeCreditCard && !s.validateLuhn(value) {
			continue
		}

		matches = append(matches, PIIMatch{
			Type:      piiType,
			Value:     value,
			StartIdx:  match[0],
			EndIdx:    match[1],
			Line:      lineNum,
			Sensitive: SensitiveTypes[piiType],
		})
	}
	return matches
}

// validateLuhn performs Luhn algorithm validation on a credit card number
func (s *Scanner) validateLuhn(cardNumber string) bool {
	// Remove spaces and dashes
	cleaned := strings.ReplaceAll(cardNumber, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")

	// Must be 13-19 digits
	if len(cleaned) < 13 || len(cleaned) > 19 {
		return false
	}

	// Check if all characters are digits
	for _, ch := range cleaned {
		if ch < '0' || ch > '9' {
			return false
		}
	}

	// Perform Luhn check
	sum := 0
	for i := len(cleaned) - 1; i >= 0; i-- {
		digit := int(cleaned[i] - '0')
		if (len(cleaned)-i)%2 == 0 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}

	return sum%10 == 0
}

// MaskPII returns text with sensitive PII values masked
func (s *Scanner) MaskPII(text string) string {
	matches := s.GetSensitiveMatches(text)
	if len(matches) == 0 {
		return text
	}

	// Sort matches in reverse order to avoid index shifting when replacing
	result := text
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		// Create mask based on PII type
		mask := s.createMask(m.Type, m.Value)
		result = result[:m.StartIdx] + mask + result[m.EndIdx:]
	}
	return result
}

// createMask creates an appropriate mask for a PII type
func (s *Scanner) createMask(piiType PIIType, value string) string {
	switch piiType {
	case PIITypeEmail:
		return "[EMAIL]"
	case PIITypePhone:
		return "[PHONE]"
	case PIITypeSSN:
		return "[SSN]"
	case PIITypeAPIKey:
		return "[API_KEY]"
	case PIITypeAWSKey:
		return "[AWS_KEY]"
	case PIITypePrivateKey:
		return "[PRIVATE_KEY]"
	case PIITypeCreditCard:
		// Show last 4 digits
		digits := extractDigits(value)
		if len(digits) >= 4 {
			return "[CC:**" + digits[len(digits)-4:] + "]"
		}
		return "[CREDIT_CARD]"
	case PIITypeToken:
		return "[TOKEN]"
	case PIITypePassword:
		return "[PASSWORD]"
	case PIITypeDatabaseURL:
		return "[DATABASE_URL]"
	default:
		return "[REDACTED]"
	}
}

// extractDigits extracts only digit characters from a string
func extractDigits(s string) string {
	var result strings.Builder
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			result.WriteRune(ch)
		}
	}
	return result.String()
}

// SetSensitivity updates the scanner's sensitivity level
func (s *Scanner) SetSensitivity(level SensitivityLevel) {
	s.sensitivity = level
}

// SetEnabled enables or disables scanning
func (s *Scanner) SetEnabled(enabled bool) {
	s.enabled = enabled
}

// IsEnabled returns whether scanning is enabled
func (s *Scanner) IsEnabled() bool {
	return s.enabled
}
