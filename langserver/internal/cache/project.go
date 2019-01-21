package cache

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/fsnotify/fsnotify"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages"
)

const (
	goext       = ".go"
	gomod       = "go.mod"
	vendor      = "vendor"
	gopathEnv   = "GOPATH"
	go111module = "GO111MODULE"
)

type path2Package map[string]*packages.Package

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindPackageFunc func(project *Project, importPath string) (*packages.Package, error)

// Project project struct
type Project struct {
	conn      jsonrpc2.JSONRPC2
	view      *View
	rootDir   string
	vendorDir string
	goroot    string
	modules   []*module
	gopath    *gopath
	bulitin   *gopath
	cached    bool
	watched   int
}

// NewProject new project
func NewProject() *Project {
	return &Project{goroot: getGoRoot()}
}

func getGoRoot() string {
	root := runtime.GOROOT()
	root = filepath.ToSlash(filepath.Join(root, "src"))
	return util.LowerDriver(root)
}

func (p *Project) notify(err error) {
	if err != nil {
		p.NotifyLog(fmt.Sprintf("notify: %s\n", err))
	}
}

// Init init project
func (p *Project) Init(ctx context.Context, conn jsonrpc2.JSONRPC2, root string, view *View, golistDuration int) error {
	packages.DebugCache = false
	packages.ParseFileTrace = false
	packages.GolistTrace = false

	start := time.Now()
	p.conn = conn
	p.rootDir = util.LowerDriver(root)
	p.vendorDir = filepath.Join(p.rootDir, vendor)
	p.view = view
	p.view.getLoadDir = p.getLoadDir

	err := p.createBuiltin()
	if err != nil {
		return err
	}

	err = p.createProject()
	p.notify(err)

	elapsedTime := time.Since(start) / time.Second
	packages.StartMonitor(time.Duration(golistDuration) * time.Second)

	p.NotifyInfo(fmt.Sprintf("load %s successfully! elapsed time: %d seconds, cached: %t, go module: %t.",
		p.rootDir, elapsedTime, p.cached, len(p.modules) > 0))

	go p.fsnotify()
	return nil
}

func (p *Project) getImportPath() ([]string, string) {
	gopath := os.Getenv(gopathEnv)
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}

	paths := strings.Split(gopath, string(os.PathListSeparator))
	for _, path := range paths {
		path = util.LowerDriver(filepath.ToSlash(path))
		srcDir := filepath.Join(path, "src")
		if strings.HasPrefix(p.rootDir, srcDir) && p.rootDir != srcDir {
			return paths, filepath.ToSlash(p.rootDir[len(srcDir)+1:])
		}
	}

	return paths, ""
}

func (p *Project) isUnderGoroot() bool {
	return strings.HasPrefix(p.rootDir, p.goroot)
}

var siteLenMap = map[string]int{
	"github.com": 3,
	"golang.org": 3,
	"gopkg.in":   2,
}

func (p *Project) createProject() error {
	value := os.Getenv(go111module)

	if value == "on" {
		gomodList := p.findGoModFiles()
		return p.createGoModule(gomodList)
	}

	if p.isUnderGoroot() {
		p.NotifyLog(fmt.Sprintf("%s under go root dir %s", p.rootDir, p.goroot))
		return p.createGoPath("", true)
	}

	paths, importPath := p.getImportPath()
	p.NotifyLog(fmt.Sprintf("GOPATH: %v, import path: %s", paths, importPath))
	if (value == "" || value == "auto") && importPath == "" {
		gomodList := p.findGoModFiles()
		return p.createGoModule(gomodList)
	}

	if importPath == "" {
		return fmt.Errorf("%s is out of GOPATH workspace %v", p.rootDir, paths)
	}

	dirs := strings.Split(importPath, "/")
	siteLen := siteLenMap[dirs[0]]

	if len(dirs) < siteLen {
		return fmt.Errorf("%s is not correct root dir of project.", p.rootDir)
	}

	return p.createGoPath(importPath, false)
}

// BuiltinPkg builtin package
const BuiltinPkg = "builtin"

// GetBuiltinPackage get builtin package
func (p *Project) GetBuiltinPackage() *packages.Package {
	return p.GetFromPkgPath(BuiltinPkg)
}

func (p *Project) createGoModule(gomodList []string) error {
	for _, v := range gomodList {
		module := newModule(p, util.LowerDriver(filepath.Dir(v)))
		err := module.init()
		p.notify(err)
		p.modules = append(p.modules, module)
	}

	if len(p.modules) == 0 {
		return nil
	}

	p.cached = true
	sort.Slice(p.modules, func(i, j int) bool {
		return p.modules[i].rootDir >= p.modules[j].rootDir
	})

	return nil
}

func (p *Project) createGoPath(importPath string, underGoroot bool) error {
	gopath := newGopath(p, p.rootDir, importPath, underGoroot)
	err := gopath.init()
	p.cached = err == nil
	return err
}

func (p *Project) createBuiltin() error {
	value := os.Getenv(go111module)

	if value == "on" {
		_ = os.Setenv(go111module, "auto")
		defer func() {
			_ = os.Setenv(go111module, value)
		}()
	}

	p.bulitin = newGopath(p, filepath.ToSlash(filepath.Join(p.goroot, BuiltinPkg)), "", true)
	return p.bulitin.init()
}

