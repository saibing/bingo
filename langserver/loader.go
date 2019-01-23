package langserver

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/saibing/bingo/langserver/internal/goast"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/saibing/bingo/pkg/lsp"
	"golang.org/x/tools/go/packages"
)

// uriHasPrefix returns true if s is starts with the given prefix
func uriHasPrefix(s, prefix lsp.DocumentURI) bool {
	s1, _ := source.FromDocumentURI(s).Filename()
	s2, _ := source.FromDocumentURI(prefix).Filename()

	return strings.HasPrefix(s1, s2)
}

func (h *LangHandler) typeCheck(ctx context.Context, fileURI lsp.DocumentURI, position lsp.Position) (*packages.Package, token.Pos, error) {
	pkg, pos, _ := h.loadFromGlobalCache(ctx, fileURI, position)
	if pkg != nil {
		return pkg, pos, nil
	}

	uri := source.FromDocumentURI(fileURI)
	pkg, pos, err := h.loadFromSourceView(uri, position)
	if ctx.Err() != nil {
		return nil, token.NoPos, ctx.Err()
	}
	return pkg, pos, err
}

func (h *LangHandler) loadFromSourceView(uri source.URI, position lsp.Position) (*packages.Package, token.Pos, error) {
	f := h.overlay.view.GetFile(uri)
	pkg, err := f.GetPackage()
	if err != nil {
		return nil, token.NoPos, err
	}
	if pkg == nil {
		return nil, token.NoPos, fmt.Errorf("package is null for file %s", uri)
	}
	tok, err := f.GetToken()
	if err != nil {
		return nil, token.NoPos, err
	}

	if tok == nil {
		return nil, token.NoPos, fmt.Errorf("token file is null for file %s", uri)
	}

	pos := fromProtocolPosition(tok, position)
	return pkg, pos, nil
}

func (h *LangHandler) loadAstFromSourceView(fileURI lsp.DocumentURI) (*packages.Package, *ast.File, error) {
	uri := source.FromDocumentURI(fileURI)

	f := h.overlay.view.GetFile(uri)
	pkg, err := f.GetPackage()
	if err != nil {
		return nil, nil, err
	}

	if pkg == nil {
		return nil, nil, fmt.Errorf("package is null for file %s", uri)
	}

	astFile, err := f.GetAST()
	if err != nil {
		return nil, nil, err
	}

	if astFile == nil {
		return nil, nil, fmt.Errorf("ast file is null for file %s", uri)
	}

	return pkg, astFile, nil
}

func (h *LangHandler) loadFromGlobalCache(ctx context.Context, fileURI lsp.DocumentURI, position lsp.Position) (*packages.Package, token.Pos, error) {
	pos := token.NoPos
	pkg, fAST, err := h.loadAstFromGlobalCache(fileURI)
	if err != nil {
		return nil, pos, err
	}

	fToken := pkg.Fset.File(fAST.Pos())
	if fToken == nil {
		return nil, pos, fmt.Errorf("%s token file does not exist", fileURI)
	}

	pos = fromProtocolPosition(fToken, position)
	return pkg, pos, nil
}

func (h *LangHandler) loadAstFromGlobalCache(fileURI lsp.DocumentURI) (*packages.Package, *ast.File, error) {
	pkg := h.load(fileURI)
	if pkg == nil {
		return nil, nil, fmt.Errorf("%s does not exist", fileURI)
	}

	fAST := goast.GetSyntaxFile(pkg, util.UriToRealPath(fileURI))
	if fAST == nil {
		return nil, nil, fmt.Errorf("%s ast file does not exist", fileURI)
	}

	return pkg, fAST, nil
}

func (h *LangHandler) load(uri lsp.DocumentURI) *packages.Package {
	return h.project.GetFromURI(uri)
}

