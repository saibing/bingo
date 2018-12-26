package langserver

import (
	"fmt"
	"github.com/saibing/bingo/langserver/internal/cache"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/saibing/bingo/pkg/lsp"
	"golang.org/x/tools/go/packages"
	"strings"
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

func (h *HandlerShared) getFindPackageFunc() cache.FindPackageFunc {
	return defaultFindPackageFunc
}

func defaultFindPackageFunc(globalCache *cache.GlobalCache, pkgDir, importPath string) (*packages.Package, error) {
	if strings.HasPrefix(importPath, "/") {
		return nil, fmt.Errorf("import %q: cannot import absolute path", importPath)
	}

	if importPath == cache.BuiltinPkg {
		return globalCache.GetBuiltinPackage(), nil
	}

	return globalCache.GetFromPkgPath(pkgDir, importPath), nil
}