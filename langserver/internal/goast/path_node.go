package goast

import (
	"fmt"
	"github.com/saibing/bingo/langserver/internal/util"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
	"reflect"
)


type LookupPackageFunc func(importPath string) *packages.Package

// PathEnclosingInterval returns the PackageInfo and ast.Node that
// contain source interval [start, end), and all the node's ancestors
// up to the AST root.  It searches all ast.Files of all packages in prog.
// exact is defined as for astutil.PathEnclosingInterval.
//
// The zero value is returned if not found.
//
func PathEnclosingInterval(pkg *packages.Package, start, end token.Pos, lookup LookupPackageFunc) (path []ast.Node, exact bool) {
	path, exact = doEnclosingInterval(pkg, start, end, lookup)
	if path != nil && len(path) != 0 {
		return path, exact
	}

	for _, importPkg := range pkg.Imports {
		path, exact = doEnclosingInterval(importPkg, start, end, lookup)
		if path != nil && len(path) != 0 {
			return path, exact
		}
	}
	return nil, false
}

func doEnclosingInterval(pkg *packages.Package, start, end token.Pos, lookup LookupPackageFunc) ([]ast.Node, bool) {
	if pkg == nil {
		return nil, false
	}

	if pkg.Syntax == nil {
		pkg = lookup(pkg.PkgPath)
		if pkg == nil {
			return nil, false
		}
	}

	for _, f := range pkg.Syntax {
		if f.Pos() == token.NoPos {
			// This can happen if the parser saw
			// too many errors and bailed out.
			// (Use parser.AllErrors to prevent that.)
			continue
		}
		if !tokenFileContainsPos(pkg.Fset.File(f.Pos()), start) {
			continue
		}
		if path, exact := astutil.PathEnclosingInterval(f, start, end); path != nil {
			return path, exact
		}
	}

	return nil, false
}

// TODO(adonovan): make this a method: func (*token.File) Contains(token.Pos)
func tokenFileContainsPos(f *token.File, pos token.Pos) bool {
	p := int(pos)
	base := f.Base()
	return base <= p && p < base+f.Size()
}

type dereferencable interface {
	Elem() types.Type
}

// TypeLookup looks for a named type, but will search through
// any number of type qualifiers (chan/array/slice/pointer)
// which have an unambiguous base type. If no named type is
// found, we are not interested, because this is only used
// for finding a type's definition.
func TypeLookup(typ types.Type) *types.TypeName {
	if typ == nil {
		return nil
	}
	for {
		switch t := typ.(type) {
		case *types.Named:
			return t.Obj()
		case *types.Map:
			return nil
		case dereferencable:
			typ = t.Elem()
		default:
			return nil
		}
	}
}

// Deref returns a pointer's element type; otherwise it returns typ.
func Deref(typ types.Type) types.Type {
	if p, ok := typ.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return typ
}

type InvalidNodeError struct {
	Node ast.Node
	msg  string
}

func (e *InvalidNodeError) Error() string {
	return e.msg
}

func GetPathNodes(pkg *packages.Package, start, end token.Pos, lookup LookupPackageFunc) ([]ast.Node, error) {
	nodes, _ := PathEnclosingInterval(pkg, start, end, lookup)
	if len(nodes) == 0 {
		s := pkg.Fset.Position(start)
		return nodes, fmt.Errorf("no node found at %s offset %d", s, s.Offset)
	}

	return nodes, nil
}

func FetchIdentFromPathNodes(pkg *packages.Package, nodes []ast.Node) (*ast.Ident, error) {
	firstNode := nodes[0]
	switch node := firstNode.(type) {
	case *ast.Ident:
		return node, nil
	default:
		return nil, NewInvalidNodeError(pkg, firstNode)
	}
}

func NewInvalidNodeError(pkg *packages.Package, node ast.Node) *InvalidNodeError {
	lineCol := func(p token.Pos) string {
		pp := pkg.Fset.Position(p)
		return fmt.Sprintf("%d:%d", pp.Line, pp.Column)
	}
	return &InvalidNodeError{
		Node: node,
		msg: fmt.Sprintf("invalid node: %s (%s-%s)",
			reflect.TypeOf(node).Elem(), lineCol(node.Pos()), lineCol(node.End())),
	}
}

func searchPackage(root *packages.Package, path string) *packages.Package {
	seen := map[string]bool{}
	return search(root, path, seen)
}

func search(root *packages.Package, path string, seen map[string]bool) *packages.Package {
	if seen[root.PkgPath] {
		return nil
	}

	seen[root.PkgPath] = true

	if pkg, ok := root.Imports[path]; ok {
		return pkg
	}

	for _, importPkg := range root.Imports {
		pkg := search(importPkg, path, seen)
		if pkg != nil {
			return pkg
		}
	}

	return nil
}

func GetObjectPathNode(pkg *packages.Package, o types.Object, lookup LookupPackageFunc) (nodes []ast.Node, ident *ast.Ident, err error) {
	nodes, _ = GetPathNodes(pkg, o.Pos(), o.Pos(), lookup)
	if len(nodes) == 0 {
		nodes, err = GetPathNodes(searchPackage(pkg, o.Pkg().Path()), o.Pos(), o.Pos(), lookup)
		if err != nil {
			return nil, nil, err
		}
	}

	ident, err = FetchIdentFromPathNodes(pkg, nodes)
	return
}

func PosForFileOffset(fset *token.FileSet, filename string, offset int) token.Pos {
	var f *token.File
	fset.Iterate(func(ff *token.File) bool {
		if util.PathEqual(ff.Name(), filename) {
			f = ff
			return false // break out of loop
		}
		return true
	})
	if f == nil {
		return token.NoPos
	}
	return f.Pos(offset)
}


func GetSyntaxFile(pkg *packages.Package, filename string) *ast.File {
	for i, file := range pkg.Syntax {
		if util.PathEqual(pkg.CompiledGoFiles[i], filename) {
			return file
		}
	}

	return nil
}

// CallExpr climbs AST tree up until call expression
func CallExpr(fset *token.FileSet, nodes []ast.Node) *ast.CallExpr {
	for _, node := range nodes {
		callExpr, ok := node.(*ast.CallExpr)
		if ok {
			return callExpr
		}
	}
	return nil
}

func GetIdent(fset *token.FileSet, nodes []ast.Node) *ast.Ident {
	for _, node := range nodes {
		ident, ok := node.(*ast.Ident)
		if ok {
			return ident
		}
	}
	return nil
}

