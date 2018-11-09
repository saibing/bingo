// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// NOTICE: Code adapted from golang.org/x/tools/internal/lsp/format.go

package langserver

import (
	"bytes"
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"go/format"
)

const (
	formatToolGoimports string = "goimports"
	formatToolGofmt     string = "gofmt"
)

func (h *LangHandler) handleTextDocumentFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentFormattingParams) ([]lsp.TextEdit, error) {
	if !util.IsURI(params.TextDocument.URI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("%s not yet supported for out-of-workspace URI (%q)", req.Method, params.TextDocument.URI),
		}
	}

	return formatRange(h.overlay.view, params.TextDocument.URI, nil)
}

func (h *LangHandler) handleTextDocumentRangeFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentRangeFormattingParams) ([]lsp.TextEdit, error) {
	if !util.IsURI(params.TextDocument.URI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("%s not yet supported for out-of-workspace URI (%q)", req.Method, params.TextDocument.URI),
		}
	}

	return formatRange(h.overlay.view, params.TextDocument.URI, &params.Range)
}

// formatRange formats a document with a given range.
func formatRange(v *source.View, uri lsp.DocumentURI, rng *lsp.Range) ([]lsp.TextEdit, error) {
	data, err := v.GetFile(source.URI(uri)).Read()
	if err != nil {
		return nil, err
	}
	if rng != nil {
		start, err := positionToOffset(data, int(rng.Start.Line), int(rng.Start.Character))
		if err != nil {
			return nil, err
		}
		end, err := positionToOffset(data, int(rng.End.Line), int(rng.End.Character))
		if err != nil {
			return nil, err
		}
		data = data[start:end]
		// format.Source will fail if the substring is not a balanced expression tree.
		// TODO(rstambler): parse the file and use astutil.PathEnclosingInterval to
		// find the largest ast.Node n contained within start:end, and format the
		// region n.Pos-n.End instead.
	}
	// format.Source changes slightly from one release to another, so the version
	// of Go used to build the LSP server will determine how it formats code.
	// This should be acceptable for all users, who likely be prompted to rebuild
	// the LSP server on each Go release.
	fmted, err := format.Source([]byte(data))
	if err != nil {
		return nil, err
	}
	if rng == nil {
		// Get the ending line and column numbers for the original file.
		line := bytes.Count(data, []byte("\n"))
		col := len(data) - bytes.LastIndex(data, []byte("\n")) - 1
		if col < 0 {
			col = 0
		}
		rng = &lsp.Range{
			Start: lsp.Position{
				Line:      0,
				Character: 0,
			},
			End: lsp.Position{
				Line:      line,
				Character: col,
			},
		}
	}
	// TODO(rstambler): Compute text edits instead of replacing whole file.
	return []lsp.TextEdit{
		{
			Range:   *rng,
			NewText: string(fmted),
		},
	}, nil
}

// positionToOffset converts a 0-based line and column number in a file
// to a byte offset value.
func positionToOffset(contents []byte, line, col int) (int, error) {
	start := 0
	for i := 0; i < int(line); i++ {
		if start >= len(contents) {
			return 0, fmt.Errorf("file contains %v lines, not %v lines", i, line)
		}
		index := bytes.IndexByte(contents[start:], '\n')
		if index == -1 {
			return 0, fmt.Errorf("file contains %v lines, not %v lines", i, line)
		}
		start += index + 1
	}
	offset := start + int(col)
	return offset, nil
}


