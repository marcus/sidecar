package security

import "regexp"

// PII pattern definitions with compiled regexes
var (
	// EmailPattern matches email addresses
	EmailPattern = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`)

	// PhonePattern matches phone numbers (US format and variations)
	PhonePattern = regexp.MustCompile(`(?:\+?1[-.\s]?)?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}\b`)

	// SSNPattern matches Social Security Numbers (XXX-XX-XXXX or XXXXXXXXX)
	SSNPattern = regexp.MustCompile(`\b(?:\d{3}-\d{2}-\d{4}|\d{9})\b`)

	// APIKeyPattern matches common API key formats
	APIKeyPattern = regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|secret[_-]?key|access[_-]?token|auth[_-]?token)['\"]?\s*[:=]\s*['\"]?([A-Za-z0-9\-_\.]{20,})['\"]?`)

	// AWSKeyPattern matches AWS access keys (AKIA...)
	AWSKeyPattern = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)

	// PrivateKeyPattern matches private key headers
	PrivateKeyPattern = regexp.MustCompile(`-----BEGIN (?:RSA|DSA|EC|PGP|OPENSSH) PRIVATE KEY-----`)

	// CreditCardPattern matches credit card numbers (basic Luhn-compatible format)
	CreditCardPattern = regexp.MustCompile(`\d{4}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{4}`)

	// TokenPattern matches bearer tokens and session tokens
	TokenPattern = regexp.MustCompile(`(?i)(?:bearer|token)['\"]?\s*[:=]\s*['\"]?([A-Za-z0-9\-_\.]{40,})['\"]?`)

	// PasswordPattern matches password assignments
	PasswordPattern = regexp.MustCompile(`(?i)(?:password|passwd|pwd)['\"]?\s*[:=]\s*['\"]?([^\s'\"]+)['\"]?`)

	// DatabaseURLPattern matches database connection strings
	DatabaseURLPattern = regexp.MustCompile(`(?i)(?:postgres|mysql|mongodb|redis)://.*?(?:@|$)`)
)

// PIIType represents the type of PII detected
type PIIType string

const (
	PIITypeEmail       PIIType = "email"
	PIITypePhone       PIIType = "phone"
	PIITypeSSN         PIIType = "ssn"
	PIITypeAPIKey      PIIType = "api_key"
	PIITypeAWSKey      PIIType = "aws_key"
	PIITypePrivateKey  PIIType = "private_key"
	PIITypeCreditCard  PIIType = "credit_card"
	PIITypeToken       PIIType = "token"
	PIITypePassword    PIIType = "password"
	PIITypeDatabaseURL PIIType = "database_url"
)

// SensitivityLevel defines the sensitivity of PII detection
type SensitivityLevel int

const (
	SensitivityLow SensitivityLevel = iota
	SensitivityMedium
	SensitivityHigh
)

// PIIMatch represents a detected PII match
type PIIMatch struct {
	Type      PIIType
	Value     string
	StartIdx  int
	EndIdx    int
	Line      int
	Sensitive bool // whether this type is sensitive (should trigger warnings)
}

// PatternConfig maps sensitivity levels to which patterns to detect
var PatternConfig = map[SensitivityLevel]map[PIIType]bool{
	SensitivityLow: {
		PIITypeEmail: true,
		// Low sensitivity: only obvious patterns
	},
	SensitivityMedium: {
		PIITypeEmail:       true,
		PIITypePhone:       true,
		PIITypeSSN:         true,
		PIITypeCreditCard:  true,
		PIITypeDatabaseURL: true,
	},
	SensitivityHigh: {
		PIITypeEmail:       true,
		PIITypePhone:       true,
		PIITypeSSN:         true,
		PIITypeAPIKey:      true,
		PIITypeAWSKey:      true,
		PIITypePrivateKey:  true,
		PIITypeCreditCard:  true,
		PIITypeToken:       true,
		PIITypePassword:    true,
		PIITypeDatabaseURL: true,
	},
}

// SensitiveTypes marks which PII types should trigger UI warnings
var SensitiveTypes = map[PIIType]bool{
	PIITypeSSN:        true,
	PIITypeAPIKey:     true,
	PIITypeAWSKey:     true,
	PIITypePrivateKey: true,
	PIITypeCreditCard: true,
	PIITypeToken:      true,
	PIITypePassword:   true,
}
