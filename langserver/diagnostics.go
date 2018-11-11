// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package langserver

import (
	"fmt"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/pkg/lsp"
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// NOTICE: Code adapted from https://github.com/golang/tools/blob/master/internal/lsp/diagnostics.go.

func diagnostics(v *source.View, uri lsp.DocumentURI) (map[string][]lsp.Diagnostic, error) {
	f := v.GetFile(source.FromDocumentURI(uri))
	pkg, err := f.GetPackage()
	if err != nil {
		return nil, err
	}

	if pkg == nil {
		return nil, fmt.Errorf("package is null for file %s", uri)
	}
	
	reports := make(map[string][]lsp.Diagnostic)
	for _, filename := range pkg.GoFiles {
		reports[filename] = []lsp.Diagnostic{}
	}
	var parseErrors, typeErrors []packages.Error
	for _, err := range pkg.Errors {
		switch err.Kind {
		case packages.ParseError:
			parseErrors = append(parseErrors, err)
		case packages.TypeError:
			typeErrors = append(typeErrors, err)
		default:
			// ignore other types of errors
			continue
		}
	}
	// Don't report type errors if there are parse errors.
	errors := typeErrors
	if len(parseErrors) > 0 {
		errors = parseErrors
	}
	for _, err := range errors {
		pos := parseErrorPos(err)
		line := pos.Line - 1
		col := pos.Column - 1
		diagnostic := lsp.Diagnostic{
			// TODO(rstambler): Add support for diagnostic ranges.
			Range: lsp.Range{
				Start: lsp.Position{
					Line:      line,
					Character: col,
				},
				End: lsp.Position{
					Line:      line,
					Character: col,
				},
			},
			Severity: lsp.Error,
			Source:   "LSP: Go compiler",
			Message:  err.Msg,
		}
		if _, ok := reports[pos.Filename]; ok {
			reports[pos.Filename] = append(reports[pos.Filename], diagnostic)
		}
	}
	return reports, nil
}

func parseErrorPos(pkgErr packages.Error) (pos token.Position) {
	remainder1, first, hasLine := chop(pkgErr.Pos)
	remainder2, second, hasColumn := chop(remainder1)
	if hasLine && hasColumn {
		pos.Filename = remainder2
		pos.Line = second
		pos.Column = first
	} else if hasLine {
		pos.Filename = remainder1
		pos.Line = first
	}
	return pos
}

func chop(text string) (remainder string, value int, ok bool) {
	i := strings.LastIndex(text, ":")
	if i < 0 {
		return text, 0, false
	}
	v, err := strconv.ParseInt(text[i+1:], 10, 64)
	if err != nil {
		return text, 0, false
	}
	return text[:i], int(v), true
}
