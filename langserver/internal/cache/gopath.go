package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/saibing/bingo/langserver/internal/util"

	"golang.org/x/tools/go/packages"
)

type gopath struct {
	mu         sync.RWMutex
	project    *Project
	rootDir    string
	importPath string
	isGoroot   bool
}

func newGopath(project *Project, rootDir string) *gopath {
	return &gopath{project: project, rootDir: rootDir}
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
	if strings.HasPrefix(p.rootDir, p.project.goroot) {
		p.importPath = ""
		p.isGoroot = true
		return nil
	}

	gopath := os.Getenv(gopathEnv)
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}

	paths := strings.Split(gopath, string(os.PathListSeparator))

	for _, path := range paths {
		path = util.LowerDriver(filepath.ToSlash(path))
		if strings.HasPrefix(p.rootDir, path) && p.rootDir != path {
			srcDir := filepath.Join(path, "src")
			if p.rootDir == srcDir {
				continue
			}

			p.importPath = filepath.ToSlash(p.rootDir[len(srcDir)+1:])
			return nil
		}
	}

	return fmt.Errorf("%s is out of GOPATH workspace %v, go root is %s", p.rootDir, paths, p.project.goroot)
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
	if p.isGoroot {
		pattern = cfg.Dir
	} else {
		pattern = p.importPath + "/..."
	}

	return packages.Load(cfg, pattern)
}

