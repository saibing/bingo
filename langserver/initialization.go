package langserver

import lsp "github.com/sourcegraph/go-lsp"

// This file contains Go-specific extensions to LSP types.
//
// The Go language server MUST NOT rely on these extensions for
// standalone operation on the local file system. (VSCode has no way
// of including these fields.)

// InitializationOptions are the options supported by go-langserver. It is the
// Config struct, but each field is optional.
type InitializationOptions struct {
	// DisableFuncSnippet is an optional version of Config.DisableFuncSnippet
	DisableFuncSnippet *bool `json:"disableFuncSnippet"`

	// DiagnosticsEnabled enables handling of diagnostics
	//
	// Defaults to false if not specified.
	DiagnosticsStyle *string `json:"diagnosticsStyle"`

	// EnableGlobalCache enable global cache when hover, reference, definition. Can be overridden by InitializationOptions.
	//
	// Defaults to false if not specified
	GlobalCacheStyle *string `json:"globalCacheStyle"`

	// FormatStyle format style
	//
	// Defaults to "gofmt" if not specified
	FormatStyle *string `json:"formatStyle"`

	// Enhance sigature help
	//
	// Defaults to false if not specified
	EnhanceSignatureHelp *bool `json:"enhanceSignatureHelp"`

	// GoimportsLocalPrefix is an optional version of
	// Config.GoimportsLocalPrefix
	GoimportsLocalPrefix *string `json:"goimportsLocalPrefix"`

	// BuildTags is an optional version of Config.BuildTags
	BuildTags []string `json:"buildTags"`
}

type InitializeParams struct {
	lsp.InitializeParams

	InitializationOptions *InitializationOptions `json:"initializationOptions,omitempty"`

	// TODO these should be InitializationOptions
	// RootImportPath is the root Go import path for this
	// workspace. For example,
	// "golang.org/x/tools" is the root import
	// path for "github.com/golang/tools".
	RootImportPath string
}
