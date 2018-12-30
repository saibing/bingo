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
	caches       []*module
}

func NewProject() *Project {
	return &Project{goroot: getGoRoot()}
}

func getGoRoot() string {
	root := runtime.GOROOT()
	root = filepath.Join(root, "src")
	return util.LowerDriver(root)
}

func (gc *Project) Init(ctx context.Context, conn jsonrpc2.JSONRPC2, root string, view *View) error {
	packages.DebugCache = false

	start := time.Now()
	gc.conn = conn
	gc.rootDir = util.LowerDriver(root)
	gc.vendorDir = filepath.Join(gc.rootDir, vendor)
	gc.view = view
	gc.view.getLoadDir = gc.getLoadDir

	gomodList, err := gc.findGoModFiles()
	if err != nil {
		gc.notifyError(err.Error())
		return err
	}

	gc.gomoduleMode = len(gomodList) > 0
	if gc.gomoduleMode {
		err = gc.createGoModuleProject(gomodList)
	} else {
		err = gc.createGoPathProject()
	}

	if err != nil {
		gc.notifyError(err.Error())
		return err
	}

	elapsedTime := time.Since(start) / time.Second

	gc.notifyInfo(fmt.Sprintf("cache package for %s successfully! elapsed time: %d seconds", gc.rootDir, elapsedTime))
	return gc.fsNotify()
}

// BuiltinPkg builtin package
const BuiltinPkg = "builtin"

func (gc *Project) GetBuiltinPackage() *packages.Package {
	return gc.GetFromPkgPath("", BuiltinPkg)
}

func (gc *Project) createGoModuleProject(gomodList []string) error {
	err := gc.createBuiltinCache()
	if err != nil {
		return err
	}

	for _, v := range gomodList {
		cache := newModuleCache(gc, util.LowerDriver(filepath.Dir(v)))
		err = cache.init()
		if err != nil {
			return err
		}

		gc.caches = append(gc.caches, cache)
	}

	sort.Slice(gc.caches, func(i, j int) bool {
		return gc.caches[i].rootDir >= gc.caches[j].rootDir
	})

	return nil
}

func (gc *Project) createGoPathProject() error {
	err := gc.createBuiltinCache()
	if err != nil {
		return err
	}

	cache := newModuleCache(gc, gc.rootDir)
	err = cache.init()
	if err != nil {
		return err
	}

	gc.caches = append(gc.caches, cache)
	return nil
}

func (gc *Project) createBuiltinCache() error {
	value := os.Getenv("GO111MODULE")
	_ = os.Setenv("GO111MODULE", "auto")
	defer func() {
		_ = os.Setenv("GO111MODULE", value)
	}()
	cache := newModuleCache(gc, filepath.Join(gc.goroot, BuiltinPkg))
	err := cache.init()
	if err != nil {
		return err
	}

	gc.caches = append(gc.caches, cache)
	return nil
}

func (gc *Project) findGoModFiles() ([]string, error) {
	var gomodList []string
	walkFunc := func(path string, name string) {
		if name == gomod {
			gomodList = append(gomodList, filepath.Join(path, name))
		}
	}

	err := gc.walkDir(gc.rootDir, 0, walkFunc)
	return gomodList, err
}

func (gc *Project) walkDir(rootDir string, level int, walkFunc func(string, string)) error {
	if level > 3 {
		return nil
	}

	if strings.HasPrefix(rootDir, gc.vendorDir) {
		return nil
	}

	files, err := ioutil.ReadDir(rootDir)
	if err != nil {
		return err
	}

	for _, fi := range files {
		if fi.IsDir() {
			level++
			err = gc.walkDir(filepath.Join(rootDir, fi.Name()), level, walkFunc)
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

func (gc *Project) fsNotify() error {
	if gc.gomoduleMode {
		return gc.fsNotifyModule()
	}
	return gc.fsNotifyVendor()
}

func (gc *Project) fsNotifyModule() error {
	var paths []string
	for _, v := range gc.caches {
		if v.rootDir == filepath.Join(gc.goroot, BuiltinPkg) {
			continue
		}
		paths = append(paths, filepath.Join(v.rootDir, gomod))
	}

	return gc.fsNotifyPaths(paths)
}

func (gc *Project) fsNotifyVendor() error {
	_, err := os.Stat(gc.vendorDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return gc.fsNotifyPaths([]string{gc.vendorDir})
}

func (gc *Project) fsNotifyPaths(paths []string) error {
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
					gc.rebuildCache(event.Name)

				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				gc.notifyError(fmt.Sprintf("receive an fsNotify error: %s", err))
			}
		}
	}()

	return nil
}

func (gc *Project) GetFromURI(uri lsp.DocumentURI) *packages.Package {
	filename, _ := source.FromDocumentURI(uri).Filename()
	return gc.view.Config.Cache.GetByURI(filename)
}

func (gc *Project) GetFromPkgPath(_ string, pkgPath string) *packages.Package {
	return gc.view.Config.Cache.Get(pkgPath)
}

func (gc *Project) getLoadDir(filename string) string {
	if len(gc.caches) == 1 {
		return gc.caches[0].rootDir
	}

	for _, v := range gc.caches {
		if strings.HasPrefix(filename, v.rootDir) {
			return v.rootDir
		}
	}

	for _, v := range gc.caches {
		for k := range v.moduleMap {
			if strings.HasPrefix(filename, k) {
				return k
			}
		}
	}

	return gc.rootDir
}

func (gc *Project) rebuildCache(eventName string) {
	for _, v := range gc.caches {
		if v.rootDir == filepath.Dir(eventName) {
			rebuild, err := v.rebuildCache()
			if err != nil {
				gc.notifyError(err.Error())
				return
			}

			if rebuild {
				gc.notifyInfo(fmt.Sprintf("rebuile module cache for %s changed", eventName))
			}

			return
		}
	}
}

func (gc *Project) notifyError(message string) {
	_ = gc.conn.Notify(context.Background(), "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: message})
}

func (gc *Project) notifyInfo(message string) {
	_ = gc.conn.Notify(context.Background(), "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: message})
}

func (gc *Project) notifyLog(message string) {
	_ = gc.conn.Notify(context.Background(), "window/logMessage", &lsp.LogMessageParams{Type: lsp.Info, Message: message})
}

func (gc *Project) Search(walkFunc packages.WalkFunc) error {
	var ranks []string
	for _, cache := range gc.caches {
		ranks = append(ranks, cache.rootDir)
	}

	ranks = append(ranks, gc.goroot)
	return gc.view.Config.Cache.Walk(walkFunc, ranks)
}
