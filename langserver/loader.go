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

	if !util.IsURI(fileURI) {
		return nil, pos, fmt.Errorf("typechecking of out-of-workspace URI (%q) is not yet supported", fileURI)
	}

	filename := h.FilePath(fileURI)

	pkg := h.load(filename)
	if pkg == nil {
		return nil, pos, fmt.Errorf("%s does not exist", filename)
	}

	pos, err := h.startPos(ctx, pkg, fileURI, position)
	return pkg, pos, err
}

func (h *LangHandler) startPos(ctx context.Context, pkg *packages.Package, fileURI lsp.DocumentURI, position lsp.Position) (token.Pos, error) {
	pos := token.NoPos

	contents, err := h.readFile(ctx, fileURI)
	if err != nil {
		return pos, err
	}

	filename := util.UriToRealPath(fileURI)
	offset, valid, why := offsetForPosition(contents, position)
	if !valid {
		return pos, fmt.Errorf("invalid position: %s:%d:%d (%s)", filename, position.Line, position.Character, why)
	}

	pos = goast.PosForFileOffset(pkg.Fset, filename, offset)
	if pos == token.NoPos {
		return pos, fmt.Errorf("invalid location: %s:#%d", filename, offset)
	}

	return pos, nil
}

func (h *LangHandler) load(filename string) *packages.Package {
	return h.globalCache.GetFromFilename(util.GetRealPath(filename))
}