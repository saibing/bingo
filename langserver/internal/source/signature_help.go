// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package source

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ast/astutil"
)

type SignatureInformation struct {
	Label           string
	Parameters      []ParameterInformation
	ActiveParameter int
}

type ParameterInformation struct {
	Label string
}

func SignatureHelp(ctx context.Context, f File, pos token.Pos, builtinPkg Package, enhance bool) (*SignatureInformation, error) {
	fAST := f.GetAST(ctx)
	pkg := f.GetPackage(ctx)

	// Find a call expression surrounding the query position.
	var callExpr *ast.CallExpr
	path, _ := astutil.PathEnclosingInterval(fAST, pos, pos)
	if path == nil {
		return nil, fmt.Errorf("cannot find node enclosing position")
	}
	for _, node := range path {
		if c, ok := node.(*ast.CallExpr); ok {
			callExpr = c
			break
		}
	}
	if callExpr == nil || callExpr.Fun == nil {
		return nil, nil
	}

	// Get the type information for the function corresponding to the call expression.
	var obj types.Object
	switch t := callExpr.Fun.(type) {
	case *ast.Ident:
		obj = pkg.GetTypesInfo().ObjectOf(t)
	case *ast.SelectorExpr:
		obj = pkg.GetTypesInfo().ObjectOf(t.Sel)
	default:
		return nil, fmt.Errorf("the enclosing function is malformed")
	}
	if obj == nil {
		return nil, fmt.Errorf("cannot resolve %s", callExpr.Fun)
	}
	// Find the signature corresponding to the object.
	var sig *types.Signature
	switch obj.(type) {
	case *types.Var:
		if underlying, ok := obj.Type().Underlying().(*types.Signature); ok {
			sig = underlying
		}
	case *types.Func:
		sig = obj.Type().(*types.Signature)

	case *types.Builtin:
		obj = FindObject(builtinPkg, obj)
		if _, ok := obj.(*types.Func); ok {
			sig = obj.Type().(*types.Signature)
		}
	}
	if sig == nil {
		return nil, fmt.Errorf("no function signatures found for %s", obj.Name())
	}
	pkgStringer := qualifier(fAST, pkg.GetTypes(), pkg.GetTypesInfo())
	var paramInfo []ParameterInformation
	for i := 0; i < sig.Params().Len(); i++ {
		param := sig.Params().At(i)
		label := types.TypeString(param.Type(), pkgStringer)
		if param.Name() != "" {
			label = fmt.Sprintf("%s %s", param.Name(), label)
		}
		paramInfo = append(paramInfo, ParameterInformation{
			Label: label,
		})
	}
	// Determine the query position relative to the number of parameters in the function.
	activeParam := len(callExpr.Args)
	for i, expr := range callExpr.Args {
		if expr.End() >= pos {
			activeParam = i
			break
		}
	}
	// Label for function, qualified by package name.
	label := obj.Name()
	if pkg := pkgStringer(obj.Pkg()); pkg != "" {
		label = pkg + "." + label
	}

	label += formatParams(sig.Params(), sig.Variadic(), pkgStringer)
	if enhance {
		label += formatResults(sig.Results(), pkgStringer)
	}

	return &SignatureInformation{
		Label:           label,
		Parameters:      paramInfo,
		ActiveParameter: activeParam,
	}, nil
}

func formatResults(t *types.Tuple, qualifier types.Qualifier) string {
	if t.Len() == 0 {
		return ""
	}
	var b bytes.Buffer
	b.WriteByte(' ')
	if t.Len() > 1 {
		b.WriteByte('(')
	}
	for i := 0; i < t.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		el := t.At(i)
		typ := types.TypeString(el.Type(), qualifier)
		// handle single named result
		if t.Len() == 1 && el.Name() != "" {
			fmt.Fprintf(&b, "(%v %v)", el.Name(), typ)
			break
		}
		if el.Name() == "" {
			fmt.Fprintf(&b, "%v", typ)
		} else {
			fmt.Fprintf(&b, "%v %v", el.Name(), typ)
		}
	}
	if t.Len() > 1 {
		b.WriteByte(')')
	}
	return b.String()
}
