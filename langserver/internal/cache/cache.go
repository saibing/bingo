package cache

import (
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"golang.org/x/tools/go/packages"
)

type id2Package map[string]*packages.Package
type file2Package map[string]*packages.Package
type path2Package map[string]*packages.Package

// PackageCache package cache
type PackageCache struct {
	mu      sync.RWMutex
	idMap   id2Package
	pathMap path2Package
	fileMap file2Package
}

// DebugCache trace package cache
var DebugCache = false

// ParseFileTrace trace file parse
var ParseFileTrace = false

// NewCache new a package cache
func NewCache() *PackageCache {
	return &PackageCache{idMap: id2Package{}, pathMap: path2Package{}, fileMap: file2Package{}}
}

func (c *PackageCache) put(pkg *packages.Package) {
	if c == nil {
		return
	}

	if DebugCache {
		log.Printf("cache %s = %p\n", pkg.ID, pkg)
	}

	c.delete(pkg.ID)
	c.idMap[pkg.ID] = pkg
	c.pathMap[pkg.PkgPath] = pkg

	for _, file := range pkg.CompiledGoFiles {
		c.fileMap[util.LowerDriver(file)] = pkg
	}
}

func (c *PackageCache) get(id string) *packages.Package {
	if c == nil {
		return nil
	}

	pkg := c.idMap[id]

	if DebugCache {
		log.Printf("get %s = %p\n", id, pkg)
	}
	return pkg
}

func (c *PackageCache) delete(id string) {
	if c == nil {
		return
	}

	if DebugCache {
		log.Printf("delete %s %p\n", id, c.idMap[id])
	}

	pkg := c.idMap[id]
	if pkg == nil {
		return
	}

	delete(c.idMap, id)
	delete(c.pathMap, pkg.PkgPath)

	for _, file := range pkg.CompiledGoFiles {
		delete(c.fileMap, util.LowerDriver(file))
	}
}

func (c *PackageCache) RLock() {
	if c == nil {
		return
	}

	c.mu.RLock()
}

func (c *PackageCache) RUnlock() {
	if c == nil {
		return
	}

	c.mu.RUnlock()
}

func (c *PackageCache) Lock() {
	if c == nil {
		return
	}

	c.mu.Lock()
}

func (c *PackageCache) Unlock() {
	if c == nil {
		return
	}

	c.mu.Unlock()
}

func (c *PackageCache) clean(idList []string) {
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
func (c *PackageCache) Get(pkgPath string) *packages.Package {
	if c == nil {
		return nil
	}

	c.RLock()
	pkg := c.pathMap[pkgPath]
	c.RUnlock()
	return pkg
}

// GetByURI get package by filename from global cache
func (c *PackageCache) GetByURI(filename string) *packages.Package {
	if c == nil {
		return nil
	}
	c.RLock()
	pkg := c.fileMap[util.LowerDriver(filename)]
	c.RUnlock()
	return pkg
}

// Walk walk the global package cache
func (c *PackageCache) Walk(walkFunc source.WalkFunc, ranks []string) error {
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

func (c *PackageCache) walk(idList []string, walkFunc WalkFunc) error {
	for _, id := range idList {
		pkg := c.get(id)
		if err := walkFunc(pkg); err != nil {
			return err
		}
	}

	return nil
}

