// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"regexp"
	"sort"
	"strings"
)

// NOTICE: Code adapted from https://github.com/golang/tools/blob/master/internal/lsp/completion.go.

var (
	CIKConstantSupported = lsp.CIKVariable // or lsp.CIKConstant if client supported
	funcArgsRegexp       = regexp.MustCompile(`func\(([^)]+)\)`)
)

func (h *LangHandler) handleTextDocumentCompletion(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.CompletionParams) (*lsp.CompletionList, error) {
	if !util.IsURI(params.TextDocument.URI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("textDocument/complete not yet supported for out-of-workspace URI (%q)", params.TextDocument.URI),
		}
	}

	f := h.overlay.view.GetFile(source.URI(params.TextDocument.URI))
	tok, err := f.GetToken()
	if err != nil {
		return nil, err
	}
	pos := fromProtocolPosition(tok, params.Position)
	items, prefix, err := source.Completion(ctx, f, pos)
	if err != nil {
		return nil, err
	}

	rangeInfo := getLspRange(params.Position, len(prefix))
	return &lsp.CompletionList{
		IsIncomplete: false,
		Items:        toProtocolCompletionItems(items, prefix, rangeInfo),
	}, nil
}

func getLspRange(pos lsp.Position, rangeLen int) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: pos.Line, Character: pos.Character - rangeLen},
		End:   lsp.Position{Line: pos.Line, Character: pos.Character},
	}
}

func toProtocolCompletionItems(items []source.CompletionItem, prefix string, rangeInfo lsp.Range) []lsp.CompletionItem {
	var results []lsp.CompletionItem
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})
	for _, item := range items {
		if strings.HasPrefix(item.Label, prefix) {
			results = append(results, lsp.CompletionItem{
				Label:    item.Label,
				Detail:   item.Detail,
				Kind:     toProtocolCompletionItemKind(item.Kind),
				TextEdit: &lsp.TextEdit{Range: rangeInfo},
			})
		}
	}
	return results
}

func toProtocolCompletionItemKind(kind source.CompletionItemKind) lsp.CompletionItemKind {
	switch kind {
	case source.InterfaceCompletionItem:
		return lsp.CIKInterface
	case source.StructCompletionItem:
		return lsp.CIKStruct
	case source.TypeCompletionItem:
		return lsp.CIKTypeParameter // ??
	case source.ConstantCompletionItem:
		return lsp.CIKConstant
	case source.FieldCompletionItem:
		return lsp.CIKField
	case source.ParameterCompletionItem, source.VariableCompletionItem:
		return lsp.CIKVariable
	case source.FunctionCompletionItem:
		return lsp.CIKFunction
	case source.MethodCompletionItem:
		return lsp.CIKMethod
	case source.PackageCompletionItem:
		return lsp.CIKModule // ??
	default:
		return lsp.CIKText
	}
}
