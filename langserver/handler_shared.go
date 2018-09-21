package langserver

import (
	"context"
	"fmt"
	"github.com/sourcegraph/go-langserver/langserver/internal/caches"
	"go/build"
	"golang.org/x/tools/go/packages"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sourcegraph/ctxvfs"
)

// HandlerShared contains data structures that a build server and its
// wrapped lang server may share in memory.
type HandlerShared struct {
	Mu     sync.Mutex // guards all fields
	Shared bool       // true if this struct is shared with a build server
	FS     *AtomicFS  // full filesystem (mounts both deps and overlay)

	// FindPackage if non-nil is used by our typechecker. See
	// loader.Config.FindPackage. We use this in production to lazily
	// fetch dependencies + cache lookups.
	FindPackage FindPackageFunc

	overlay *overlay // files to overlay
}

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindPackageFunc func(ctx context.Context, bctx *build.Context, importPath, fromDir string, mode build.ImportMode) (*build.Package, error)

func defaultFindPackageFunc(ctx context.Context, bctx *build.Context, importPath, fromDir string, mode build.ImportMode) (*build.Package, error) {
	return bctx.Import(importPath, fromDir, mode)
}

// getFindPackageFunc is a helper which returns h.FindPackage if non-nil, otherwise defaultFindPackageFunc
func (h *HandlerShared) getFindPackageFunc() FindPackageFunc {
	if h.FindPackage != nil {
		return h.FindPackage
	}
	return defaultFindPackageFunc
}

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindModulePackageFunc func(packageCache *caches.PackageCache, importPath, fromDir string) (*packages.Package, error)


func (h *HandlerShared) getFindModulePackageFunc() FindModulePackageFunc {
	return defaultModuleFindPackageFunc
}

func defaultModuleFindPackageFunc(packageCache *caches.PackageCache, importPath, fromDir string) (*packages.Package, error) {
	if strings.HasPrefix(importPath, "/") {
		return nil, fmt.Errorf("import %q: cannot import absolute path", importPath)
	}

	if build.IsLocalImport(importPath) {
		dir := filepath.Join(fromDir, importPath)
		return packageCache.Load(dir)
	}

	return packageCache.Lookup(importPath), nil
}


func (h *HandlerShared) Reset(useOSFS bool) error {
	h.Mu.Lock()
	defer h.Mu.Unlock()
	h.overlay = newOverlay()
	h.FS = NewAtomicFS()

	if useOSFS {
		// The overlay FS takes precedence, but we fall back to the OS
		// file system.
		h.FS.Bind("/", ctxvfs.OS("/"), "/", ctxvfs.BindAfter)
	}
	h.FS.Bind("/", h.overlay.FS(), "/", ctxvfs.BindBefore)
	return nil
}
