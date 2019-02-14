package langserver

import (
	"context"
	"fmt"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleTextDocumentSignatureHelp(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.TextDocumentPositionParams) (*lsp.SignatureHelp, error) {
	fileURI := params.TextDocument.URI
	if !util.IsURI(fileURI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("%s not yet supported for out-of-workspace URI", fileURI),
		}
	}

	f, err := h.overlay.view.GetFile(ctx, source.FromDocumentURI(fileURI))
	if err != nil {
		return nil, err
	}
	tok, err := f.GetToken()
	if err != nil {
		return nil, err
	}
	pos := fromProtocolPosition(tok, params.Position)
	info, err := source.SignatureHelp(ctx, f, pos, h.project.GetBuiltinPackage(), h.DefaultConfig.EnhanceSignatureHelp)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return nil, nil
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return toProtocolSignatureHelp(info), nil
}

func toProtocolSignatureHelp(info *source.SignatureInformation) *lsp.SignatureHelp {
	return &lsp.SignatureHelp{
		ActiveParameter: info.ActiveParameter,
		ActiveSignature: 0, // there is only ever one possible signature
		Signatures: []lsp.SignatureInformation{
			{
				Label:      info.Label,
				Parameters: toProtocolParameterInformation(info.Parameters),
			},
		},
	}
}

func toProtocolParameterInformation(info []source.ParameterInformation) []lsp.ParameterInformation {
	var result []lsp.ParameterInformation
	for _, p := range info {
		result = append(result, lsp.ParameterInformation{
			Label: p.Label,
		})
	}
	return result
}
