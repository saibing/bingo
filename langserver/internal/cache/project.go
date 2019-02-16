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

	lsp "github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages"
)

const (
	goext           = ".go"
	gomod           = "go.mod"
	vendor          = "vendor"
	gopathEnv       = "GOPATH"
	go111module     = "GO111MODULE"
	emacsLockPrefix = ".#"
)

var (
	goroot  = getGoRoot()
	gopaths = getGoPaths()
)

func getGoRoot() string {
	root := runtime.GOROOT()
	root = filepath.ToSlash(filepath.Join(root, "src"))
	return util.LowerDriver(root)
}

func getGoPaths() []string {
	gopath := os.Getenv(gopathEnv)
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}

	paths := strings.Split(gopath, string(os.PathListSeparator))
	return paths
}

func isFileInsideGomod(path string) bool {
	gomodpath := filepath.Join(gopaths[0], "pkg", "mod")
	return strings.HasPrefix(path, gomodpath)
}

// FindPackageFunc matches the signature of loader.Config.FindPackage, except
// also takes a context.Context.
type FindPackageFunc func(project *Project, importPath string) (*packages.Package, error)

// Project project struct
type Project struct {
	context       context.Context
	conn          jsonrpc2.JSONRPC2
	view          *View
	rootDir       string
	vendorDir     string
	modules       []*module
	gopath        *gopath
	cached        bool
	lastBuildTime time.Time
}

// NewProject new project
func NewProject(conn jsonrpc2.JSONRPC2, rootDir string, view *View) *Project {
	p := &Project{
		conn:    conn,
		view:    view,
		rootDir: util.LowerDriver(rootDir),
	}

	p.vendorDir = filepath.Join(p.rootDir, vendor)
	return p
}

func (p *Project) notify(err error) {
	if err != nil {
		p.notifyLog(fmt.Sprintf("notify: %s\n", err))
	}
}

// Init init project
func (p *Project) Init(ctx context.Context, globalCacheStyle string) error {
	p.context = ctx
	start := time.Now()
	defer func() {
		elapsedTime := time.Since(start) / time.Second
		p.notifyInfo(fmt.Sprintf("load %s successfully! elapsed time: %d seconds, cache: %t, go module: %t.",
			p.rootDir, elapsedTime, p.cached, len(p.modules) > 0))
	}()

	if globalCacheStyle == "none" {
		return nil
	}

	p.view.cache = NewCache()
	err := p.createBuiltin()
	if err != nil {
		p.notify(err)
	}

	if globalCacheStyle != "always" {
		return nil
	}

	err = p.createProject()
	p.notify(err)
	p.lastBuildTime = time.Now()

	p.fsnotify()
	return nil
}

func (p *Project) fsnotify() {
	if !p.cached {
		return
	}

	subject := newSubject(p)
	go subject.notify()
}

func (p *Project) getImportPath() string {
	for _, path := range gopaths {
		path = util.LowerDriver(filepath.ToSlash(path))
		srcDir := filepath.Join(path, "src")
		if strings.HasPrefix(p.rootDir, srcDir) && p.rootDir != srcDir {
			return filepath.ToSlash(p.rootDir[len(srcDir)+1:])
		}
	}

	return ""
}

func (p *Project) isUnderGoroot() bool {
	return strings.HasPrefix(p.rootDir, goroot)
}

var siteLenMap = map[string]int{
	"github.com": 3,
	"golang.org": 3,
	"gopkg.in":   2,
}

func (p *Project) createProject() error {
	value := os.Getenv(go111module)

	if value == "on" {
		p.notifyLog("GO111MODULE=on, module mode")
		gomodList := p.findGoModFiles()
		return p.createGoModule(gomodList)
	}

	if p.isUnderGoroot() {
		p.notifyLog(fmt.Sprintf("%s under go root dir %s", p.rootDir, goroot))
		return p.createGoPath("", true)
	}

	importPath := p.getImportPath()
	p.notifyLog(fmt.Sprintf("GOPATH: %v, import path: %s", gopaths, importPath))
	if (value == "" || value == "auto") && importPath == "" {
		p.notifyLog("GO111MODULE=auto, module mode")
		gomodList := p.findGoModFiles()
		return p.createGoModule(gomodList)
	}

	if importPath == "" {
		return fmt.Errorf("%s is out of GOPATH workspace %v", p.rootDir, gopaths)
	}

	dirs := strings.Split(importPath, "/")
	siteLen := siteLenMap[dirs[0]]

	if len(dirs) < siteLen {
		return fmt.Errorf("%s is not correct root dir of project.", p.rootDir)
	}

	p.notifyLog("GOPATH mode")
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

	bulitin := newGopath(p, filepath.ToSlash(filepath.Join(goroot, BuiltinPkg)), "", true)
	return bulitin.init()
}

