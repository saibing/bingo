// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// NOTICE: Code adapted from golang.org/x/tools/internal/lsp/format.go

package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"go/token"
)

func (h *LangHandler) handleTextDocumentFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentFormattingParams) ([]lsp.TextEdit, error) {
	if !util.IsURI(params.TextDocument.URI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("%s not yet supported for out-of-workspace URI (%q)", req.Method, params.TextDocument.URI),
		}
	}

	return formatRange(ctx, h.overlay.view, params.TextDocument.URI, nil)
}

func (h *LangHandler) handleTextDocumentRangeFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentRangeFormattingParams) ([]lsp.TextEdit, error) {
	if !util.IsURI(params.TextDocument.URI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("%s not yet supported for out-of-workspace URI (%q)", req.Method, params.TextDocument.URI),
		}
	}

	return formatRange(ctx, h.overlay.view, params.TextDocument.URI, &params.Range)
}

// formatRange formats a document with a given range.
func formatRange(ctx context.Context, v *source.View, uri lsp.DocumentURI, rng *lsp.Range) ([]lsp.TextEdit, error) {
	f := v.GetFile(source.URI(uri))
	tok, err := f.GetToken()
	if err != nil {
		return nil, err
	}
	var r source.Range
	if rng == nil {
		r.Start = tok.Pos(0)
		r.End = tok.Pos(tok.Size())
	} else {
		r = fromProtocolRange(tok, *rng)
	}
	edits, err := source.Format(ctx, f, r)
	if err != nil {
		return nil, err
	}
	return toProtocolEdits(tok, edits), nil
}

func toProtocolEdits(f *token.File, edits []source.TextEdit) []lsp.TextEdit {
	if edits == nil {
		return nil
	}
	result := make([]lsp.TextEdit, len(edits))
	for i, edit := range edits {
		result[i] = lsp.TextEdit{
			Range:   toProtocolRange(f, edit.Range),
			NewText: edit.NewText,
		}
	}
	return result
}

// toProtocolRange converts from a source range back to a protocol range.
func toProtocolRange(f *token.File, r source.Range) lsp.Range {
	return lsp.Range{
		Start: toProtocolPosition(f, r.Start),
		End:   toProtocolPosition(f, r.End),
	}
}


