package langserver

import (
	"context"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleTextDocumentSignatureHelp(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.TextDocumentPositionParams) (*lsp.SignatureHelp, error) {
	f := h.overlay.view.GetFile(source.FromDocumentURI(params.TextDocument.URI))
	tok, err := f.GetToken()
	if err != nil {
		return nil, err
	}
	pos := fromProtocolPosition(tok, params.Position)
	info, err := source.SignatureHelp(ctx, f, pos)
	if err != nil {
		return nil, err
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