func (p *Project) findGoModFiles() []string {
	var gomodList []string
	walkFunc := func(path string, name string) {
		if name == gomod {
			fullpath := filepath.Join(path, name)
			gomodList = append(gomodList, fullpath)
			p.notifyLog(fullpath)
		}
	}

	err := p.walkDir(p.rootDir, 0, walkFunc)
	p.notify(err)
	return gomodList
}

var defaultExcludeDir = []string{".git", ".svn", ".hg", ".vscode", ".idea", vendor}

func isExclude(dir string) bool {
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
		if isExclude(fi.Name()) {
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

// GetFromURI get package from document uri.
func (p *Project) GetFromURI(uri lsp.DocumentURI) *packages.Package {
	filename, _ := source.FromDocumentURI(uri).Filename()
	return p.view.cache.GetByURI(filename)
}

// GetFromPkgPath get package from package import path.
func (p *Project) GetFromPkgPath(pkgPath string) *packages.Package {
	return p.view.cache.Get(pkgPath)
}

func (p *Project) update(eventName string) {
	if p.needRebuild(eventName) {
		p.notifyLog("fsnotify " + eventName)
		p.rebuildGopapthCache(eventName)
		p.rebuildModuleCache(eventName)
		p.lastBuildTime = time.Now()
	}
}

func (p *Project) needRebuild(eventName string) bool {
	if strings.HasSuffix(eventName, gomod) {
		return true
	}

	if strings.HasPrefix(eventName, emacsLockPrefix) {
		return false
	}

	if !strings.HasSuffix(eventName, goext) {
		return false
	}

	return time.Now().Sub(p.lastBuildTime) >= 60*time.Second
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
				p.notifyError(err.Error())
				return
			}

			if rebuild {
				p.notifyInfo(fmt.Sprintf("rebuild module cache for %s changed", eventName))
			}

			return
		}
	}
}

// NotifyError notify error to lsp client
func (p *Project) notifyError(message string) {
	_ = p.conn.Notify(p.context, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.MTError, Message: message})
}

// NotifyInfo notify info to lsp client
func (p *Project) notifyInfo(message string) {
	_ = p.conn.Notify(p.context, "window/showMessage", &lsp.ShowMessageParams{Type: lsp.Info, Message: message})
}

// NotifyLog notify log to lsp client
func (p *Project) notifyLog(message string) {
	_ = p.conn.Notify(p.context, "window/logMessage", &lsp.LogMessageParams{Type: lsp.Info, Message: message})
}

func (p *Project) root() string {
	return p.rootDir
}

func (p *Project) getContext() context.Context {
	return p.context
}

// Search serach package cache
func (p *Project) Search(walkFunc source.WalkFunc) error {
	var ranks []string
	for _, module := range p.modules {
		if module.mainModulePath == "." || module.mainModulePath == "" {
			continue
		}
		ranks = append(ranks, module.mainModulePath)
	}

	return p.view.cache.Walk(walkFunc, ranks)
}

func (p *Project) setCache(pkgs []*packages.Package) {
	seen := map[string]bool{}
	for _, pkg := range pkgs {
		p.setOnePackage(pkg, seen)
	}
}

func (p *Project) setOnePackage(pkg *packages.Package, seen map[string]bool) {
	if pkg == nil || len(pkg.Syntax) == 0 {
		return
	}

	if seen[pkg.ID] {
		return
	}
	seen[pkg.ID] = true

	p.view.cache.put(pkg)

	for _, ip := range pkg.Imports {
		p.setOnePackage(ip, seen)
	}
}

func (p *Project) Cache() *PackageCache {
	return p.view.cache
}

func (p *Project) TypeCheck(ctx context.Context, fileURI lsp.DocumentURI) (*packages.Package, source.File, error) {
	uri := source.FromDocumentURI(fileURI)

	p.view.mu.Lock()
	f := p.view.files[uri]
	p.view.mu.Unlock()

	if f == nil {
		pkg := p.GetFromURI(fileURI)
		if pkg != nil {
			return pkg, nil, nil
		}

		p.view.mu.Lock()
		f = p.view.getFile(uri)
		p.view.mu.Unlock()
	}

	pkg := f.GetPackage()
	if pkg == nil {
		return nil, nil, fmt.Errorf("package is null for file %s", uri)
	}

	return pkg, f, nil
}

func newSubject(observer Observer) Subject {
	return &fsSubject{observer: observer}
}
