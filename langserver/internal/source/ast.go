package source

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"

	"github.com/saibing/bingo/langserver/internal/util"
	"golang.org/x/tools/go/ast/astutil"
)

// PathEnclosingInterval returns the PackageInfo and ast.Node that
// contain source interval [start, end), and all the node's ancestors
// up to the AST root.  It searches all ast.Files of all packages in prog.
// exact is defined as for astutil.PathEnclosingInterval.
//
// The zero value is returned if not found.
//
func PathEnclosingInterval(pkg Package, fset *token.FileSet, start, end token.Pos) (path []ast.Node, exact bool) {
	path, exact = doEnclosingInterval(pkg, fset, start, end)
	return
}

func doEnclosingInterval(pkg Package, fset *token.FileSet, start, end token.Pos) ([]ast.Node, bool) {
	if pkg == nil || pkg.GetSyntax() == nil {
		return nil, false
	}

	for _, f := range pkg.GetSyntax() {
		if f.Pos() == token.NoPos {
			// This can happen if the parser saw
			// too many errors and bailed out.
			// (Use parser.AllErrors to prevent that.)
			continue
		}
		if !tokenFileContainsPos(fset.File(f.Pos()), start) {
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

func GetPathNodes(pkg Package, fset *token.FileSet, start, end token.Pos) ([]ast.Node, error) {
	nodes, _ := PathEnclosingInterval(pkg, fset, start, end)
	if len(nodes) == 0 {
		s := fset.Position(start)
		return nodes, fmt.Errorf("no node found at %s offset %d", s, s.Offset)
	}

	return nodes, nil
}

func FetchIdentFromPathNodes(fset *token.FileSet, nodes []ast.Node) (*ast.Ident, error) {
	firstNode := nodes[0]
	switch node := firstNode.(type) {
	case *ast.Ident:
		return node, nil
	default:
		return nil, NewInvalidNodeError(fset, firstNode)
	}
}

func NewInvalidNodeError(fset *token.FileSet, node ast.Node) *InvalidNodeError {
	lineCol := func(p token.Pos) string {
		pp := fset.Position(p)
		return fmt.Sprintf("%d:%d", pp.Line, pp.Column)
	}
	return &InvalidNodeError{
		Node: node,
		msg: fmt.Sprintf("invalid node: %s (%s-%s)",
			reflect.TypeOf(node).Elem(), lineCol(node.Pos()), lineCol(node.End())),
	}
}

func GetObjectPathNode(pkg Package, fset *token.FileSet, o types.Object) (nodes []ast.Node, ident *ast.Ident, err error) {
	nodes, _ = GetPathNodes(pkg, fset, o.Pos(), o.Pos())
	if len(nodes) == 0 {
		nodes, err = GetPathNodes(pkg.GetImport(o.Pkg().Path()), fset, o.Pos(), o.Pos())
		if err != nil {
			return nil, nil, err
		}
	}

	ident, err = FetchIdentFromPathNodes(fset, nodes)
	return
}

func GetSyntaxFile(pkg Package, filename string) *ast.File {
	for i, f := range pkg.GetSyntax() {
		if util.PathEqual(pkg.GetFilenames()[i], filename) {
			return f
		}
	}

	return nil
}

func FindIdentObject(pkg Package, ident *ast.Ident) types.Object {
	return pkg.GetTypesInfo().ObjectOf(ident)
}

func FindIdentType(pkg Package, ident *ast.Ident) types.Type {
	return pkg.GetTypesInfo().TypeOf(ident)
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

// FindObject find object
func FindObject(pkg Package, o types.Object) types.Object {
	for _, def := range pkg.GetTypesInfo().Defs {
		if def == nil {
			continue
		}
		if def.Name() == o.Name() {
			return def
		}
	}

	return nil
}
