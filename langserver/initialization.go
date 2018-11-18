package langserver

import "github.com/saibing/bingo/pkg/lsp"

// This file contains Go-specific extensions to LSP types.
//
// The Go language server MUST NOT rely on these extensions for
// standalone operation on the local file system. (VSCode has no way
// of including these fields.)

// InitializationOptions are the options supported by go-langserver. It is the
// Config struct, but each field is optional.
type InitializationOptions struct {
	// FuncSnippetEnabled is an optional version of Config.FuncSnippetEnabled
	FuncSnippetEnabled *bool `json:"funcSnippetEnabled"`

	// DiagnosticsEnabled enables handling of diagnostics
	//
	// Defaults to false if not specified.
	DiagnosticsEnabled *bool `json:"diagnosticsEnabled"`

	// NoGlobalCache do not use global package cache when hover and go to definition
	NoGlobalCache *bool `json:"noGlobalCache"`


	// MaxParallelism is an optional version of Config.MaxParallelism
	MaxParallelism *int `json:"maxParallelism"`
}

type InitializeParams struct {
	lsp.InitializeParams

	InitializationOptions *InitializationOptions `json:"initializationOptions,omitempty"`

	// TODO these should be InitializationOptions

	// NoOSFileSystemAccess makes the server never access the OS file
	// system. It exclusively uses the file overlay (from
	// textDocument/didOpen) and the LSP proxy's VFS.
	NoOSFileSystemAccess bool

	// BuildContext, if set, configures the language server's default
	// go/build.Context.
	BuildContext *InitializeBuildContextParams

	// RootImportPath is the root Go import path for this
	// workspace. For example,
	// "golang.org/x/tools" is the root import
	// path for "github.com/golang/tools".
	RootImportPath string
}

type InitializeBuildContextParams struct {
	// These fields correspond to the fields of the same name from
	// go/build.Context.

	GOOS        string
	GOARCH      string
	GOPATH      string
	GOROOT      string
	CgoEnabled  bool
	UseAllFiles bool
	Compiler    string
	BuildTags   []string

	// Irrelevant fields: ReleaseTags, InstallSuffix.
}
