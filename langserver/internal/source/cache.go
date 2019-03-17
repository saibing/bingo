package source

// WalkFunc walk function
type WalkFunc func(p Package) error

type Cache interface {
	Walk(walkFunc WalkFunc, ranks []string) error
}

