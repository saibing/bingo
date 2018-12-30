package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/util"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

type module struct {
	mu             sync.RWMutex
	project        *Project
	rootDir        string
	mainModulePath string
	moduleMap      map[string]moduleInfo
}

func newModule(gc *Project, rootDir string) *module {
	return &module{project: gc, rootDir: rootDir}
}

func (m *module) init() (err error) {
	if m.project.gomoduleMode {
		err = m.initModuleProject()
	} else {
		err = m.initGoPathProject()
	}
	if err != nil {
		return err
	}

	_, err = m.buildCache()
	return err
}

func (m *module) initModuleProject() error {
	moduleMap, err := m.readModuleFromFile()
	if err != nil {
		return err
	}

	m.initModule(moduleMap)
	return nil
}

func (m *module) initGoPathProject() error {
	if strings.HasPrefix(m.rootDir, util.LowerDriver(filepath.ToSlash(m.project.goroot))) {
		m.mainModulePath = "."
		return nil
	}

	gopath := os.Getenv(gopathEnv)
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}

	paths := strings.Split(gopath, string(os.PathListSeparator))

	for _, path := range paths {
		p := util.LowerDriver(filepath.ToSlash(path))
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

func (m *module) readModuleFromFile() (map[string]moduleInfo, error) {
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
		moduleMap[util.LowerDriver(module.Dir)] = module
	}

	return moduleMap, nil
}

func (m *module) initModule(moduleMap map[string]moduleInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, module := range moduleMap {
		if module.Main {
			m.mainModulePath = module.Path
		}
	}

	m.moduleMap = moduleMap
}

func (m *module) checkModuleCache() (bool, error) {
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

func (m *module) rebuildCache() (bool, error) {
	if m.project.gomoduleMode {
		rebuild, err := m.checkModuleCache()
		if err != nil {
			return false, err
		}

		if !rebuild {
			return false, nil
		}
	}

	_, err := m.buildCache()
	return err == nil, err
}

func (m *module) hasChanged(moduleMap map[string]moduleInfo) bool {
	for dir := range moduleMap {
		// there are some new module add into go.mod
		if _, ok := m.moduleMap[dir]; !ok {
			return true
		}
	}

	return false
}

func (m *module) buildCache() ([]*packages.Package, error) {
	cfg := m.project.view.Config
	cfg.Dir = m.rootDir
	var pattern string
	if filepath.Join(m.project.goroot, BuiltinPkg) == m.rootDir {
		pattern = cfg.Dir
	} else if m.project.gomoduleMode {
		pattern = cfg.Dir + "/..."
	} else {
		pattern = m.mainModulePath + "/..."
	}

	return packages.Load(cfg, pattern)
}

