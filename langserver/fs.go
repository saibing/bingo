package langserver

import (
	"context"
	"encoding/json"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

// isFileSystemRequest returns if this is an LSP method whose sole
// purpose is modifying the contents of the overlay file system.
func isFileSystemRequest(method string) bool {
	return method == "textDocument/didOpen" ||
		method == "textDocument/didChange" ||
		method == "textDocument/didClose" ||
		method == "textDocument/didSave"
}

// handleFileSystemRequest handles textDocument/did* requests. The URI the
// request is for is returned. true is returned if a file was modified.
func (h *HandlerShared) handleFileSystemRequest(ctx context.Context, req *jsonrpc2.Request) error {
	overlay := h.overlay

	switch req.Method {
	case "textDocument/didOpen":
		var params lsp.DidOpenTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return err
		}
		overlay.didOpen(ctx, &params)
		return nil

	case "textDocument/didChange":
		var params lsp.DidChangeTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return err
		}

		return overlay.didChange(ctx, &params)

	case "textDocument/didClose":
		var params lsp.DidCloseTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return err
		}
		overlay.didClose(&params)
		return nil

	case "textDocument/didSave":
		var params lsp.DidSaveTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return err
		}
		// no-op
		return nil

	default:
		panic("unexpected file system request method: " + req.Method)
	}
}

// overlay owns the overlay filesystem, as well as handling LSP filesystem
// requests.
type overlay struct {
	conn               *jsonrpc2.Conn
	view               *source.View
	diagnosticsEnabled bool
}

func newOverlay(conn *jsonrpc2.Conn, diagnosticsEnabled bool) *overlay {
	return &overlay{conn: conn, view: source.NewView(), diagnosticsEnabled:diagnosticsEnabled}
}

func (h *overlay) cacheAndDiagnoseFile(ctx context.Context, uri lsp.DocumentURI, text string) {
	h.view.GetFile(source.FromDocumentURI(uri)).SetContent([]byte(text))

	if !h.diagnosticsEnabled {
		return
	}

	go func() {
		reports, err := diagnostics(h.view, uri)
		if err == nil {
			for filename, diagnostics := range reports {
				if len(diagnostics) == 0 {
					continue
				}
				params := &lsp.PublishDiagnosticsParams{
					URI:         lsp.DocumentURI(source.ToURI(filename)),
					Diagnostics: diagnostics,
				}

				h.conn.Notify(ctx, "textDocument/publishDiagnostics", params)
			}
		}
	}()
}

func (h *overlay) didOpen(ctx context.Context, params *lsp.DidOpenTextDocumentParams) {
	h.cacheAndDiagnoseFile(ctx, params.TextDocument.URI, params.TextDocument.Text)
}

func (h *overlay) didChange(ctx context.Context, params *lsp.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) < 1 {
		return &jsonrpc2.Error{Code: jsonrpc2.CodeMethodNotFound, Message: "no content changes provided"}
	}

	// We expect the full content of file, i.e. a single change with no range.
	if change := params.ContentChanges[0]; change.RangeLength == 0 {
		h.cacheAndDiagnoseFile(ctx, params.TextDocument.URI, change.Text)
	}
	return nil
}

func (h *overlay) didClose(params *lsp.DidCloseTextDocumentParams) {
	//h.view.GetFile(source.FromDocumentURI(params.TextDocument.URI)).SetContent(nil)
}

func (h *overlay) get(uri lsp.DocumentURI) ([]byte, bool) {
	file := h.view.GetFile(source.FromDocumentURI(uri))
	if file != nil {
		contents, err := file.Read()
		return contents, err == nil
	}
	return nil, false
}