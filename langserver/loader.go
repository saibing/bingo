package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/goast"
	"github.com/saibing/bingo/langserver/internal/util"
	"go/token"
	"golang.org/x/tools/go/packages"

	"github.com/saibing/bingo/pkg/lsp"
)

func (h *LangHandler) loadFromGlobalCache(ctx context.Context, fileURI lsp.DocumentURI, position lsp.Position) (*packages.Package, token.Pos, error) {
	pos := token.NoPos

	pkg := h.load(fileURI)
	if pkg == nil {
		return nil, pos, fmt.Errorf("%s does not exist", fileURI)
	}

	fAST := goast.GetSyntaxFile(pkg, util.UriToRealPath(fileURI))
	if fAST == nil {
		return nil, pos, fmt.Errorf("%s ast file does not exist", fileURI)
	}

	fToken := pkg.Fset.File(fAST.Pos())
	if fToken == nil {
		return nil, pos, fmt.Errorf("%s token file does not exist", fileURI)
	}

	pos = fromProtocolPosition(fToken, position)
	return pkg, pos, nil
}

func (h *LangHandler) load(uri lsp.DocumentURI) *packages.Package {
	return h.globalCache.GetFromURI(uri)
}
