package langserver

import (
	"context"
	"errors"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/goast"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"
)

func (h *LangHandler) handleTextDocumentReferences(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.ReferenceParams) ([]lsp.Location, error) {
	pkg, pos, err := h.typeCheck(ctx, params.TextDocument.URI, params.Position)
	if err != nil {
		// Invalid nodes means we tried to click on something which is
		// not an ident (eg comment/string/etc). Return no information.
		if _, ok := err.(*goast.InvalidNodeError); ok {
			return []lsp.Location{}, nil
		}
		return nil, err
	}

	pathNodes, err := goast.GetPathNodes(pkg, pos, pos)
	if err != nil {
		return nil, err
	}

	ident, err := goast.FetchIdentFromPathNodes(pkg, pathNodes)
	if err != nil {
		return nil, err
	}

	// NOTICE: Code adapted from golang.org/x/tools/cmd/guru
	// referrers.go.
	obj := pkg.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return nil, errors.New("references object not found")
	}

	if obj.Pkg() == nil {
		if _, builtin := obj.(*types.Builtin); builtin {
			// We don't support builtin references due to the massive number
			// of references, so ignore the missing package error.
			return []lsp.Location{}, nil
		}
		return nil, fmt.Errorf("no package found for object %s", obj)
	}

	refs, err := h.findReferences(ctx, conn, pkg.Fset, h.globalCache, obj)
	if err != nil {
		// If we are canceled, cancel loop early
		return nil, err
	}

	if params.Context.IncludeDeclaration {
		refs = append(refs, &ast.Ident{NamePos: obj.Pos(), Name: obj.Name()})
	}

	return refStreamAndCollect(pkg.Fset, refs, params.Context.XLimit), nil
}

// refStreamAndCollect returns all refs read in from chan until it is
// closed. While it is reading, it will also occasionally stream out updates of
// the refs received so far.
func refStreamAndCollect(fset *token.FileSet, refs []*ast.Ident, limit int) []lsp.Location {
	if limit == 0 {
		// If we don't have a limit, just set it to a value we should never exceed
		limit = len(refs)
	}

	l := len(refs)
	if limit < l {
		l = limit
	}

	var locs []lsp.Location

	seen := map[string]bool{}
	for i := 0; i < l; i++ {
		n := refs[i]
		loc := goRangeToLSPLocation(fset, n.Pos(), n.Name)

		// remove duplicate results because they contain uses of the xtest package
		locStr := formatLocation(loc)
		if seen[locStr] {
			continue
		}
		seen[locStr] = true
		locs = append(locs, loc)
	}

	return locs
}

func formatLocation(loc lsp.Location) string {
	return fmt.Sprintf("%s:%s", loc.URI, loc.Range)
}

// findReferences will find all references to obj. It will only return
// references from packages in pkg.Imports.
func (h *LangHandler) findReferences(ctx context.Context, conn jsonrpc2.JSONRPC2, fset *token.FileSet, packageCache *source.GlobalCache, obj types.Object) ([]*ast.Ident, error) {
	// Bail out early if the context is canceled
	var refs []*ast.Ident
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	defPkgPath := obj.Pkg().Path()
	objPos := fset.Position(obj.Pos())

	var queryObj types.Object

	defPkg := h.globalCache.GetFromPackagePath(defPkgPath)
	// Find the object by its position (slightly ugly).
	queryObj = findObject(defPkg.Fset, defPkg.TypesInfo, objPos)
	if queryObj == nil {
		return nil, fmt.Errorf("object at %s not found in package %s", objPos, defPkgPath)
	}

	f := func(pkg *packages.Package) error {
		if _, ok := pkg.Imports[defPkgPath]; !ok && pkg.PkgPath != defPkgPath {
			return nil
		}

		for id, obj := range pkg.TypesInfo.Uses {
			if sameObj(queryObj, obj) {
				refs = append(refs, id)
			}
		}

		return nil
	}

	err := packageCache.Search(f)
	if err != nil {
		return nil, err
	}

	return refs, nil
}

// findObject returns the object defined at the specified position.
func findObject(fset *token.FileSet, info *types.Info, objposn token.Position) types.Object {
	good := func(obj types.Object) bool {
		if obj == nil {
			return false
		}
		pos := fset.Position(obj.Pos())
		return pos.Filename == objposn.Filename && pos.Offset == objposn.Offset
	}
	for _, obj := range info.Defs {
		if good(obj) {
			return obj
		}
	}
	for _, obj := range info.Implicits {
		if good(obj) {
			return obj
		}
	}
	return nil
}

// same reports whether x and y are identical, or both are PkgNames
// that import the same Package.
func sameObj(x, y types.Object) bool {
	if x == y {
		return true
	}

	if x.Pkg() != nil && y.Pkg() != nil && x.Pkg().Path() == y.Pkg().Path() {
		// enable find the xtest pakcage's uses, but this will product some duplicate results
		return x.Name() == y.Name()
	}

	if x, ok := x.(*types.PkgName); ok {
		if y, ok := y.(*types.PkgName); ok {
			return x.Imported() == y.Imported()
		}
	}
	return false
}
