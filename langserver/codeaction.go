package langserver

import (
	"context"
	"fmt"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/langserver/internal/protocol"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleCodeAction(ctx context.Context, conn jsonrpc2.JSONRPC2,
	req *jsonrpc2.Request, params lsp.CodeActionParams) ([]protocol.CodeAction, error) {
	fileURI := params.TextDocument.URI
	if !util.IsURI(fileURI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("%s not yet supported for out-of-workspace URI", fileURI),
		}
	}

	if !h.project.Contain(fileURI) {
		return []protocol.CodeAction{}, nil
	}

	edits, err := organizeImports(ctx, h.overlay.view, fileURI)
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
	if tok == nil {
		return nil, fmt.Errorf("token file does not exist for file %s", uri)
	}

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
