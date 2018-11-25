package langserver

import (
	"runtime"
)

// Config adjusts the behaviour of go-langserver. Please keep in sync with
// InitializationOptions in the README.
type Config struct {
	// FuncSnippetEnabled enables the returning of argument snippets on `func`
	// completions, eg. func(foo string, arg2 bar). Requires code complete
	// to be enabled.
	//
	// Defaults to true if not specified.
	FuncSnippetEnabled bool

	// UseGlobalCache enable global cache when hover, reference, definition. Can be overridden by InitializationOptions.
	//
	// Defaults to false if not specified
	UseGlobalCache bool

	// DiagnosticsEnabled enables handling of diagnostics
	//
	// Defaults to false if not specified.
	DiagnosticsEnabled bool

	// MaxParallelism controls the maximum number of goroutines that should be used
	// to fulfill requests. This is useful in editor environments where users do
	// not want results ASAP, but rather just semi quickly without eating all of
	// their CPU.
	//
	// Defaults to half of your CPU cores if not specified.
	MaxParallelism int
}

// Apply sets the corresponding field in c for each non-nil field in o.
func (c Config) Apply(o *InitializationOptions) Config {
	if o == nil {
		return c
	}
	if o.FuncSnippetEnabled != nil {
		c.FuncSnippetEnabled = *o.FuncSnippetEnabled
	}

	if o.DiagnosticsEnabled != nil {
		c.DiagnosticsEnabled = *o.DiagnosticsEnabled
	}

	if o.UseGlobalCache != nil {
		c.UseGlobalCache = *o.UseGlobalCache
	}

	if o.MaxParallelism != nil {
		c.MaxParallelism = *o.MaxParallelism
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
		FuncSnippetEnabled: true,
		MaxParallelism:     maxparallelism,
	}
}

