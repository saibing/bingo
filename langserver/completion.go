// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package langserver

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleTextDocumentCompletion(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.CompletionParams) (*lsp.CompletionList, error) {
	fileURI := params.TextDocument.URI
	if err := checkFileURI(fileURI); err != nil {
		return nil, nil
	}

	f, err := h.View().GetFile(ctx, source.FromDocumentURI(fileURI))
	if err != nil {
		return nil, err
	}
	tok := f.GetToken(ctx)
	if tok == nil {
		return nil, newJsonrpc2Errorf(jsonrpc2.CodeInternalError, fmt.Sprintf("token file does not exist of %s", fileURI))
	}

	pos := fromProtocolPosition(tok, params.Position)
	items, prefix, err := source.Completion(ctx, f, pos, h.project.Cache())
	if err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	useSnippets := h.clientSupportsSnippets() && !h.config.DisableFuncSnippet
	result := &lsp.CompletionList{
		IsIncomplete: false,
		Items:        toProtocolCompletionItems(items, prefix, params.Position, useSnippets, false),
	}
	return result, nil
}

func (h *LangHandler) clientSupportsSnippets() bool {
	return h.init != nil && h.init.Capabilities.TextDocument.Completion.CompletionItem.SnippetSupport
}

func getLspRange(pos lsp.Position, rangeLen int) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: pos.Line, Character: pos.Character - rangeLen},
		End:   lsp.Position{Line: pos.Line, Character: pos.Character},
	}
}

func toProtocolCompletionItems(candidates []source.CompletionItem, prefix string, pos lsp.Position, snippetsSupported, signatureHelpEnabled bool) []lsp.CompletionItem {
	insertTextFormat := lsp.ITFPlainText
	if snippetsSupported {
		insertTextFormat = lsp.ITFSnippet
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	items := []lsp.CompletionItem{}
	for i, candidate := range candidates {
		// Matching against the label.
		if !strings.HasPrefix(candidate.Label, prefix) {
			continue
		}
		insertText, _ := labelToProtocolSnippets(candidate.Label, candidate.Kind, insertTextFormat, signatureHelpEnabled)
		//if strings.HasPrefix(insertText, prefix) {
		//	insertText = insertText[len(prefix):]
		//}
		item := lsp.CompletionItem{
			Label:            candidate.Label,
			Detail:           candidate.Detail,
			Kind:             toProtocolCompletionItemKind(candidate.Kind),
			InsertTextFormat: insertTextFormat,
			TextEdit: &lsp.TextEdit{
				NewText: insertText,
				Range:   getLspRange(pos, len(prefix)),
			},
			// InsertText is deprecated in favor of TextEdit.
			InsertText: insertText,
			// This is a hack so that the client sorts completion results in the order
			// according to their score. This can be removed upon the resolution of
			// https://github.com/Microsoft/language-server-protocol/issues/348.
			SortText:      fmt.Sprintf("%05d", i),
			Documentation: candidate.Documentation,
		}
		// If we are completing a function, we should trigger signature help if possible.
		//if triggerSignatureHelp && signatureHelpEnabled {
		//	item.Command = &lsp.Command{
		//		Command: "editor.action.triggerParameterHints",
		//	}
		//}
		items = append(items, item)
	}
	return items
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
	switch kind {
	case source.ConstantCompletionItem:
		// The label for constants is of the format "<identifier> = <value>".
		// We should not insert the " = <value>" part of the label.
		if i := strings.Index(label, " ="); i >= 0 {
			return label[:i], false
		}
	case source.FunctionCompletionItem, source.MethodCompletionItem:
		var trimmed, params string
		if i := strings.Index(label, "("); i >= 0 {
			trimmed = label[:i]
			params = strings.Trim(label[i:], "()")
		}
		if params == "" || trimmed == "" {
			return label, true
		}
		// Don't add parameters or parens for the plaintext insert format.
		if insertTextFormat == lsp.ITFPlainText {
			return trimmed, true
		}
		// If we do have signature help enabled, the user can see parameters as
		// they type in the function, so we just return empty parentheses.
		if signatureHelpEnabled {
			return trimmed + "($1)", true
		}
		// If signature help is not enabled, we should give the user parameters
		// that they can tab through. The insert text format follows the
		// specification defined by Microsoft for LSP. The "$", "}, and "\"
		// characters should be escaped.
		r := strings.NewReplacer(
			`\`, `\\`,
			`}`, `\}`,
			`$`, `\$`,
		)
		b := bytes.NewBufferString(trimmed)
		b.WriteByte('(')
		for i, p := range splitArgs(params) {
			if i != 0 {
				b.WriteString(", ")
			}
			paramName := strings.Split(strings.Trim(p, " "), " ")[0]
			fmt.Fprintf(b, "${%v:%v}", i+1, r.Replace(paramName))
		}
		fmt.Fprintf(b, ")$0")
		return b.String(), false

	}
	return label, false
}

func splitArgs(input string) []string {
	if len(input) == 0 {
		return nil
	}
	var res []string
	var parCounter, prevIdx int
	for i, c := range input {
		switch c {
		case '(':
			parCounter++
		case ')':
			parCounter--
		case ',':
			if parCounter == 0 {
				res = append(res, input[prevIdx:i])
				prevIdx = i + 1
			}
		}
	}
	res = append(res, input[prevIdx:])

	return res
}
