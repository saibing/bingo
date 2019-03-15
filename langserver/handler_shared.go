package langserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/saibing/bingo/langserver/internal/cache"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/sourcegraph/go-lsp"
)

// HandlerShared contains data structures that a build server and its
// wrapped lang server may share in memory.
type HandlerShared struct {
	overlay *overlay // files to overlay
}

func (h *HandlerShared) FilePath(uri lsp.DocumentURI) string {
	path := util.UriToPath(uri)
	if !strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("bad uri %q (path %q MUST have leading slash; it can't be relative)", uri, path))
	}
	return util.GetRealPath(path)
}

func (h *HandlerShared) notifyError(message string) {
	_ = h.overlay.conn.Notify(context.Background(), "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: message})
}

// NotifyInfo notify info to lsp client
func (h *HandlerShared) notifyInfo(message string) {
	_ = h.overlay.conn.Notify(context.Background(), "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: message})
}

// NotifyLog notify log to lsp client
func (h *HandlerShared) notifyLog(message string) {
	_ = h.overlay.conn.Notify(context.Background(), "window/logMessage", &lsp.LogMessageParams{Type: lsp.Info, Message: message})
}

func (h *HandlerShared) View() source.View {
	return h.overlay.view()
}

func (h *HandlerShared) getFindPackageFunc() cache.FindPackageFunc {
	return defaultFindPackageFunc
}

func defaultFindPackageFunc(project *cache.Project, importPath string) (source.Package, error) {
	if strings.HasPrefix(importPath, "/") {
		return nil, fmt.Errorf("import %q: cannot import absolute path", importPath)
	}

	return project.GetFromPkgPath(importPath), nil
}
