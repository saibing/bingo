package source

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/util"
	"path/filepath"
	"strings"
	"sync"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages"
)

type packagePool map[string]*packages.Package

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindPackageFunc func(packageCache *GlobalCache, importPath string) (*packages.Package, error)

type GlobalCache struct {
	mu      sync.RWMutex
	pool    packagePool
	rootDir string
	view    *View
}

func NewGlobalCache() *GlobalCache {
	return &GlobalCache{pool: packagePool{}}
}

func (c *GlobalCache) Init(ctx context.Context, conn jsonrpc2.JSONRPC2, root string, view *View) error {
	c.rootDir = root
	c.view = view

	err := c.buildCache(ctx, conn, nil)
	//msg := fmt.Sprintf("cache root package: %s successfully!", root)
	//conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: msg})
	return err
}

func (c *GlobalCache) Root() string {
	return c.rootDir
}

func (c *GlobalCache) Load(ctx context.Context, conn jsonrpc2.JSONRPC2, pkgDir string, overlay map[string][]byte) (*packages.Package, error) {
	loadDir := GetLoadDir(pkgDir)
	cacheKey := loadDir

	if util.IsWindows() {
		cacheKey = getCacheKeyFromDir(loadDir)
	}

	c.mu.RLock()

	pkg := c.pool[cacheKey]
	if pkg != nil {
		c.mu.RUnlock()
		return pkg, nil
	}

	c.mu.RUnlock()
	c.buildCache(context.Background(), conn, overlay)

	return c.pool[cacheKey], nil
}

func (c *GlobalCache) buildCache(ctx context.Context, conn jsonrpc2.JSONRPC2, overlay map[string][]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pool = packagePool{}

	loadDir := GetLoadDir(c.rootDir)

	cfg := &packages.Config{
		Dir:     loadDir,
		Fset:    c.view.Config.Fset,
		Mode:    packages.LoadAllSyntax,
		Context: ctx,
		Tests:   true,
		Overlay: overlay,
	}
	pkgList, err := packages.Load(cfg, loadDir+"/...")
	if err != nil {
		conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: err.Error()})
		return err
	}
	c.push(ctx, conn, pkgList)
	msg := fmt.Sprintf("cache package for %s successfully!", loadDir)
	conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: msg})
	return nil
}

func (c *GlobalCache) Iterate(visit func(p *packages.Package) error) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seen := map[string]bool{}

	for _, f := range c.view.files {
		pkg, _ := f.GetPackage()
		if pkg == nil || seen[pkg.PkgPath] {
			continue
		}

		seen[pkg.PkgPath] = true
		if err := visit(pkg); err != nil {
			return err
		}
	}

	for _, pkg := range c.pool {
		if seen[pkg.PkgPath] {
			continue
		}

		seen[pkg.PkgPath] = true

		if err := visit(pkg); err != nil {
			return err
		}
	}

	return nil
}

func (c *GlobalCache) pushWithLock(ctx context.Context, conn jsonrpc2.JSONRPC2, pkgList []*packages.Package) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.push(ctx, conn, pkgList)
}

func (c *GlobalCache) push(ctx context.Context, conn jsonrpc2.JSONRPC2, pkgList []*packages.Package) {
	for _, pkg := range pkgList {
		c.cache(ctx, conn, pkg)
	}
}

func (c *GlobalCache) cache(ctx context.Context, conn jsonrpc2.JSONRPC2, pkg *packages.Package) {
	if len(pkg.CompiledGoFiles) == 0 {
		return
	}

	cacheKey := getCacheKeyFromFile(pkg.CompiledGoFiles[0])

	if _, ok := c.pool[cacheKey]; ok {
		return
	}

	c.pool[cacheKey] = pkg

	msg := fmt.Sprintf("cached package %s", cacheKey)
	conn.Notify(ctx, "window/logMessage", &lsp.LogMessageParams{Type: lsp.Info, Message: msg})
	for _, importPkg := range pkg.Imports {
		c.cache(ctx, conn, importPkg)
	}
}

func (c *GlobalCache) Lookup(pkgPath string) *packages.Package {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, pkg := range c.pool {
		if pkg.PkgPath == pkgPath {
			return pkg
		}
	}

	return nil
}

func GetLoadDir(dir string) string {
	if !util.IsWindows() {
		return dir
	}

	if dir[0] == '/' {
		return dir[1:]
	}

	return dir
}

func getCacheKeyFromFile(filename string) string {
	dir := filepath.Dir(filename)
	return getCacheKeyFromDir(dir)
}

func getCacheKeyFromDir(dir string) string {
	if !util.IsWindows() {
		return dir
	}

	dirs := strings.Split(dir, ":")
	if len(dirs) >= 2 {
		dirs[0] = strings.ToUpper(dirs[0])
		dir = strings.Join(dirs, ":")
	}

	dir = strings.Replace(dir, "\\", "/", -1)
	return dir
}
