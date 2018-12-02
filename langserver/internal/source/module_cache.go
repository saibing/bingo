package source

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/saibing/bingo/langserver/internal/sys"
	"github.com/saibing/bingo/pkg/lsp"
	"golang.org/x/tools/go/packages"
)

type moduleInfo struct {
	Path     string    `json:"Path"`
	Main     bool      `json:"Main"`
	Dir      string    `json:"Dir"`
	GoMod    string    `json:"GoMod"`
	Version  string    `json:"Version"`
	Time     time.Time `json:"Time"`
	Indirect bool      `json:"Indirect"`
}

type moduleCache struct {
	mu             sync.RWMutex
	gc             *GlobalCache
	rootDir        string
	pathMap        path2Package
	workspacePkg   []string
	modulePkg      []string
	stdLibPkg      []string
	mainModulePath string

	moduleMap map[string]moduleInfo
}

func newModuleCache(gc *GlobalCache, rootDir string) *moduleCache {
	return &moduleCache{gc: gc, rootDir: rootDir}
}

func lowerDriver(path string) string {
	if !sys.IsWindows() {
		return path
	}

	return strings.ToLower(path[0:1]) + path[1:]
}

func (m *moduleCache) init() (err error) {
	if m.gc.gomoduleMode {
		err = m.initModuleProject()
	} else {
		err = m.initGoPathProject()
	}
	if err != nil {
		return err
	}

	pkgList, err := m.buildCache()
	if err != nil {
		return err
	}
	m.setCache(pkgList)
	return nil
}

func (m *moduleCache) initModuleProject() error {
	moduleMap, err := m.readModuleFromFile()
	if err != nil {
		return err
	}

	m.initModule(moduleMap)
	return nil
}

func (m *moduleCache) initGoPathProject() error {
	if strings.HasPrefix(m.rootDir, m.gc.goroot) {
		m.mainModulePath = ""
		return nil
	}

	gopath := os.Getenv(gopathEnv)
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}

	paths := strings.Split(gopath, string(os.PathListSeparator))

	for _, path := range paths {
		p := lowerDriver(filepath.ToSlash(path))
		if strings.HasPrefix(m.rootDir, p) && m.rootDir != p {
			srcDir := filepath.Join(p, "src")
			if m.rootDir == srcDir {
				continue
			}

			m.mainModulePath = filepath.ToSlash(m.rootDir[len(srcDir)+1:])
			return nil
		}
	}

	return fmt.Errorf("%s is out of GOPATH workspace %v, but not a go module project", m.rootDir, paths)
}

