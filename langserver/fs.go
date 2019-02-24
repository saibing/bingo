package langserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"unicode/utf8"

	"github.com/saibing/bingo/langserver/internal/cache"
	"github.com/saibing/bingo/langserver/internal/source"
	lsp "github.com/sourcegraph/go-lsp"
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

		if err := checkFileURI(params.TextDocument.URI); err != nil {
			return err
		}

		overlay.didOpen(ctx, &params)
		return nil

	case "textDocument/didChange":
		var params lsp.DidChangeTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return err
		}

		if err := checkFileURI(params.TextDocument.URI); err != nil {
			return err
		}

		return overlay.didChange(ctx, &params)

	case "textDocument/didClose":
		var params lsp.DidCloseTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return err
		}

		if err := checkFileURI(params.TextDocument.URI); err != nil {
			return err
		}

		overlay.didClose(ctx, &params)
		return nil

	case "textDocument/didSave":
		var params lsp.DidSaveTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return err
		}

		if err := checkFileURI(params.TextDocument.URI); err != nil {
			return err
		}

		overlay.didSave(ctx, &params)
		return nil

	default:
		panic("unexpected file system request method: " + req.Method)
	}
}

// overlay owns the overlay filesystem, as well as handling LSP filesystem
// requests.
type overlay struct {
	conn             *jsonrpc2.Conn
	project          *cache.Project
	diagnosticsStyle DiagnosticsStyleEnum
}

func newOverlay(conn *jsonrpc2.Conn, project *cache.Project, diagnosticsStyle DiagnosticsStyleEnum) *overlay {
	return &overlay{conn: conn, project: project, diagnosticsStyle: diagnosticsStyle}
}

func (h *overlay) view() source.View {
	return h.project.View()
}

func (h *overlay) didOpen(ctx context.Context, params *lsp.DidOpenTextDocumentParams) {
	h.cacheAndDiagnose(ctx, params.TextDocument.URI, []byte(params.TextDocument.Text))
}

func (h *overlay) didChange(ctx context.Context, params *lsp.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) < 1 {
		return &jsonrpc2.Error{Code: jsonrpc2.CodeMethodNotFound, Message: "no content changes provided"}
	}

	text, err := h.applyChanges(ctx, params)
	if err != nil {
		return err
	}

	h.cacheAndDiagnose(ctx, params.TextDocument.URI, text)
	return nil
}

func (h *overlay) didClose(ctx context.Context, params *lsp.DidCloseTextDocumentParams) {
	uri := source.FromDocumentURI(params.TextDocument.URI)
	h.setContent(ctx, uri, nil)
}

func (h *overlay) didSave(ctx context.Context, param *lsp.DidSaveTextDocumentParams) {
	if h.diagnosticsStyle != onsaveDiagnostics {
		return
	}

	sourceURI := source.FromDocumentURI(param.TextDocument.URI)
	f, err := h.view().GetFile(ctx, sourceURI)
	if err != nil {
		log.Fatal(err)
		return
	}
	h.diagnosetics(ctx, f)
}

func (h *overlay) cacheAndDiagnose(ctx context.Context, uri lsp.DocumentURI, text []byte) {
	sourceURI := source.FromDocumentURI(uri)
	h.setContent(ctx, sourceURI, text)
	f, err := h.view().GetFile(ctx, sourceURI)
	if err != nil {
		return
	}
	if h.diagnosticsStyle != instantDiagnostics {
		return
	}

	go h.diagnosetics(ctx, f)
}

func (h *overlay) setContent(ctx context.Context, uri source.URI, content []byte) error {
	v, err := h.view().SetContent(ctx, uri, content)
	if err != nil {
		return err
	}

	h.project.SetView(v)

	return nil
}

type DiagnosticsStyleEnum string

const (
	noneDiagnostics    DiagnosticsStyleEnum = "none"
	onsaveDiagnostics  DiagnosticsStyleEnum = "onsave"
	instantDiagnostics DiagnosticsStyleEnum = "instant"
)

func (h *overlay) diagnosetics(ctx context.Context, f source.File) {
	reports, err := diagnostics(f)
	if err == nil {
		for filename, diagnostics := range reports {
			fileURI := source.ToURI(filename)
			params := &lsp.PublishDiagnosticsParams{
				URI:         lsp.DocumentURI(fileURI),
				Diagnostics: diagnostics,
			}

			h.conn.Notify(ctx, "textDocument/publishDiagnostics", params)
		}
	}
}

func bytesOffset(content []byte, pos lsp.Position) int {
	var line, char, offset int
	if len(content) == 0 && pos.Line == 0 && pos.Character == 0 {
		return 0
	}

	for len(content) > 0 {
		if line == int(pos.Line) && char == int(pos.Character) {
			return offset
		}
		r, size := utf8.DecodeRune(content)
		char++
		// The offsets are based on a UTF-16 string representation.
		// So the rune should be checked twice for two code units in UTF-16.
		if r >= 0x10000 {
			if line == int(pos.Line) && char == int(pos.Character) {
				return offset
			}
			char++
		}
		offset += size
		content = content[size:]
		if r == '\n' {
			line++
			char = 0
		}
	}

	if line == int(pos.Line) && char == int(pos.Character) {
		return offset
	}

	return -1
}

func newJsonrpc2Errorf(code int64, message string) error {
	return &jsonrpc2.Error{Code: code, Message: message}
}

func (h *overlay) applyChanges(ctx context.Context, params *lsp.DidChangeTextDocumentParams) ([]byte, error) {
	if len(params.ContentChanges) == 1 && params.ContentChanges[0].Range == nil {
		// If range is empty, we expect the full content of file, i.e. a single change with no range.
		change := params.ContentChanges[0]
		if change.RangeLength != 0 {
			return nil, newJsonrpc2Errorf(jsonrpc2.CodeInternalError, "unexpected change range provided")
		}
		return []byte(change.Text), nil
	}

	sourceURI, err := fromProtocolURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}

	file, err := h.project.View().GetFile(ctx, sourceURI)
	if err != nil {
		return nil, newJsonrpc2Errorf(jsonrpc2.CodeInternalError, "file not found")
	}

	content := file.GetContent()
	for _, change := range params.ContentChanges {
		start := bytesOffset(content, change.Range.Start)
		if start == -1 {
			return nil, newJsonrpc2Errorf(jsonrpc2.CodeInternalError, "invalid range for content change")
		}
		end := bytesOffset(content, change.Range.End)
		if end == -1 {
			return nil, newJsonrpc2Errorf(jsonrpc2.CodeInternalError, "invalid range for content change")
		}
		var buf bytes.Buffer
		buf.Write(content[:start])
		buf.WriteString(change.Text)
		buf.Write(content[end:])
		content = buf.Bytes()
	}
	return content, nil
}
