// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package source provides core features for use by Go editors and tools.
package source

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"strings"

	"github.com/saibing/bingo/langserver/internal/diff"
	"github.com/saibing/bingo/langserver/internal/span"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/imports"
)

// Format formats a file with a given range.
func Format(ctx context.Context, f File, rng span.Range) ([]TextEdit, error) {
	fAST := f.GetAST(ctx)
	path, exact := astutil.PathEnclosingInterval(fAST, rng.Start, rng.End)
	if !exact || len(path) == 0 {
		return nil, fmt.Errorf("no exact AST node matching the specified range")
	}
	node := path[0]
	// format.Node can fail when the AST contains a bad expression or
	// statement. For now, we preemptively check for one.
	// TODO(rstambler): This should really return an error from format.Node.
	var isBad bool
	ast.Inspect(node, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.BadDecl, *ast.BadExpr, *ast.BadStmt:
			isBad = true
			return false
		default:
			return true
		}
	})
	if isBad {
		return nil, fmt.Errorf("unable to format file due to a badly formatted AST")
	}
	// format.Node changes slightly from one release to another, so the version
	// of Go used to build the LSP server will determine how it formats code.
	// This should be acceptable for all users, who likely be prompted to rebuild
	// the LSP server on each Go release.
	fset := f.GetFileSet(ctx)
	buf := &bytes.Buffer{}
	if err := format.Node(buf, fset, node); err != nil {
		return nil, err
	}
	return computeTextEdits(ctx, f, buf.String()), nil
}

// Imports formats a file using the goimports tool.
func Imports(ctx context.Context, f File, rng span.Range) ([]TextEdit, error) {
	formatted, err := imports.Process(f.GetToken(ctx).Name(), f.GetContent(ctx), nil)
	if err != nil {
		return nil, err
	}
	return computeTextEdits(ctx, f, string(formatted)), nil
}

func computeTextEdits(ctx context.Context, file File, formatted string) (edits []TextEdit) {
	u := strings.SplitAfter(string(file.GetContent(ctx)), "\n")
	f := strings.SplitAfter(formatted, "\n")
	for _, op := range diff.Operations(u, f) {
		s := span.New(file.URI(), span.NewPoint(op.I1+1, 1, 0), span.NewPoint(op.I2+1, 1, 0))
		switch op.Kind {
		case diff.Delete:
			// Delete: unformatted[i1:i2] is deleted.
			edits = append(edits, TextEdit{Span: s})
		case diff.Insert:
			// Insert: formatted[j1:j2] is inserted at unformatted[i1:i1].
			edits = append(edits, TextEdit{Span: s, NewText: op.Content})
		}
	}
	return edits
}
