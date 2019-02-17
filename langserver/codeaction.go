package langserver

import (
	"context"

	"github.com/saibing/bingo/langserver/internal/protocol"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleCodeAction(ctx context.Context, conn jsonrpc2.JSONRPC2,
	req *jsonrpc2.Request, params lsp.CodeActionParams) ([]protocol.CodeAction, error) {

	edits, err := organizeImports(ctx, h.overlay.view, params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	return []protocol.CodeAction{
		{
			Title: "Organize Imports",
			Kind:  protocol.SourceOrganizeImports,
			Edit: lsp.WorkspaceEdit{
				Changes: map[string][]lsp.TextEdit{
					string(params.TextDocument.URI): edits,
				},
			},
		},
	}, nil
}

func organizeImports(ctx context.Context, v source.View, uri lsp.DocumentURI) ([]lsp.TextEdit, error) {
	sourceURI, err := fromProtocolURI(uri)
	if err != nil {
		return nil, err
	}
	f, err := v.GetFile(ctx, sourceURI)
	if err != nil {
		return nil, err
	}
	tok := f.GetToken()
	r := source.Range{
		Start: tok.Pos(0),
		End:   tok.Pos(tok.Size()),
	}
	edits, err := source.Imports(ctx, f, r)
	if err != nil {
		return nil, err
	}
	return toProtocolEdits(f, edits), nil
}
