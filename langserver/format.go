// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// NOTICE: Code adapted from golang.org/x/tools/internal/lsp/format.go

package langserver

import (
	"context"
	"fmt"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/span"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

const (
	goimportsStyle = "goimports"
)

func (h *LangHandler) handleTextDocumentFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentFormattingParams) ([]lsp.TextEdit, error) {
	return formatRange(ctx, h.View(), params.TextDocument.URI, nil, h.DefaultConfig.FormatStyle == goimportsStyle)
}

func (h *LangHandler) handleTextDocumentRangeFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentRangeFormattingParams) ([]lsp.TextEdit, error) {
	return formatRange(ctx, h.View(), params.TextDocument.URI, &params.Range, h.DefaultConfig.FormatStyle == goimportsStyle)
}

// formatRange formats a document with a given range.
func formatRange(ctx context.Context, v source.View, uri lsp.DocumentURI, rng *lsp.Range, imports bool) ([]lsp.TextEdit, error) {
	sourceURI, err := fromProtocolURI(uri)
	if err != nil {
		return nil, err
	}
	f, err := v.GetFile(ctx, sourceURI)
	if err != nil {
		return nil, err
	}
	tok := f.GetToken(ctx)
	if tok == nil {
		return nil, newJsonrpc2Errorf(jsonrpc2.CodeInternalError, fmt.Sprintf("token file does not exist of %s", uri))
	}
	var r span.Range
	if rng == nil {
		r.Start = tok.Pos(0)
		r.End = tok.Pos(tok.Size())
	} else {
		r = fromProtocolRange(tok, *rng)
	}

	var edits []source.TextEdit
	if imports {
		edits, err = source.Imports(ctx, f, r)
	} else {
		edits, err = source.Format(ctx, f, r)
	}
	if err != nil {
		return nil, err
	}
	return toProtocolEdits(ctx, f, edits), nil
}

func toProtocolEdits(ctx context.Context, f source.File, edits []source.TextEdit) []lsp.TextEdit {
	if edits == nil {
		return []lsp.TextEdit{}
	}

	result := make([]lsp.TextEdit, len(edits))
	for i, edit := range edits {
		result[i] = lsp.TextEdit{
			Range:   toProtocolRange(edit.Span),
			NewText: edit.NewText,
		}
	}
	return result
}

// toProtocolRange converts from a source range back to a protocol range.
func toProtocolRange(s span.Span) lsp.Range {
	return lsp.Range{
		Start: toProtocolPosition(s.Start()),
		End:   toProtocolPosition(s.End()),
	}
}
