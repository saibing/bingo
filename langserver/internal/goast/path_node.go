package goast

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"

	"github.com/saibing/bingo/langserver/internal/util"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// PathEnclosingInterval returns the PackageInfo and ast.Node that
// contain source interval [start, end), and all the node's ancestors
// up to the AST root.  It searches all ast.Files of all packages in prog.
// exact is defined as for astutil.PathEnclosingInterval.
//
// The zero value is returned if not found.
//
func PathEnclosingInterval(root *packages.Package, start, end token.Pos) (path []ast.Node, exact bool) {
	found := func(pkg *packages.Package) bool {
		path, exact = doEnclosingInterval(pkg, start, end)
		return path != nil && len(path) != 0
	}

	visit(root, found)
	return
}

func doEnclosingInterval(pkg *packages.Package, start, end token.Pos) ([]ast.Node, bool) {
	if pkg == nil || pkg.Syntax == nil {
		return nil, false
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

func GetPathNodes(pkg *packages.Package, start, end token.Pos) ([]ast.Node, error) {
	nodes, _ := PathEnclosingInterval(pkg, start, end)
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

func GetObjectPathNode(pkg *packages.Package, o types.Object) (nodes []ast.Node, ident *ast.Ident, err error) {
	nodes, _ = GetPathNodes(pkg, o.Pos(), o.Pos())
	if len(nodes) == 0 {
		nodes, err = GetPathNodes(SearchImportPackage(pkg, o.Pkg().Path()), o.Pos(), o.Pos())
		if err != nil {
			return nil, nil, err
		}
	}

	ident, err = FetchIdentFromPathNodes(pkg, nodes)
	return
}

func visit(root *packages.Package, found func(*packages.Package) bool) bool {
	if found(root) {
		return true
	}

	for _, ip := range root.Imports {
		if found(ip) {
			return true
		}
	}

	return false
}

func GetSyntaxFile(pkg *packages.Package, filename string) *ast.File {
	var file *ast.File
	found := func(pkg *packages.Package) bool {
		for i, f := range pkg.Syntax {
			if util.PathEqual(pkg.CompiledGoFiles[i], filename) {
				file = f
				return true
			}
		}

		return false
	}

	visit(pkg, found)

	return file
}

func FindIdentObject(pkg *packages.Package, ident *ast.Ident) types.Object {
	var o types.Object

	if pkg == nil {
		return nil
	}

	found := func(pkg *packages.Package) bool {
		if pkg.TypesInfo == nil {
			return false
		}

		o = pkg.TypesInfo.ObjectOf(ident)
		return o != nil
	}

	visit(pkg, found)
	return o
}

func FindIdentType(pkg *packages.Package, ident *ast.Ident) types.Type {
	var t types.Type

	if pkg == nil {
		return nil
	}

	found := func(pkg *packages.Package) bool {
		if pkg.TypesInfo == nil {
			return false
		}

		t = pkg.TypesInfo.TypeOf(ident)
		return t != nil
	}

	visit(pkg, found)
	return t
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

func SearchImportPackage(root *packages.Package, path string) *packages.Package {
	if root == nil {
		return nil
	}

	var p *packages.Package

	found := func(pkg *packages.Package) bool {
		p = pkg.Imports[path]
		return p != nil
	}

	visit(root, found)

	return p
}

// FindObject find object
func FindObject(pkg *packages.Package, o types.Object) types.Object {
	if pkg == nil {
		return nil
	}

	for _, def := range pkg.TypesInfo.Defs {
		if def == nil {
			continue
		}
		if def.Name() == o.Name() {
			return def
		}
	}

	return nil
}
