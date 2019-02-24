package langserver

import (
	"runtime"
)

// Config adjusts the behaviour of go-langserver. Please keep in sync with
// InitializationOptions in the README.
type Config struct {
	// DisableFuncSnippet enables the returning of argument snippets on `func`
	// completions, eg. func(foo string, arg2 bar). Requires code complete
	// to be enabled.
	//
	// Defaults to true if not specified.
	DisableFuncSnippet bool

	// EnableGlobalCache enable global cache when hover, reference, definition. Can be overridden by InitializationOptions.
	//
	// Defaults to "always" if not specified
	GlobalCacheStyle string

	// DiagnosticsEnabled enables handling of diagnostics
	//
	// Defaults to false if not specified.
	DiagnosticsStyle string

	// FormatStyle format style
	//
	// Defaults to "gofmt" if not secified
	FormatStyle string

	// GoimportsLocalPrefix sets the local prefix (comma-separated string) that goimports will use
	//
	// Defaults to empty string if not specified.
	GoimportsLocalPrefix string

	// MaxParallelism controls the maximum number of goroutines that should be used
	// to fulfill requests. This is useful in editor environments where users do
	// not want results ASAP, but rather just semi quickly without eating all of
	// their CPU.
	//
	// Defaults to half of your CPU cores if not specified.
	MaxParallelism int

	// EnhanceSignatureHelp enhance the signature help with return result.
	//
	// Defaults to false
	EnhanceSignatureHelp bool

	// BuildTags controls build tag constraints and will be passed to build flags.
	//
	// Defaults to empty
	BuildTags []string
}

// Apply sets the corresponding field in c for each non-nil field in o.
func (c Config) Apply(o *InitializationOptions) Config {
	if o == nil {
		return c
	}
	if o.DisableFuncSnippet != nil {
		c.DisableFuncSnippet = *o.DisableFuncSnippet
	}

	if o.DiagnosticsStyle != nil {
		c.DiagnosticsStyle = *o.DiagnosticsStyle
	}

	if o.GlobalCacheStyle != nil {
		c.GlobalCacheStyle = *o.GlobalCacheStyle
	}

	if o.FormatStyle != nil {
		c.FormatStyle = *o.FormatStyle
	}

	if o.EnhanceSignatureHelp != nil {
		c.EnhanceSignatureHelp = *o.EnhanceSignatureHelp
	}

	if o.GoimportsLocalPrefix != nil {
		c.GoimportsLocalPrefix = *o.GoimportsLocalPrefix
	}

	if o.MaxParallelism != nil {
		c.MaxParallelism = *o.MaxParallelism
	}

	if o.BuildTags != nil {
		c.BuildTags = o.BuildTags
	}

	return c
}

// NewDefaultConfig returns the default config. See the field comments for the
// defaults.
func NewDefaultConfig() Config {
	// Default max parallelism to half the CPU cores, but at least always one.
	maxparallelism := runtime.NumCPU() / 2
	if maxparallelism <= 0 {
		maxparallelism = 1
	}

	return Config{
		DisableFuncSnippet: false,
		MaxParallelism:     maxparallelism,
	}
}
