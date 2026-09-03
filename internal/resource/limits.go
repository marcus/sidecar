package resource

import "time"

// Protocol identifies the wire contract Sidecar speaks. A response that does
// not carry exactly this string is a transport failure, not a service failure.
const Protocol = "sidecar.terminal-resource/v1"

// Bounds from the protocol document's "Limits" table. They are defaults the
// host owns: a provider must never assume anything larger. Character bounds
// count runes, not bytes, so a multi-byte title is truncated where a reader
// would expect.
const (
	// MaxResponseBytes caps one invocation's stdout.
	MaxResponseBytes = 256 * 1024
	// MaxBodyBytes caps the sanitized body text.
	MaxBodyBytes = 64 * 1024
	// MaxFields caps the ordered label/value grid.
	MaxFields = 24
	// MaxFieldLabelChars and MaxFieldValueChars bound one grid cell.
	MaxFieldLabelChars = 64
	MaxFieldValueChars = 512

	// MaxTitleChars and MaxSubtitleChars bound the header lines.
	MaxTitleChars    = 300
	MaxSubtitleChars = 120

	// MaxIdentityChars and MaxLocatorChars bound the two identifiers that can
	// reach persisted state.
	MaxIdentityChars = 200
	MaxLocatorChars  = 200

	// MaxURLChars bounds sourceUrl and docsUrl.
	MaxURLChars = 2048

	// MaxMatchersPerProvider, MaxPatternChars, MaxMatchesPerLine and
	// MaxProviders bound the scanner's exposure to configuration.
	MaxMatchersPerProvider = 32
	MaxPatternChars        = 512
	MaxMatchesPerLine      = 32
	MaxProviders           = 16
)

// The rest of the Limits table. These were host choices during the draft and
// are now published values the protocol document points back at, so changing
// one changes the contract.
const (
	// MaxStatusLabelChars bounds status.label, which renders as one pill.
	MaxStatusLabelChars = 64
	// MaxMessageChars bounds a typed error's display message.
	MaxMessageChars = 512
	// MaxSetupHintChars bounds the copyable setup hint.
	MaxSetupHintChars = 512
	// MaxProviderKindChars, MaxProviderNameChars and MaxProviderVersionChars
	// bound the informational describe strings.
	MaxProviderKindChars    = 64
	MaxProviderNameChars    = 64
	MaxProviderVersionChars = 64
	// MaxMatcherIDChars bounds a matcher ID, which is persisted in resource
	// references.
	MaxMatcherIDChars = 64
	// MaxInstanceIDChars bounds a configured provider instance ID, which is
	// also persisted.
	MaxInstanceIDChars = 64
)

// Bounds on the collection shape of a Reference. They are stated here, beside
// the document shape's, because both shapes reach persisted state through one
// value and a bound that lived only in the plugin host would not be checked on
// the way back off disk.
const (
	// MaxCollectionIDChars bounds a plugin-declared collection ID. It matches
	// pluginhost.MaxCollectionIDChars; this package cannot import that one, so
	// a test over there pins the two together.
	MaxCollectionIDChars = 64
	// MaxQueryChars bounds a collection tab's persisted query — user-typed text
	// that survives a relaunch, so it is bounded on the way out as well as in.
	MaxQueryChars = 512
	// MaxViewIDChars and MaxSortIDChars bound the declared view and sort key a
	// collection tab remembers.
	MaxViewIDChars = 64
	MaxSortIDChars = 64
	// MaxFilters, MaxFilterIDChars and MaxFilterValueChars bound the applied
	// filter set a collection tab remembers. They match
	// pluginhost.MaxFilters/MaxFilterIDChars/MaxFilterValueChars; this package
	// cannot import that one, so a test over there pins them together.
	MaxFilters          = 8
	MaxFilterIDChars    = 32
	MaxFilterValueChars = 64
)

// Timeouts. describe is local and must be fast; resolve may cross a network.
const (
	DescribeTimeout       = 5 * time.Second
	DefaultResolveTimeout = 10 * time.Second
	MinResolveTimeout     = time.Second
	MaxResolveTimeout     = 60 * time.Second
)

// Freshness clamps for the provider's freshForSeconds hint. Absent or zero
// means "no hint" and takes the default; anything else is clamped into
// [MinFreshFor, MaxFreshFor].
const (
	DefaultFreshFor = 60 * time.Second
	MinFreshFor     = 10 * time.Second
	MaxFreshFor     = 15 * time.Minute
)

// ClampResolveTimeout brings a configured timeout into range. A non-positive
// value means "unset" and takes the default.
func ClampResolveTimeout(d time.Duration) time.Duration {
	switch {
	case d <= 0:
		return DefaultResolveTimeout
	case d < MinResolveTimeout:
		return MinResolveTimeout
	case d > MaxResolveTimeout:
		return MaxResolveTimeout
	default:
		return d
	}
}

// ClampFreshFor turns a provider's freshForSeconds hint into a duration the
// cache will honor. Negative and absurd values are not errors; they are hints
// the host declines to follow.
func ClampFreshFor(seconds float64) time.Duration {
	if seconds <= 0 {
		return DefaultFreshFor
	}
	d := time.Duration(seconds * float64(time.Second))
	switch {
	case d < MinFreshFor:
		return MinFreshFor
	case d > MaxFreshFor:
		return MaxFreshFor
	default:
		return d
	}
}
