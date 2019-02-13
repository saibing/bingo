package source

import "golang.org/x/tools/go/packages"

// WalkFunc walk function
type WalkFunc func(p *packages.Package) error

type Cache interface {
	Walk(walkFunc WalkFunc, ranks []string)
}
