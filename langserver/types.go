package langserver

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// deref returns a pointer's element type; otherwise it returns typ.
func deref(typ types.Type) types.Type {
	if p, ok := typ.Underlying().(*types.Pointer); ok {
		return p.Elem()
	}
	return typ
}

// typeLookup looks for a named type, but will search through
// any number of type qualifiers (chan/array/slice/pointer)
// which have an unambiguous base type. If no named type is
// found, we are not interested, because this is only used
// for finding a type's definition.
func typeLookup(typ types.Type) *types.TypeName {
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

// TODO(adonovan): make this a method: func (*token.File) Contains(token.Pos)
func tokenFileContainsPos(f *token.File, pos token.Pos) bool {
	p := int(pos)
	base := f.Base()
	return base <= p && p < base+f.Size()
}

// PathEnclosingInterval returns the PackageInfo and ast.Node that
// contain source interval [start, end), and all the node's ancestors
// up to the AST root.  It searches all ast.Files of all packages in prog.
// exact is defined as for astutil.PathEnclosingInterval.
//
// The zero value is returned if not found.
//
func pathEnclosingInterval(pkg *packages.Package, start, end token.Pos) (path []ast.Node, exact bool) {
	path, exact = doEnclosingInterval(pkg, start, end)
	if path != nil && len(path) != 0{
		return path, exact
	}

	for _, importPkg := range pkg.Imports {
		path, exact = doEnclosingInterval(importPkg, start, end)
		if path != nil && len(path) != 0{
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

func getPathNode(pkg *packages.Package, start, end token.Pos) ([]ast.Node, *ast.Ident, error) {
	nodes, _ := pathEnclosingInterval(pkg, start, end)
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
		return nil, nil, &invalidNodeError{
			Node: firstNode,
			msg: fmt.Sprintf("invalid node: %s (%s-%s)",
				reflect.TypeOf(firstNode).Elem(), lineCol(firstNode.Pos()), lineCol(firstNode.End())),
		}
	}
	return nodes, node, nil
}

func getObjectPathNode(pkg *packages.Package, o types.Object) ([]ast.Node, *ast.Ident, error) {
	nodes, node, err := getPathNode(pkg, o.Pos(), o.Pos())
	if len(nodes) == 0 {
		return getPathNode(pkg.Imports[o.Pkg().Name()], o.Pos(), o.Pos())
	}

	return nodes, node, err
}