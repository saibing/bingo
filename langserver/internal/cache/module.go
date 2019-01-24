package cache

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/saibing/bingo/langserver/internal/util"

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
	err = m.doInit()
	if err != nil {
		return err
	}

	_, err = m.buildCache()
	return err
}

func (m *module) doInit() error {
	moduleMap, err := m.readGoModule()
	if err != nil {
		return err
	}

	m.initModule(moduleMap)
	return nil
}

func (m *module) readGoModule() (map[string]moduleInfo, error) {
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
	moduleMap, err := m.readGoModule()
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
	rebuild, err := m.checkModuleCache()
	if err != nil {
		return false, err
	}

	if !rebuild {
		return false, nil
	}

	_, err = m.buildCache()
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
	m.project.view.mu.Lock()
	defer m.project.view.mu.Unlock()

	cfg := m.project.view.Config
	cfg.Dir = m.rootDir
	cfg.ParseFile = nil
	pattern := cfg.Dir + "/..."

	return packages.Load(cfg, pattern)
}
