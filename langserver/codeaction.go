package langserver

import (
	"context"

	"github.com/saibing/bingo/langserver/internal/protocol"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleCodeAction(ctx context.Context, conn jsonrpc2.JSONRPC2,
	req *jsonrpc2.Request, params lsp.CodeActionParams) ([]protocol.CodeAction, error) {
	return nil, nil
}
