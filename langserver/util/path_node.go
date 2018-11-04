package util

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
	"reflect"
)



// PathEnclosingInterval returns the PackageInfo and ast.Node that
// contain source interval [start, end), and all the node's ancestors
// up to the AST root.  It searches all ast.Files of all packages in prog.
// exact is defined as for astutil.PathEnclosingInterval.
//
// The zero value is returned if not found.
//
func PathEnclosingInterval(pkg *packages.Package, start, end token.Pos) (path []ast.Node, exact bool) {
	path, exact = doEnclosingInterval(pkg, start, end)
	if path != nil && len(path) != 0 {
		return path, exact
	}

	for _, importPkg := range pkg.Imports {
		path, exact = doEnclosingInterval(importPkg, start, end)
		if path != nil && len(path) != 0 {
			return path, exact
		}
	}
	return nil, false
}

func doEnclosingInterval(pkg *packages.Package, start, end token.Pos) ([]ast.Node, bool) {
	if pkg == nil {
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

func GetPathNode(pkg *packages.Package, start, end token.Pos) ([]ast.Node, *ast.Ident, error) {
	nodes, _ := PathEnclosingInterval(pkg, start, end)
	if len(nodes) == 0 {
		s := pkg.Fset.Position(start)
		return nodes, nil, fmt.Errorf("no node found at %s offset %d", s, s.Offset)
	}

	firstNode := nodes[0]
	node, ok := firstNode.(*ast.Ident)
	if !ok {
		lineCol := func(p token.Pos) string {
			pp := pkg.Fset.Position(p)
			return fmt.Sprintf("%d:%d", pp.Line, pp.Column)
		}
		return nil, nil, &InvalidNodeError{
			Node: firstNode,
			msg: fmt.Sprintf("invalid node: %s (%s-%s)",
				reflect.TypeOf(firstNode).Elem(), lineCol(firstNode.Pos()), lineCol(firstNode.End())),
		}
	}
	return nodes, node, nil
}

func GetObjectPathNode(pkg *packages.Package, o types.Object) ([]ast.Node, *ast.Ident, error) {
	nodes, node, err := GetPathNode(pkg, o.Pos(), o.Pos())
	if len(nodes) == 0 {
		return GetPathNode(pkg.Imports[o.Pkg().Name()], o.Pos(), o.Pos())
	}

	return nodes, node, err
}

func PosForFileOffset(fset *token.FileSet, filename string, offset int) token.Pos {
	var f *token.File
	fset.Iterate(func(ff *token.File) bool {
		if PathEqual(ff.Name(), filename) {
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
	for _, file := range pkg.Syntax {
		if PathEqual(file.Name.Name, filename) {
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

