package caches

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages"
)

type packagePool map[string]*packages.Package

type PackageCache struct {
	mu      sync.RWMutex
	pool    packagePool
	rootDir string
}

func New() *PackageCache {
	return &PackageCache{pool: packagePool{}}
}

const windowsOS = "windows"

func (c *PackageCache) Init(ctx context.Context, conn jsonrpc2.JSONRPC2, root string) error {
	c.rootDir = root
	return c.buildCache(ctx, conn, nil)
}

func (c *PackageCache) Root() string {
	return c.rootDir
}

func (c *PackageCache) Load(ctx context.Context, conn jsonrpc2.JSONRPC2, pkgDir string, overlay map[string][]byte) (*packages.Package, error) {
	loadDir := getLoadDir(pkgDir)
	cacheKey := loadDir

	if runtime.GOOS == windowsOS {
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

func (c *PackageCache) buildCache(ctx context.Context, conn jsonrpc2.JSONRPC2, overlay map[string][]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pool = packagePool{}

	loadDir := getLoadDir(c.rootDir)
	msg := fmt.Sprintf("cache root package: %s ...", loadDir)
	conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: msg})
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Context: ctx, Tests: true, Overlay: overlay}
	pkgList, err := packages.Load(cfg, loadDir+"/...")
	if err != nil {
		conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: err.Error()})
		return err
	}
	c.push(ctx, conn, pkgList)

	msg = fmt.Sprintf("cache root package: %s successfully!", loadDir)
	conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: msg})
	return nil
}

func (c *PackageCache) Iterate(visit func(p *packages.Package) error) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, pkg := range c.pool {
		if err := visit(pkg); err != nil {
			return err
		}
	}

	return nil
}

func (c *PackageCache) pushWithLock(ctx context.Context, conn jsonrpc2.JSONRPC2, pkgList []*packages.Package) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.push(ctx, conn, pkgList)
}

func (c *PackageCache) push(ctx context.Context, conn jsonrpc2.JSONRPC2, pkgList []*packages.Package) {
	for _, pkg := range pkgList {
		c.cache(ctx, conn, pkg)
	}
}

func (c *PackageCache) cache(ctx context.Context, conn jsonrpc2.JSONRPC2, pkg *packages.Package) {
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

func (c *PackageCache) Lookup(pkgPath string) *packages.Package {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, pkg := range c.pool {
		if pkg.PkgPath == pkgPath {
			return pkg
		}
	}

	return nil
}

func getLoadDir(dir string) string {
	if runtime.GOOS != windowsOS {
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
	if runtime.GOOS != windowsOS {
		return dir
	}

	dirs := strings.Split(dir, ":")
	if len(dirs) >= 2 {
		dirs[0] = strings.ToLower(dirs[0])
		dir = strings.Join(dirs, ":")
	}

	dir = strings.Replace(dir, "\\", "/", -1)
	return dir
}
