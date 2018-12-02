// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"sort"
	"strings"
	"time"
)

func (h *LangHandler) handleTextDocumentCompletion(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.CompletionParams) (*lsp.CompletionList, error) {
	start := time.Now()

	f := h.overlay.view.GetFile(source.FromDocumentURI(params.TextDocument.URI))
	tok, err := f.GetToken()
	if err != nil {
		return nil, err
	}
	pos := fromProtocolPosition(tok, params.Position)
	items, prefix, err := source.Completion(ctx, f, pos)
	if err != nil {
		return nil, err
	}

	result := &lsp.CompletionList{
		IsIncomplete: false,
		Items:        toProtocolCompletionItems(items, prefix, params.Position, false, true),
	}

	elapsedTime := time.Since(start) / time.Second

	h.notifyLog(conn, fmt.Sprintf("completion elapsed time: %d seconds", elapsedTime))

	return result, nil
}

func getLspRange(pos lsp.Position, rangeLen int) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: pos.Line, Character: pos.Character - rangeLen},
		End:   lsp.Position{Line: pos.Line, Character: pos.Character},
	}
}


func toProtocolCompletionItems(items []source.CompletionItem, prefix string, pos lsp.Position, snippetsSupported, signatureHelpEnabled bool) []lsp.CompletionItem {
	var results []lsp.CompletionItem
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})
	insertTextFormat := lsp.ITFPlainText
	if snippetsSupported {
		insertTextFormat = lsp.ITFSnippet
	}
	for i, item := range items {
		// Matching against the label.
		if !strings.HasPrefix(item.Label, prefix) {
			continue
		}
		insertText, triggerSignatureHelp := labelToProtocolSnippets(item.Label, item.Kind, insertTextFormat, signatureHelpEnabled)
		if prefix != "" {
			insertText = insertText[len(prefix)-1:]
		}
		i := lsp.CompletionItem{
			Label:            item.Label,
			Detail:           item.Detail,
			Kind:             toProtocolCompletionItemKind(item.Kind),
			InsertTextFormat: insertTextFormat,
			TextEdit: &lsp.TextEdit{
				NewText: insertText,
				Range: getLspRange(pos, len(prefix)),
			},
			// InsertText is deprecated in favor of TextEdit.
			InsertText: insertText,
			// This is a hack so that the client sorts completion results in the order
			// according to their score. This can be removed upon the resolution of
			// https://github.com/Microsoft/language-server-protocol/issues/348.
			SortText: fmt.Sprintf("%05d", i),
		}
		// If we are completing a function, we should trigger signature help if possible.
		if triggerSignatureHelp && signatureHelpEnabled {
			i.Command = &lsp.Command{
				Command: "editor.action.triggerParameterHints",
			}
		}
		results = append(results, i)
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

func labelToProtocolSnippets(label string, kind source.CompletionItemKind, insertTextFormat lsp.InsertTextFormat, signatureHelpEnabled bool) (string, bool) {
	return label, false
	//switch kind {
	//case source.ConstantCompletionItem:
	//	// The label for constants is of the format "<identifier> = <value>".
	//	// We should now insert the " = <value>" part of the label.
	//	if i := strings.Index(label, " ="); i >= 0 {
	//		return label[:i], false
	//	}
	//case source.FunctionCompletionItem, source.MethodCompletionItem:
	//	trimmed := label[:strings.Index(label, "(")]
	//	params := strings.Trim(label[strings.Index(label, "("):], "()")
	//	if params == "" {
	//		return label, true
	//	}
	//	// Don't add parameters or parens for the plaintext insert format.
	//	if insertTextFormat == lsp.ITFPlainText {
	//		return trimmed, true
	//	}
	//	// If we do have signature help enabled, the user can see parameters as
	//	// they type in the function, so we just return empty parentheses.
	//	if signatureHelpEnabled {
	//		return trimmed + "($1)", true
	//	}
	//	// If signature help is not enabled, we should give the user parameters
	//	// that they can tab through. The insert text format follows the
	//	// specification defined by Microsoft for LSP. The "$", "}, and "\"
	//	// characters should be escaped.
	//	r := strings.NewReplacer(
	//		`\`, `\\`,
	//		`}`, `\}`,
	//		`$`, `\$`,
	//	)
	//	trimmed += "("
	//	for i, p := range strings.Split(params, ",") {
	//		if i != 0 {
	//			trimmed += ", "
	//		}
	//		trimmed += fmt.Sprintf("${%v:%v}", i+1, r.Replace(strings.Trim(p, " ")))
	//	}
	//	return trimmed + ")", false
	//
	//}
	//return label, false
}
