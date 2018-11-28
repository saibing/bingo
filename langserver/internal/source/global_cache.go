package source

import (
	"context"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type path2Package map[string]*packages.Package

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindPackageFunc func(globalCache *GlobalCache, pkgDir, importPath string) (*packages.Package, error)

type GlobalCache struct {
	conn    jsonrpc2.JSONRPC2
	rootDir string
	goroot  string
	view    *View
	caches  []*moduleCache
}

func NewGlobalCache() *GlobalCache {
	return &GlobalCache{goroot: getGoRoot()}
}

func getGoRoot() string {
	root := runtime.GOROOT()
	root = filepath.Join(root, "src")
	return lowerDriver(root)
}

func (gc *GlobalCache) Init(ctx context.Context, conn jsonrpc2.JSONRPC2, root string, view *View) error {
	gc.conn = conn
	gc.rootDir = root
	gc.view = view
	gc.view.Config.Dir = gc.rootDir

	gomodList, err := gc.findGoModFiles()
	if err != nil {
		return err
	}

	if len(gomodList) == 0 {
		return fmt.Errorf("there is no any go.mod file under %s", gc.rootDir)
	}

	for _, v := range gomodList {
		cache := newModuleCache(gc, filepath.Dir(v))
		err = cache.init()
		if err != nil {
			gc.conn.Notify(context.Background(), "window/showMessage",
				&lsp.ShowMessageParams{Type: lsp.MTError, Message: err.Error()})
			return err
		}

		gc.caches = append(gc.caches, cache)
	}

	sort.Slice(gc.caches, func(i, j int) bool {
		return gc.caches[i].gomodDir >= gc.caches[j].gomodDir
	})

	msg := fmt.Sprintf("cache package for %s successfully!", gc.rootDir)
	gc.conn.Notify(ctx, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: msg})

	return gc.fsNotify()
}

const gomod = "go.mod"

func (gc *GlobalCache) findGoModFiles() ([]string, error) {
	var gomodList []string
	find := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == gomod {
			gomodList = append(gomodList, path)
		}

		return nil
	}

	err := filepath.Walk(gc.rootDir, find)
	return gomodList, err
}

func (gc *GlobalCache) fsNotify() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	for _, v := range gc.caches {
		err = watcher.Add(filepath.Join(v.gomodDir, gomod))
		if err != nil {
			watcher.Close()
			return err
		}
	}

	go func() {
		defer watcher.Close()

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
				gc.conn.Notify(context.Background(), "window/showMessage",
					&lsp.ShowMessageParams{Type: lsp.MTError, Message: fmt.Sprintf("receive an fsNotify error: %s", err)})
			}
		}
	}()

	return nil
}

func (gc *GlobalCache) GetFromURI(uri lsp.DocumentURI) *packages.Package {
	filename, _ := FromDocumentURI(uri).Filename()

	for _, v := range gc.caches {
		if strings.HasPrefix(filename, v.gomodDir) {
			return v.getFromURI(uri)
		}
	}

	return nil
}

func (gc *GlobalCache) GetFromPackagePath(pkgDir string, pkgPath string) *packages.Package {
	for _, v := range gc.caches {
		if strings.HasPrefix(pkgDir, v.gomodDir) {
			return v.getFromPackagePath(pkgPath)
		}
	}

	return nil
}

func (gc *GlobalCache) rebuildCache(eventName string) {
	for _, v := range gc.caches {
		if v.gomodDir == filepath.Dir(eventName) {
			rebuild, err := v.rebuildCache()
			if err != nil {
				gc.conn.Notify(context.Background(), "window/showMessage",
					&lsp.ShowMessageParams{Type: lsp.MTError, Message: err.Error()})
			}

			if rebuild {
				gc.conn.Notify(context.Background(), "window/showMessage",
					&lsp.ShowMessageParams{Type: lsp.Info, Message: fmt.Sprintf("rebuile module cache for %s changed", eventName)})
			}

			break
		}
	}
}

func (gc *GlobalCache) Search(visit func(p *packages.Package) error) error {
	for _, v := range gc.caches {
		err := v.search(visit)
		if err != nil {
			return err
		}
	}

	return nil
}