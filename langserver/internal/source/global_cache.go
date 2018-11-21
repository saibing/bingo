package source

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/saibing/bingo/langserver/internal/util"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages"
)

type path2Package map[string]*packages.Package

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindPackageFunc func(globalCache *GlobalCache, importPath string) (*packages.Package, error)

type GlobalCache struct {
	mu             sync.RWMutex
	conn           jsonrpc2.JSONRPC2
	view           *View
	pathMap        path2Package
	searchOrder    []string
	rootDir        string
	mainModulePath string
	moduleMap      map[string]moduleInfo
	goroot         string
}

func NewGlobalCache() *GlobalCache {
	return &GlobalCache{goroot: getGoRoot()}
}

func getGoRoot() string {
	root := runtime.GOROOT()
	root = filepath.Join(root, "src")
	return lowerDriver(root)
}

func lowerDriver(path string) string {
	if !util.IsWindows() {
		return path
	}

	return strings.ToLower(path[0:1]) + path[1:]
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

func (c *GlobalCache) getPackagePath(filename string) (pkgPath string, testPkgPath string) {
	dir := lowerDriver(filepath.Dir(filename))
	base := filepath.Base(filename)

	if strings.HasPrefix(dir, c.goroot) {
		pkgPath = dir[len(c.goroot)+1:]
	} else {
		for k, v := range c.moduleMap {
			if strings.HasPrefix(dir, k) {
				pkgPath = filepath.Join(v.Path, dir[len(k):])
				break
			}
		}
	}

	pkgPath = filepath.ToSlash(pkgPath)

	if strings.HasSuffix(base, "_test.go") {
		testPkgPath = pkgPath + ".test"
	}
	return pkgPath, testPkgPath
}

func (c *GlobalCache) GetFromFilename(filename string) *packages.Package {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pkgPath, testPkgPath := c.getPackagePath(filename)
	if testPkgPath != "" {
		pkg := c.pathMap[testPkgPath]
		return pkg.Imports[pkgPath]
	}
	return c.pathMap[pkgPath]
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

	err = watcher.Add(filepath.Join(c.rootDir, "go.mod"))
	if err != nil {
		watcher.Close()
		return err
	}

	go func() {
		defer watcher.Close()

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

	return nil
}

func (c *GlobalCache) readModuleFromFile() (map[string]moduleInfo, error) {
	moduleFile := filepath.Join(c.rootDir, "go.mod")
	_, err := os.Stat(moduleFile)
	if err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("%s does not exist, please use 'go mod init' to create it", moduleFile)
		}
		return nil, err
	}

	buf, err := invokeGo(context.Background(), c.rootDir, "list", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}

	var modules []moduleInfo

	decoder := json.NewDecoder(buf)
	for {
		module := moduleInfo{}
		err = decoder.Decode(&module)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}

	moduleMap := map[string]moduleInfo{}
	for _, module := range modules {
		if module.Dir == "" {
			// module define in go.mod but not in ${GOMOD}
			continue
		}
		moduleMap[lowerDriver(module.Dir)] = module
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
	cfg := *c.view.Config
	cfg.Dir = c.rootDir
	cfg.Mode = packages.LoadAllSyntax
	cfg.Fset = token.NewFileSet()

	pkgList, err := packages.Load(&cfg, c.rootDir+"/...")
	if err != nil {
		c.conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: err.Error()})
	}

	return pkgList, err
}

func (c *GlobalCache) setCache(ctx context.Context, pkgList []*packages.Package) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pathMap = path2Package{}

	var thirdParty []string
	var stdLib []string

	for _, pkg := range pkgList {
		c.cache(ctx, pkg)
		if strings.HasPrefix(pkg.PkgPath, c.mainModulePath) {
			c.searchOrder = append(c.searchOrder, pkg.PkgPath)
		} else if strings.Contains(pkg.PkgPath, ".") {
			thirdParty = append(thirdParty, pkg.PkgPath)
		} else {
			stdLib = append(stdLib, pkg.PkgPath)
		}
	}

	c.searchOrder = append(c.searchOrder, thirdParty...)
	c.searchOrder = append(c.searchOrder, stdLib...)

	msg := fmt.Sprintf("cache package for %s successfully!", c.rootDir)
	c.conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: msg})
}

func (c *GlobalCache) Search(visit func(p *packages.Package) error) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	seen := map[string]bool{}

	for _, f := range c.view.files {
		filename, _ := f.URI.Filename()
		if !strings.HasPrefix(filename, c.rootDir) {
			continue
		}

		if !c.view.HasParsed(f.URI) {
			continue
		}

		pkg, _ := f.GetPackage()
		if pkg == nil || seen[pkg.PkgPath] {
			continue
		}

		seen[pkg.PkgPath] = true

		//fmt.Printf("visit view package %s\n", pkg.PkgPath)
		if err := visit(pkg); err != nil {
			return err
		}
	}

	for _, pkgPath := range c.searchOrder {
		if seen[pkgPath] {
			continue
		}

		seen[pkgPath] = true

		pkg := c.pathMap[pkgPath]
		if pkg == nil {
			continue
		}

		//fmt.Printf("visit package %s\n", pkg.PkgPath)
		if err := visit(pkg); err != nil {
			return err
		}

		if strings.HasSuffix(pkgPath, ".test") || strings.HasSuffix(pkgPath, "_test") {
			pkg = pkg.Imports[pkgPath[:len(pkgPath) - len(".test")]]
			if pkg == nil {
				continue
			}

			seen[pkg.PkgPath] = false
			//fmt.Printf("visit xtest import package %s\n", pkg.PkgPath)
			if err := visit(pkg); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *GlobalCache) cache(ctx context.Context, pkg *packages.Package) {
	if _, ok := c.pathMap[pkg.PkgPath]; ok {
		return
	}

	c.pathMap[pkg.PkgPath] = pkg

	msg := fmt.Sprintf("cached package %s", pkg.PkgPath)
	c.conn.Notify(ctx, "window/logMessage", &lsp.LogMessageParams{Type: lsp.Info, Message: msg})
	for _, importPkg := range pkg.Imports {
		c.cache(ctx, importPkg)
	}
}
