package cache

import (
	"sync"

	"golang.org/x/tools/go/packages"
)

type gopath struct {
	mu          sync.RWMutex
	project     *Project
	rootDir     string
	importPath  string
	underGoroot bool
}

func newGopath(project *Project, rootDir string, importPath string, underGoroot bool) *gopath {
	return &gopath{
		project:     project,
		rootDir:     rootDir,
		importPath:  importPath,
		underGoroot: underGoroot,
	}
}

func (p *gopath) init() (err error) {
	err = p.doInit()
	if err != nil {
		return err
	}

	_, err = p.buildCache()
	return err
}

func (p *gopath) doInit() error {
	return nil
}

func (p *gopath) rebuildCache() (bool, error) {
	_, err := p.buildCache()
	return err == nil, err
}

func (p *gopath) buildCache() ([]*packages.Package, error) {
	p.project.view.mu.Lock()
	defer p.project.view.mu.Unlock()

	cfg := p.project.view.Config
	cfg.Dir = p.rootDir
	cfg.ParseFile = nil

	var pattern string
	if p.underGoroot {
		pattern = cfg.Dir
	} else {
		pattern = p.importPath + "/..."
	}

	return packages.Load(cfg, pattern)
}
