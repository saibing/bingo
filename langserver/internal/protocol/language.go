package protocol

import (
	"github.com/sourcegraph/go-lsp"
)

/**
 * The kind of a code action.
 *
 * Kinds are a hierarchical list of identifiers separated by `.`, e.g. `"refactor.extract.function"`.
 *
 * The set of kinds is open and client needs to announce the kinds it supports to the server during
 * initialization.
 */
 type CodeActionKind string

 /**
 * A code action represents a change that can be performed in code, e.g. to fix a problem or
 * to refactor code.
 *
 * A CodeAction must set either `edit` and/or a `command`. If both are supplied, the `edit` is applied first, then the `command` is executed.
 */
 type CodeAction struct {

	/**
	 * A short, human-readable, title for this code action.
	 */
	Title string `json:"title"`

	/**
	 * The kind of the code action.
	 *
	 * Used to filter code actions.
	 */
	Kind CodeActionKind `json:"kind,omitempty"`

	/**
	 * The diagnostics that this code action resolves.
	 */
	Diagnostics []lsp.Diagnostic `json:"diagnostics,omitempty"`

	/**
	 * The workspace edit this code action performs.
	 */
	Edit lsp.WorkspaceEdit `json:"edit,omitempty"`

	/**
	 * A command this code action executes. If a code action
	 * provides an edit and a command, first the edit is
	 * executed and then the command.
	 */
	Command Command `json:"command,omitempty"`
}