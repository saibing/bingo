package langserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/source"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/ctxvfs"
	"github.com/sourcegraph/jsonrpc2"

	"github.com/saibing/bingo/langserver/internal/util"
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
func (h *HandlerShared) handleFileSystemRequest(ctx context.Context, req *jsonrpc2.Request) (lsp.DocumentURI, bool, error) {
	h.Mu.Lock()
	overlay := h.overlay
	h.Mu.Unlock()

	do := func(uri lsp.DocumentURI, op func() error) (lsp.DocumentURI, bool, error) {
		before, beforeErr := h.readFile(ctx, uri)
		if beforeErr != nil && !os.IsNotExist(beforeErr) {
			// There is no op that could succeed in this case. (Most
			// commonly occurs when uri refers to a dir, not a file.)
			return uri, false, beforeErr
		}
		err := op()
		after, afterErr := h.readFile(ctx, uri)
		if os.IsNotExist(beforeErr) && os.IsNotExist(afterErr) {
			// File did not exist before or after so nothing has changed.
			return uri, false, err
		} else if afterErr != nil || beforeErr != nil {
			// If an error prevented us from reading the file
			// before or after then we assume the file changed to
			// be conservative.
			return uri, true, err
		}
		return uri, !bytes.Equal(before, after), err
	}

	switch req.Method {
	case "textDocument/didOpen":
		var params lsp.DidOpenTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return "", false, err
		}
		return do(params.TextDocument.URI, func() error {
			overlay.didOpen(ctx, &params)
			return nil
		})

	case "textDocument/didChange":
		var params lsp.DidChangeTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return "", false, err
		}
		return do(params.TextDocument.URI, func() error {
			return overlay.didChange(ctx, &params)
		})

	case "textDocument/didClose":
		var params lsp.DidCloseTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return "", false, err
		}
		return do(params.TextDocument.URI, func() error {
			overlay.didClose(&params)
			return nil
		})

	case "textDocument/didSave":
		var params lsp.DidSaveTextDocumentParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return "", false, err
		}
		// no-op
		return params.TextDocument.URI, false, nil

	default:
		panic("unexpected file system request method: " + req.Method)
	}
}

// overlay owns the overlay filesystem, as well as handling LSP filesystem
// requests.
type overlay struct {
	conn *jsonrpc2.Conn
	view *source.View
}

func newOverlay(conn *jsonrpc2.Conn) *overlay {
	return &overlay{conn: conn, view: source.NewView()}
}

// FS returns a vfs for the overlay.
func (h *overlay) FS() ctxvfs.FileSystem {
	return nil
}

func (h *overlay) cacheAndDiagnoseFile(ctx context.Context, uri lsp.DocumentURI, text string, immediate bool) {
	if immediate {
		h.view.GetFile(source.FromDocumentURI(uri)).SetContent([]byte(text))
		return
	}

	go func() {
		h.view.GetFile(source.FromDocumentURI(uri)).SetContent([]byte(text))
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
	h.cacheAndDiagnoseFile(ctx, params.TextDocument.URI, params.TextDocument.Text, false)
}

func (h *overlay) didChange(ctx context.Context, params *lsp.DidChangeTextDocumentParams) error {
	contents, found := h.get(params.TextDocument.URI)
	if !found {
		return fmt.Errorf("received textDocument/didChange for unknown file %q", params.TextDocument.URI)
	}

	contents, err := applyContentChanges(params.TextDocument.URI, contents, params.ContentChanges)
	if err != nil {
		return err
	}

	h.cacheAndDiagnoseFile(ctx, params.TextDocument.URI, string(contents), true)
	return nil
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

func (h *overlay) didClose(params *lsp.DidCloseTextDocumentParams) {
	//h.view.GetFile(source.FromDocumentURI(params.TextDocument.URI)).SetContent(nil)
}

func uriToOverlayPath(uri lsp.DocumentURI) string {
	if util.IsURI(uri) {
		return strings.TrimPrefix(util.UriToPath(uri), "/")
	}
	return string(uri)
}

func (h *overlay) get(uri lsp.DocumentURI) ([]byte, bool) {
	file := h.view.GetFile(source.FromDocumentURI(uri))
	if file != nil {
		contents, err := file.Read()
		return contents, err == nil
	}
	return nil, false
}

func (h *HandlerShared) FilePath(uri lsp.DocumentURI) string {
	path := util.UriToPath(uri)
	if !strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("bad uri %q (path %q MUST have leading slash; it can't be relative)", uri, path))
	}
	return path
}

func (h *HandlerShared) readFile(ctx context.Context, uri lsp.DocumentURI) ([]byte, error) {
	if !util.IsURI(uri) {
		return nil, &os.PathError{Op: "Open", Path: string(uri), Err: errors.New("unable to read out-of-workspace resource from virtual file system")}
	}
	h.Mu.Lock()
	fs := h.FS
	h.Mu.Unlock()
	path := h.FilePath(uri)
	contents, err := ctxvfs.ReadFile(ctx, fs, path)
	if os.IsNotExist(err) {
		if _, ok := err.(*os.PathError); !ok {
			err = &os.PathError{Op: "Open", Path: path, Err: err}
		}
	}
	return contents, err
}

// AtomicFS wraps a ctxvfs.NameSpace but is safe for concurrent calls to Bind
// while doing FS operations. It is optimized for "ReadMostly" use-case. IE
// Bind is a relatively rare call compared to actual FS operations.
type AtomicFS struct {
	mu sync.Mutex   // serialize calls to Bind (ie only used by writers)
	v  atomic.Value // stores the ctxvfs.NameSpace
}

// NewAtomicFS returns an AtomicFS with an empty wrapped ctxvfs.NameSpace
func NewAtomicFS() *AtomicFS {
	fs := &AtomicFS{}
	fs.v.Store(make(ctxvfs.NameSpace))
	return fs
}

// Bind wraps ctxvfs.NameSpace.Bind
func (a *AtomicFS) Bind(old string, newfs ctxvfs.FileSystem, new string, mode ctxvfs.BindMode) {
	// We do copy-on-write
	a.mu.Lock()
	defer a.mu.Unlock()

	fs1 := a.v.Load().(ctxvfs.NameSpace)
	fs2 := make(ctxvfs.NameSpace)
	for k, v := range fs1 {
		fs2[k] = v
	}
	fs2.Bind(old, newfs, new, mode)
	a.v.Store(fs2)
}

func (*AtomicFS) String() string {
	return "atomicfs"
}

// Open wraps ctxvfs.NameSpace.Open
func (a *AtomicFS) Open(ctx context.Context, path string) (ctxvfs.ReadSeekCloser, error) {
	fs := a.v.Load().(ctxvfs.NameSpace)
	return fs.Open(ctx, path)
}

// Stat wraps ctxvfs.NameSpace.Stat
func (a *AtomicFS) Stat(ctx context.Context, path string) (os.FileInfo, error) {
	fs := a.v.Load().(ctxvfs.NameSpace)
	return fs.Stat(ctx, path)
}

// Lstat wraps ctxvfs.NameSpace.Lstat
func (a *AtomicFS) Lstat(ctx context.Context, path string) (os.FileInfo, error) {
	fs := a.v.Load().(ctxvfs.NameSpace)
	return fs.Lstat(ctx, path)
}

// ReadDir wraps ctxvfs.NameSpace.ReadDir
func (a *AtomicFS) ReadDir(ctx context.Context, path string) ([]os.FileInfo, error) {
	fs := a.v.Load().(ctxvfs.NameSpace)
	return fs.ReadDir(ctx, path)
}
