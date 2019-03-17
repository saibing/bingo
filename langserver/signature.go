package langserver

import (
	"context"
	"fmt"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/span"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleTextDocumentSignatureHelp(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.TextDocumentPositionParams) (*lsp.SignatureHelp, error) {
	fileURI := params.TextDocument.URI
	if err := checkFileURI(fileURI); err != nil {
		return nil, err
	}

	f, err := h.View().GetFile(ctx, span.FromDocumentURI(fileURI))
	if err != nil {
		return nil, err
	}
	tok := f.GetToken(ctx)
	if tok == nil {
		return nil, newJsonrpc2Errorf(jsonrpc2.CodeInternalError, fmt.Sprintf("token file does not exist of %s", fileURI))
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
