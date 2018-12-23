package langserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/cache"
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
	conn                *jsonrpc2.Conn
	view                *cache.View
	diagnosticsDisabled bool
}

func newOverlay(conn *jsonrpc2.Conn, diagnosticsDisabled bool) *overlay {
	return &overlay{conn: conn, view: cache.NewView(), diagnosticsDisabled: diagnosticsDisabled}
}

func (h *overlay) didOpen(ctx context.Context, params *lsp.DidOpenTextDocumentParams) {
	h.cacheAndDiagnoseFile(ctx, params.TextDocument.URI, []byte(params.TextDocument.Text))
}

func (h *overlay) didChange(ctx context.Context, params *lsp.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) < 1 {
		return &jsonrpc2.Error{Code: jsonrpc2.CodeMethodNotFound, Message: "no content changes provided"}
	}

	contents, found := h.get(params.TextDocument.URI)
	if !found {
		return fmt.Errorf("received textDocument/didChange for unknown file %q", params.TextDocument.URI)
	}

	contents, err := applyContentChanges(params.TextDocument.URI, contents, params.ContentChanges)
	if err != nil {
		return err
	}

	h.cacheAndDiagnoseFile(ctx, params.TextDocument.URI, contents)
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

func (h *overlay) cacheAndDiagnoseFile(ctx context.Context, uri lsp.DocumentURI, text []byte) {
	sourceURI := source.FromDocumentURI(uri)
	f := h.view.GetFile(sourceURI)
	f.SetContent(text)

	if h.diagnosticsDisabled {
		return
	}

	go func() {
		reports, err := diagnostics(f)
		if err == nil {
			for filename, diagnostics := range reports {
				fileURI := source.ToURI(filename)
				if fileURI != sourceURI {
					continue
				}
				params := &lsp.PublishDiagnosticsParams{
					URI:         lsp.DocumentURI(fileURI),
					Diagnostics: diagnostics,
				}

				h.conn.Notify(ctx, "textDocument/publishDiagnostics", params)
			}
		}
	}()
}

// applyContentChanges updates `contents` based on `changes`
func applyContentChanges(uri lsp.DocumentURI, contents []byte, changes []lsp.TextDocumentContentChangeEvent) ([]byte, error) {
	for _, change := range changes {
		if change.Range == nil && change.RangeLength == 0 {
			contents = []byte(change.Text) // new full content
			continue
		}
		start, ok, why := offsetForPosition(contents, change.Range.Start)
		if !ok {
			return nil, fmt.Errorf("received textDocument/didChange for invalid position %q on %q: %s", change.Range.Start, uri, why)
		}
		var end int
		if change.RangeLength != 0 {
			end = start + int(change.RangeLength)
		} else {
			// RangeLength not specified, work it out from Range.End
			end, ok, why = offsetForPosition(contents, change.Range.End)
			if !ok {
				return nil, fmt.Errorf("received textDocument/didChange for invalid position %q on %q: %s", change.Range.Start, uri, why)
			}
		}
		if start < 0 || end > len(contents) || end < start {
			return nil, fmt.Errorf("received textDocument/didChange for out of range position %q on %q", change.Range, uri)
		}
		// Try avoid doing too many allocations, so use bytes.Buffer
		b := &bytes.Buffer{}
		b.Grow(start + len(change.Text) + len(contents) - end)
		b.Write(contents[:start])
		b.WriteString(change.Text)
		b.Write(contents[end:])
		contents = b.Bytes()
	}
	return contents, nil
}

func offsetForPosition(contents []byte, p lsp.Position) (offset int, valid bool, whyInvalid string) {
	line := 0
	col := 0
	// TODO(sqs): count chars, not bytes, per LSP. does that mean we
	// need to maintain 2 separate counters since we still need to
	// return the offset as bytes?
	for _, b := range contents {
		if line == p.Line && col == p.Character {
			return offset, true, ""
		}
		if (line == p.Line && col > p.Character) || line > p.Line {
			return 0, false, fmt.Sprintf("character %d is beyond line %d boundary", p.Character, p.Line)
		}
		offset++
		if b == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	if line == p.Line && col == p.Character {
		return offset, true, ""
	}
	if line == 0 {
		return 0, false, fmt.Sprintf("character %d is beyond first line boundary", p.Character)
	}
	return 0, false, fmt.Sprintf("file only has %d lines", line+1)
}
