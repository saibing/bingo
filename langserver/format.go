// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// NOTICE: Code adapted from golang.org/x/tools/internal/lsp/format.go

package langserver

import (
	"context"
	"github.com/saibing/bingo/langserver/internal/cache"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"go/token"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

func (h *LangHandler) handleTextDocumentFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentFormattingParams) ([]lsp.TextEdit, error) {
	return formatRange(ctx, h.overlay.view, params.TextDocument.URI, nil)
}

func (h *LangHandler) handleTextDocumentRangeFormatting(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.DocumentRangeFormattingParams) ([]lsp.TextEdit, error) {
	return formatRange(ctx, h.overlay.view, params.TextDocument.URI, &params.Range)
}

// formatRange formats a document with a given range.
func formatRange(ctx context.Context, v *cache.View, uri lsp.DocumentURI, rng *lsp.Range) ([]lsp.TextEdit, error) {
	f := v.GetFile(source.FromDocumentURI(uri))
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

	if len(edits) == 1 && rng == nil {
		content, _ := f.Read()
		return ComputeTextEdits(string(content), edits[0].NewText), nil
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

// ComputeTextEdits computes text edits that are required to
// change the `unformatted` to the `formatted` text.
func ComputeTextEdits(unformatted string, formatted string) []lsp.TextEdit {
	// LSP wants a list of TextEdits. We use difflib to compute a
	// non-naive TextEdit. Originally we returned an edit which deleted
	// everything followed by inserting everything. This leads to a poor
	// experience in vscode.
	unformattedLines := strings.Split(unformatted, "\n")
	formattedLines := strings.Split(formatted, "\n")
	m := difflib.NewMatcher(unformattedLines, formattedLines)
	var edits []lsp.TextEdit
	for _, op := range m.GetOpCodes() {
		switch op.Tag {
		case 'r': // 'r' (replace):  a[i1:i2] should be replaced by b[j1:j2]
			edits = append(edits, lsp.TextEdit{
				Range: lsp.Range{
					Start: lsp.Position{
						Line: op.I1,
					},
					End: lsp.Position{
						Line: op.I2,
					},
				},
				NewText: strings.Join(formattedLines[op.J1:op.J2], "\n") + "\n",
			})
		case 'd': // 'd' (delete):   a[i1:i2] should be deleted, j1==j2 in this case.
			edits = append(edits, lsp.TextEdit{
				Range: lsp.Range{
					Start: lsp.Position{
						Line: op.I1,
					},
					End: lsp.Position{
						Line: op.I2,
					},
				},
			})
		case 'i': // 'i' (insert):   b[j1:j2] should be inserted at a[i1:i1], i1==i2 in this case.
			edits = append(edits, lsp.TextEdit{
				Range: lsp.Range{
					Start: lsp.Position{
						Line: op.I1,
					},
					End: lsp.Position{
						Line: op.I1,
					},
				},
				NewText: strings.Join(formattedLines[op.J1:op.J2], "\n") + "\n",
			})
		}
	}

	return edits
}
