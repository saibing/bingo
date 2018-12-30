package cache

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages"
)

const (
	gomod     = "go.mod"
	vendor    = "vendor"
	gopathEnv = "GOPATH"
)

type path2Package map[string]*packages.Package

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindPackageFunc func(project *Project, pkgDir, importPath string) (*packages.Package, error)

type Project struct {
	conn         jsonrpc2.JSONRPC2
	rootDir      string
	vendorDir    string
	goroot       string
	view         *View
	gomoduleMode bool
	modules      []*module
}

func NewProject() *Project {
	return &Project{goroot: getGoRoot()}
}

func getGoRoot() string {
	root := runtime.GOROOT()
	root = filepath.Join(root, "src")
	return util.LowerDriver(root)
}

func (p *Project) Init(ctx context.Context, conn jsonrpc2.JSONRPC2, root string, view *View) error {
	packages.DebugCache = false

	start := time.Now()
	p.conn = conn
	p.rootDir = util.LowerDriver(root)
	p.vendorDir = filepath.Join(p.rootDir, vendor)
	p.view = view
	p.view.getLoadDir = p.getLoadDir

	gomodList, err := p.findGoModFiles()
	if err != nil {
		p.notifyError(err.Error())
		return err
	}

	p.gomoduleMode = len(gomodList) > 0
	if p.gomoduleMode {
		err = p.createGoModuleProject(gomodList)
	} else {
		err = p.createGoPathProject()
	}

	if err != nil {
		p.notifyError(err.Error())
		return err
	}

	elapsedTime := time.Since(start) / time.Second

	p.notifyInfo(fmt.Sprintf("cache package for %s successfully! elapsed time: %d seconds", p.rootDir, elapsedTime))
	return p.fsNotify()
}

// BuiltinPkg builtin package
const BuiltinPkg = "builtin"

func (p *Project) GetBuiltinPackage() *packages.Package {
	return p.GetFromPkgPath("", BuiltinPkg)
}

func (p *Project) createGoModuleProject(gomodList []string) error {
	err := p.createBuiltinModule()
	if err != nil {
		return err
	}

	for _, v := range gomodList {
		module := newModule(p, util.LowerDriver(filepath.Dir(v)))
		err = module.init()
		if err != nil {
			return err
		}

		p.modules = append(p.modules, module)
	}

	sort.Slice(p.modules, func(i, j int) bool {
		return p.modules[i].rootDir >= p.modules[j].rootDir
	})

	return nil
}

func (p *Project) createGoPathProject() error {
	err := p.createBuiltinModule()
	if err != nil {
		return err
	}

	cache := newModule(p, p.rootDir)
	err = cache.init()
	if err != nil {
		return err
	}

	p.modules = append(p.modules, cache)
	return nil
}

func (p *Project) createBuiltinModule() error {
	value := os.Getenv("GO111MODULE")
	_ = os.Setenv("GO111MODULE", "auto")
	defer func() {
		_ = os.Setenv("GO111MODULE", value)
	}()
	module := newModule(p, filepath.Join(p.goroot, BuiltinPkg))
	err := module.init()
	if err != nil {
		return err
	}

	p.modules = append(p.modules, module)
	return nil
}

func (p *Project) findGoModFiles() ([]string, error) {
	var gomodList []string
	walkFunc := func(path string, name string) {
		if name == gomod {
			gomodList = append(gomodList, filepath.Join(path, name))
		}
	}

	err := p.walkDir(p.rootDir, 0, walkFunc)
	return gomodList, err
}

func (p *Project) walkDir(rootDir string, level int, walkFunc func(string, string)) error {
	if level > 3 {
		return nil
	}

	if strings.HasPrefix(rootDir, p.vendorDir) {
		return nil
	}

	files, err := ioutil.ReadDir(rootDir)
	if err != nil {
		p.notifyLog(err.Error())
		return nil
	}

	for _, fi := range files {
		if fi.IsDir() {
			level++
			err = p.walkDir(filepath.Join(rootDir, fi.Name()), level, walkFunc)
			if err != nil {
				return err
			}
			level--
		} else {
			walkFunc(rootDir, fi.Name())
		}
	}

	return nil
}

func (p *Project) fsNotify() error {
	if p.gomoduleMode {
		return p.fsNotifyModule()
	}
	return p.fsNotifyVendor()
}

func (p *Project) fsNotifyModule() error {
	var paths []string
	for _, v := range p.modules {
		if v.rootDir == filepath.Join(p.goroot, BuiltinPkg) {
			continue
		}
		paths = append(paths, filepath.Join(v.rootDir, gomod))
	}

	return p.fsNotifyPaths(paths)
}

func (p *Project) fsNotifyVendor() error {
	_, err := os.Stat(p.vendorDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return p.fsNotifyPaths([]string{p.vendorDir})
}

func (p *Project) fsNotifyPaths(paths []string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	for _, p := range paths {
		err = watcher.Add(p)
		if err != nil {
			_ = watcher.Close()
			return err
		}
	}

	go func() {
		defer func() {
			_ = watcher.Close()
		}()

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Write == fsnotify.Write {
					p.rebuildCache(event.Name)

				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				p.notifyError(fmt.Sprintf("receive an fsNotify error: %s", err))
			}
		}
	}()

	return nil
}

func (p *Project) GetFromURI(uri lsp.DocumentURI) *packages.Package {
	filename, _ := source.FromDocumentURI(uri).Filename()
	return p.view.Config.Cache.GetByURI(filename)
}

func (p *Project) GetFromPkgPath(_ string, pkgPath string) *packages.Package {
	return p.view.Config.Cache.Get(pkgPath)
}

func (p *Project) getLoadDir(filename string) string {
	if len(p.modules) == 1 {
		return p.modules[0].rootDir
	}

	for _, v := range p.modules {
		if strings.HasPrefix(filename, v.rootDir) {
			return v.rootDir
		}
	}

	for _, v := range p.modules {
		for k := range v.moduleMap {
			if strings.HasPrefix(filename, k) {
				return k
			}
		}
	}

	return p.rootDir
}

func (p *Project) rebuildCache(eventName string) {
	for _, v := range p.modules {
		if v.rootDir == filepath.Dir(eventName) {
			rebuild, err := v.rebuildCache()
			if err != nil {
				p.notifyError(err.Error())
				return
			}

			if rebuild {
				p.notifyInfo(fmt.Sprintf("rebuile module cache for %s changed", eventName))
			}

			return
		}
	}
}

func (p *Project) notifyError(message string) {
	_ = p.conn.Notify(context.Background(), "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: message})
}

func (p *Project) notifyInfo(message string) {
	_ = p.conn.Notify(context.Background(), "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: message})
}

func (p *Project) notifyLog(message string) {
	_ = p.conn.Notify(context.Background(), "window/logMessage", &lsp.LogMessageParams{Type: lsp.Info, Message: message})
}

func (p *Project) Search(walkFunc packages.WalkFunc) error {
	var ranks []string
	for _, cache := range p.modules {
		ranks = append(ranks, cache.rootDir)
	}

	ranks = append(ranks, p.goroot)
	return p.view.Config.Cache.Walk(walkFunc, ranks)
}