func (p *Project) findGoModFiles() []string {
	var gomodList []string
	walkFunc := func(path string, name string) {
		if name == gomod {
			fullpath := filepath.Join(path, name)
			gomodList = append(gomodList, fullpath)
			p.NotifyLog(fullpath)
		}
	}

	err := p.walkDir(p.rootDir, 0, walkFunc)
	p.notify(err)
	return gomodList
}

var defaultExcludeDir = []string{".git", ".svn", ".hg", ".vscode", ".idea", vendor}

func (p *Project) isExclude(dir string) bool {
	for _, d := range defaultExcludeDir {
		if d == dir {
			return true
		}
	}

	return false
}

func (p *Project) walkDir(rootDir string, level int, walkFunc func(string, string)) error {
	if level > 8 {
		return nil
	}

	files, err := ioutil.ReadDir(rootDir)
	if err != nil {
		p.notify(err)
		return nil
	}

	for _, fi := range files {
		if p.isExclude(fi.Name()) {
			continue
		}

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

func (p *Project) fsnotify() {
	if !p.cached {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		p.notify(err)
		return
	}

	p.watched = 0

	p.watch(p.rootDir, watcher)

	p.NotifyLog(fmt.Sprintf("fsnotify watch dir number: %d", p.watched))

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
				p.NotifyError(fmt.Sprintf("receive an fsNotify error: %s", err))
			}
		}
	}()
}

func (p *Project) watch(rootDir string, watcher *fsnotify.Watcher) {
	err := watcher.Add(rootDir)
	p.notify(err)
	if err == nil {
		p.watched++
	}
	//p.NotifyLog(fmt.Sprintf("watch %s", rootDir))

	files, err := ioutil.ReadDir(rootDir)
	if err != nil {
		p.notify(err)
		return
	}

	for _, fi := range files {
		if p.isExclude(fi.Name()) {
			continue
		}

		fullpath := filepath.Join(rootDir, fi.Name())
		if fi.IsDir() {
			p.watch(fullpath, watcher)
		} else {
			// if p.needWatch(fi.Name()) {
			// 	err = watcher.Add(fullpath)
			// 	p.notify(err)
			// }
		}
	}
}

func (p *Project) needWatch(filename string) bool {
	if strings.HasSuffix(filename, goext) {
		return true
	}
	return filename == gomod
}

// GetFromURI get package from document uri.
func (p *Project) GetFromURI(uri lsp.DocumentURI) *packages.Package {
	filename, _ := source.FromDocumentURI(uri).Filename()
	return p.view.Config.Cache.GetByURI(filename)
}

// GetFromPkgPath get package from package import path.
func (p *Project) GetFromPkgPath(pkgPath string) *packages.Package {
	return p.view.Config.Cache.Get(pkgPath)
}

func (p *Project) getLoadDir(filename string) string {
	if len(p.modules) == 1 {
		return p.modules[0].rootDir
	}

	for _, m := range p.modules {
		if strings.HasPrefix(filename, m.rootDir) {
			return m.rootDir
		}
	}

	for _, m := range p.modules {
		for k := range m.moduleMap {
			if strings.HasPrefix(filename, k) {
				return k
			}
		}
	}

	return p.rootDir
}

func (p *Project) rebuildCache(eventName string) {
	if p.needRebuild(eventName) {
		p.NotifyLog("fsnotify " + eventName)
		packages.CleanListCache()
		p.rebuildGopapthCache(eventName)
		p.rebuildModuleCache(eventName)
	}
}

func (p *Project) needRebuild(eventName string) bool {
	if strings.HasSuffix(eventName, gomod) {
		return true
	}

	return false

	// if !strings.HasSuffix(eventName, goext) {
	// 	return false
	// }

	// p.view.mu.Lock()
	// defer p.view.mu.Unlock()

	// uri := source.ToURI(util.LowerDriver(eventName))
	// return p.view.files[uri] == nil
}

func (p *Project) rebuildGopapthCache(eventName string) {
	if p.gopath == nil {
		return
	}

	if strings.HasSuffix(eventName, p.gopath.rootDir) {
		p.gopath.rebuildCache()
	}
}

func (p *Project) rebuildModuleCache(eventName string) {
	if len(p.modules) == 0 {
		return
	}

	for _, m := range p.modules {
		if strings.HasPrefix(filepath.Dir(eventName), m.rootDir) {
			rebuild, err := m.rebuildCache()
			if err != nil {
				p.NotifyError(err.Error())
				return
			}

			if rebuild {
				p.NotifyInfo(fmt.Sprintf("rebuild module cache for %s changed", eventName))
			}

			return
		}
	}
}

// NotifyError notify error to lsp client
func (p *Project) NotifyError(message string) {
	_ = p.conn.Notify(context.Background(), "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: message})
}

// NotifyInfo notify info to lsp client
func (p *Project) NotifyInfo(message string) {
	_ = p.conn.Notify(context.Background(), "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: message})
}

// NotifyLog notify log to lsp client
func (p *Project) NotifyLog(message string) {
	_ = p.conn.Notify(context.Background(), "window/logMessage", &lsp.LogMessageParams{Type: lsp.Info, Message: message})
}

// Search serach package cache
func (p *Project) Search(walkFunc packages.WalkFunc) error {
	var ranks []string
	for _, module := range p.modules {
		if module.mainModulePath == "." || module.mainModulePath == "" {
			continue
		}
		ranks = append(ranks, module.mainModulePath)
	}

	return p.view.Config.Cache.Walk(walkFunc, ranks)
}
