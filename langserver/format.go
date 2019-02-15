// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// NOTICE: Code adapted from golang.org/x/tools/internal/lsp/format.go

package langserver

import (
	"context"
	"go/token"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

const (
	goimportsStyle = "goimports"
)

func (h *LangHandler) handleTextDocumentFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentFormattingParams) ([]lsp.TextEdit, error) {
	return formatRange(ctx, h.overlay.view, params.TextDocument.URI, nil, h.DefaultConfig.FormatStyle == goimportsStyle)
}

func (h *LangHandler) handleTextDocumentRangeFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentRangeFormattingParams) ([]lsp.TextEdit, error) {
	return formatRange(ctx, h.overlay.view, params.TextDocument.URI, &params.Range, h.DefaultConfig.FormatStyle == goimportsStyle)
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
	tok := f.GetToken()
	var r source.Range
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
	return toProtocolEdits(f, edits), nil
}

func toProtocolEdits(f source.File, edits []source.TextEdit) []lsp.TextEdit {
	if edits == nil {
		return nil
	}
	tok := f.GetToken()
	content := f.GetContent()
	// When a file ends with an empty line, the newline character is counted
	// as part of the previous line. This causes the formatter to insert
	// another unnecessary newline on each formatting. We handle this case by
	// checking if the file already ends with a newline character.
	hasExtraNewline := content[len(content)-1] == '\n'
	result := make([]lsp.TextEdit, len(edits))
	for i, edit := range edits {
		rng := toProtocolRange(tok, edit.Range)
		// If the edit ends at the end of the file, add the extra line.
		if hasExtraNewline && tok.Offset(edit.Range.End) == len(content) {
			rng.End.Line++
			rng.End.Character = 0
		}
		result[i] = lsp.TextEdit{
			Range:   rng,
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