func (m *moduleCache) readModuleFromFile() (map[string]moduleInfo, error) {
	buf, err := invokeGo(context.Background(), m.rootDir, "list", "-m", "-json", "all")
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

func (m *moduleCache) getFromPackagePath(pkgPath string) *packages.Package {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pathMap[pkgPath]
}

func (m *moduleCache) getPackagePath(filename string) (pkgPath string, testFile bool) {
	dir := lowerDriver(filepath.Dir(filename))
	base := filepath.Base(filename)

	if strings.HasPrefix(dir, m.gc.goroot) {
		pkgPath = dir[len(m.gc.goroot)+1:]
	} else {
		for k, v := range m.moduleMap {
			if strings.HasPrefix(dir, k) {
				pkgPath = filepath.Join(v.Path, dir[len(k):])
				break
			}
		}
	}

	pkgPath = filepath.ToSlash(pkgPath)

	if strings.HasSuffix(base, "_test.go") {
		testFile = true
	}
	return pkgPath, testFile
}

func (m *moduleCache) getFromURI(uri lsp.DocumentURI) *packages.Package {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sourceURI := FromDocumentURI(uri)
	filename, _ := sourceURI.Filename()
	pkgPath, testFile := m.getPackagePath(filename)
	if testFile {
		file := m.gc.view.GetFile(sourceURI)
		content, err := file.Read()
		if err != nil {
			panic(err)
		}

		fSet := token.NewFileSet()
		astFile, err := parser.ParseFile(fSet, filename, content, parser.PackageClauseOnly)
		if err != nil {
			panic(err)
		}

		if strings.HasSuffix(astFile.Name.Name, "_test") {
			return m.pathMap[pkgPath+"_test"]
		}

		return m.pathMap[pkgPath+".test"]
	}
	return m.pathMap[pkgPath]
}

func (m *moduleCache) initModule(moduleMap map[string]moduleInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, module := range moduleMap {
		if module.Main {
			m.mainModulePath = module.Path
		}
	}

	m.moduleMap = moduleMap
}

func (m *moduleCache) checkModuleCache() (bool, error) {
	moduleMap, err := m.readModuleFromFile()
	if err != nil {
		return false, err
	}

	if !m.hasChanged(moduleMap) {
		return false, nil
	}

	m.initModule(moduleMap)
	return true, nil
}

func (m *moduleCache) rebuildCache() (bool, error) {
	if m.gc.gomoduleMode {
		rebuild, err := m.checkModuleCache()
		if err != nil {
			return false, err
		}

		if !rebuild {
			return false, nil
		}
	}

	pkgList, err := m.buildCache()
	if err != nil {
		return true, err
	}

	m.setCache(pkgList)
	return true, nil
}

func (m *moduleCache) hasChanged(moduleMap map[string]moduleInfo) bool {
	for dir := range moduleMap {
		// there are some new module add into go.mod
		if _, ok := m.moduleMap[dir]; !ok {
			return true
		}
	}

	return false
}

func (m *moduleCache) buildCache() ([]*packages.Package, error) {
	cfg := *m.gc.view.Config
	cfg.Dir = m.rootDir
	cfg.Mode = packages.LoadAllSyntax
	cfg.Fset = m.gc.view.Config.Fset

	pattern := m.mainModulePath + "/..."
	if m.gc.gomoduleMode {
		pattern = cfg.Dir + "/..."
	}
	return packages.Load(&cfg, pattern)
}

func (m *moduleCache) setCache(pkgList []*packages.Package) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pathMap = path2Package{}
	m.workspacePkg = []string{}
	m.modulePkg = []string{}
	m.stdLibPkg = []string{}

	for _, pkg := range pkgList {
		m.cache(pkg)
	}
}

func (m *moduleCache) cache(pkg *packages.Package) {
	if _, ok := m.pathMap[pkg.PkgPath]; ok {
		return
	}

	if strings.HasPrefix(pkg.PkgPath, m.mainModulePath) {
		m.workspacePkg = append(m.workspacePkg, pkg.PkgPath)
	} else if strings.Contains(pkg.PkgPath, ".") {
		m.modulePkg = append(m.modulePkg, pkg.PkgPath)
	} else {
		m.stdLibPkg = append(m.stdLibPkg, pkg.PkgPath)
	}

	m.pathMap[pkg.PkgPath] = pkg
	m.gc.notifyLog(fmt.Sprintf("cached module %s's package %s", m.mainModulePath, pkg.PkgPath))
	for _, importPkg := range pkg.Imports {
		m.cache(importPkg)
	}
}

func (m *moduleCache) search(seen map[string]bool, visit func(p *packages.Package) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	visitPkgList := func(pkgList []string) error {
		for _, pkgPath := range pkgList {
			if seen[pkgPath] {
				continue
			}

			seen[pkgPath] = true

			pkg := m.pathMap[pkgPath]
			if pkg == nil {
				continue
			}

			if err := visit(pkg); err != nil {
				return err
			}

			if strings.HasSuffix(pkgPath, ".test") || strings.HasSuffix(pkgPath, "_test") {
				pkg = pkg.Imports[pkgPath[:len(pkgPath)-len(".test")]]
				if pkg == nil {
					continue
				}

				seen[pkg.PkgPath] = false
				if err := visit(pkg); err != nil {
					return err
				}
			}
		}

		return nil
	}

	err := visitPkgList(m.workspacePkg)
	if err != nil {
		return err
	}

	err = visitPkgList(m.modulePkg)
	if err != nil {
		return err
	}

	return visitPkgList(m.stdLibPkg)
}

