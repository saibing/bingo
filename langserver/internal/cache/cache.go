package cache

import (
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"golang.org/x/tools/go/packages"
)

type CacheStyle string

const (
	None     CacheStyle = "none"
	Ondemand CacheStyle = "on-demand"
	Always   CacheStyle = "always"
)

type Package struct {
	pkg     *packages.Package
	modTime time.Time
}

func (p *Package) Package() *packages.Package {
	if p == nil {
		return nil
	}
	return p.pkg
}

func (p *Package) ModTime() time.Time {
	if p == nil {
		return time.Time{}
	}
	return p.modTime
}

type id2Package map[string]*Package
type file2Package map[string]*Package
type path2Package map[string]*Package

func getPackageModTime(pkg *packages.Package) time.Time {
	if pkg == nil || len(pkg.CompiledGoFiles) == 0 {
		return time.Time{}
	}

	dir := pkg.CompiledGoFiles[0]
	fi, err := os.Stat(dir)
	if err != nil {
		return time.Time{}
	}

	return fi.ModTime()
}

// PackageCache package cache
type GlobalCache struct {
	mu      sync.RWMutex
	idMap   id2Package
	pathMap path2Package
	fileMap file2Package
}

// debugCache trace package cache
var debugCache = false

// NewCache new a package cache
func NewCache() *GlobalCache {
	return &GlobalCache{idMap: id2Package{}, pathMap: path2Package{}, fileMap: file2Package{}}
}

func (c *GlobalCache) put(pkg *packages.Package) {
	if c == nil {
		return
	}

	if debugCache {
		log.Printf("cache %s = %p\n", pkg.ID, pkg)
	}

	c.delete(pkg.ID)
	p := &Package{pkg: pkg, modTime: getPackageModTime(pkg)}
	c.idMap[pkg.ID] = p
	c.pathMap[pkg.PkgPath] = p

	for _, file := range pkg.CompiledGoFiles {
		c.fileMap[util.LowerDriver(file)] = p
	}
}

func (c *GlobalCache) get(id string) *Package {
	if c == nil {
		return nil
	}

	pkg := c.idMap[id]

	if debugCache {
		log.Printf("get %s = %p\n", id, pkg)
	}
	return pkg
}

func (c *GlobalCache) delete(id string) {
	if c == nil {
		return
	}

	if debugCache {
		log.Printf("delete %s %p\n", id, c.idMap[id])
	}

	p := c.idMap[id]
	if p == nil {
		return
	}

	delete(c.idMap, id)
	delete(c.pathMap, p.pkg.PkgPath)

	for _, file := range p.pkg.CompiledGoFiles {
		delete(c.fileMap, util.LowerDriver(file))
	}
}

func (c *GlobalCache) RLock() {
	if c == nil {
		return
	}

	c.mu.RLock()
}

func (c *GlobalCache) RUnlock() {
	if c == nil {
		return
	}

	c.mu.RUnlock()
}

func (c *GlobalCache) Lock() {
	if c == nil {
		return
	}

	c.mu.Lock()
}

func (c *GlobalCache) Unlock() {
	if c == nil {
		return
	}

	c.mu.Unlock()
}

func (c *GlobalCache) clean(idList []string) {
	if c == nil || len(idList) == 0 {
		return
	}

	c.Lock()
	defer c.Unlock()

	for _, id := range idList {
		c.delete(id)
	}
}

// Get get package by package import path from global cache
func (c *GlobalCache) Get(pkgPath string) *Package {
	if c == nil {
		return nil
	}

	c.RLock()
	p := c.pathMap[pkgPath]
	c.RUnlock()
	return p
}

func (c *GlobalCache) Put(pkg *packages.Package) {
	if c == nil {
		return
	}

	c.Lock()
	defer c.Unlock()
	c.put(pkg)
}

func (c *GlobalCache) Delete(id string) {
	if c == nil {
		return
	}

	c.Lock()
	defer c.Unlock()
	c.delete(id)
}

// GetByURI get package by filename from global cache
func (c *GlobalCache) GetByURI(filename string) *Package {
	if c == nil {
		return nil
	}
	c.RLock()
	p := c.fileMap[util.LowerDriver(filename)]
	c.RUnlock()
	return p
}

// Walk walk the global package cache
func (c *GlobalCache) Walk(walkFunc source.WalkFunc, ranks []string) error {
	if c == nil {
		return nil
	}

	c.RLock()
	defer c.RUnlock()

	var idList []string
	for id := range c.idMap {
		idList = append(idList, id)
	}

	getRank := func(id string) int {
		var i int
		for i = 0; i < len(ranks); i++ {
			if strings.HasPrefix(id, ranks[i]) {
				return i
			}
		}

		if strings.Contains(id, ".") {
			return i
		}

		return i + 1
	}

	sort.Slice(idList, func(i, j int) bool {
		r1 := getRank(idList[i])
		r2 := getRank(idList[j])
		if r1 < r2 {
			return true
		}

		if r1 == r2 {
			return idList[i] <= idList[j]
		}

		return false
	})

	return c.walk(idList, walkFunc)
}

func (c *GlobalCache) walk(idList []string, walkFunc source.WalkFunc) error {
	for _, id := range idList {
		p := c.get(id)
		if err := walkFunc(p.pkg); err != nil {
			return err
		}
	}

	return nil
}
