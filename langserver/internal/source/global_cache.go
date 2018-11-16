package source

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/saibing/bingo/langserver/internal/util"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages"
)

type uri2Package map[string]*packages.Package
type path2Package map[string]*packages.Package

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindPackageFunc func(globalCache *GlobalCache, importPath string) (*packages.Package, error)

type GlobalCache struct {
	mu             sync.RWMutex
	conn jsonrpc2.JSONRPC2
	view           *View
	urlMap         uri2Package
	pathMap		   path2Package
	rootDir        string
	mainModulePath string
	moduleMap      map[string]moduleInfo
}

func NewGlobalCache() *GlobalCache {
	return &GlobalCache{}
}

type moduleInfo struct {
	Path     string    `json:"Path"`
	Main     bool      `json:"Main"`
	Dir      string    `json:"Dir"`
	GoMod    string    `json:"GoMod"`
	Version  string    `json:"Version"`
	Time     time.Time `json:"Time"`
	Indirect bool      `json:Indirect`
}

func (c *GlobalCache) MainModulePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mainModulePath
}

func (c *GlobalCache) GetFromPackagePath(pkgPath string) *packages.Package {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pathMap[pkgPath]
}

func (c *GlobalCache) GetFromURI(pkgDir string) *packages.Package {
	cacheKey := pkgDir
	if util.IsWindows() {
		cacheKey = getCacheKeyFromDir(pkgDir)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.urlMap[cacheKey]
}

func (c *GlobalCache) Init(ctx context.Context, conn jsonrpc2.JSONRPC2, root string, view *View) error {
	c.conn = conn
	c.rootDir = root
	c.view = view

	moduleMap, err := c.readModuleFromFile()
	if err != nil {
		return err
	}

	c.initModule(moduleMap)

	pkgList, err := c.buildCache(ctx)
	if err != nil {
		return err
	}

	c.setCache(ctx, pkgList)

	return c.fsNotify()
}

func (c *GlobalCache) fsNotify() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				//log.Println("event:", event)
				if event.Op&fsnotify.Write == fsnotify.Write {
					//log.Println("modified file:", event.Name)
					c.conn.Notify(context.Background(), "window/showMessage",
						&lsp.ShowMessageParams{Type: lsp.Info, Message: fmt.Sprintf("rebuile module cache for %s changed", event.Name)})
					c.rebuildCache()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				c.conn.Notify(context.Background(), "window/showMessage",
					&lsp.ShowMessageParams{Type: lsp.MTError, Message: fmt.Sprintf("receive an fsNotify error: %s", err)})
			}
		}
	}()

	err = watcher.Add(filepath.Join(c.rootDir, "go.mod"))
	if err != nil {
		return err
	}
	<-done

	return nil
}

func (c *GlobalCache) readModuleFromFile() (map[string]moduleInfo, error) {
	buf, err := invokeGo(context.Background(), c.rootDir, "list", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}

	var modules []moduleInfo

	err = json.Unmarshal(buf.Bytes(), &modules)
	if err != nil {
		return nil, err
	}

	moduleMap := map[string]moduleInfo{}
	for _, module := range  modules {
		moduleMap[module.Dir] = module
	}

	return moduleMap, nil
}

func (c *GlobalCache) initModule(moduleMap map[string]moduleInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, module := range moduleMap {
		if module.Main {
			c.mainModulePath = module.Path
		}
	}

	c.moduleMap = moduleMap
}

func (c *GlobalCache) rebuildCache() error {
	moduleMap, err := c.readModuleFromFile()
	if err != nil {
		c.conn.Notify(context.Background(), "window/showMessage",
			&lsp.ShowMessageParams{Type: lsp.MTError, Message: fmt.Sprintf("read go module info failed: %s", err)})
		return err
	}

	if !c.hasChanged(moduleMap) {
		return nil
	}

	c.initModule(moduleMap)

	ctx := context.Background()
	pkgList, err := c.buildCache(ctx)
	if err != nil {
		return err
	}

	c.setCache(ctx, pkgList)
	return nil
}

func (c *GlobalCache) hasChanged(moduleMap map[string]moduleInfo) bool {
	for dir := range moduleMap {
		// there are some new module add into go.mod
		if _, ok := c.moduleMap[dir]; !ok {
			return true
		}
	}

	return false
}

func (c *GlobalCache) buildCache(ctx context.Context) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Dir:      c.rootDir,
		Fset:    c.view.Config.Fset,
		Mode:    packages.LoadAllSyntax,
		Context: ctx,
		Tests:   true,
		Overlay: nil,
	}

	pkgList, err := packages.Load(cfg,  c.rootDir+"/...")
	if err != nil {
		c.conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: err.Error()})
	}

	return pkgList, err
}

func (c *GlobalCache) setCache(ctx context.Context, pkgList []*packages.Package)  {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.urlMap = uri2Package{}
	c.pathMap = path2Package{}
	for _, pkg := range pkgList {
		c.cache(ctx, pkg)
	}

	msg := fmt.Sprintf("cache package for %s successfully!", c.rootDir)
	c.conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: msg})
}

func (c *GlobalCache) Iterate(visit func(p *packages.Package) error) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seen := map[string]bool{}

	for _, f := range c.view.files {
		uri, _ := f.URI.Filename()
		if !strings.HasPrefix(uri, c.rootDir) {
			continue
		}

		pkg, _ := f.GetPackage()
		if pkg == nil || seen[pkg.PkgPath] {
			continue
		}

		seen[pkg.PkgPath] = true
		if err := visit(pkg); err != nil {
			return err
		}
	}

	for _, pkg := range c.urlMap {
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

func (c *GlobalCache) cache(ctx context.Context, pkg *packages.Package) {
	c.pathMap[pkg.PkgPath] = pkg

	if len(pkg.CompiledGoFiles) == 0 {
		return
	}

	cacheKey := getCacheKeyFromFile(pkg.CompiledGoFiles[0])

	if _, ok := c.urlMap[cacheKey]; ok {
		return
	}

	c.urlMap[cacheKey] = pkg

	msg := fmt.Sprintf("cached package %s for dir %s", pkg.PkgPath, cacheKey)
	c.conn.Notify(ctx, "window/logMessage", &lsp.LogMessageParams{Type: lsp.Info, Message: msg})
	for _, importPkg := range pkg.Imports {
		c.cache(ctx, importPkg)
	}
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
