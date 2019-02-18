package langserver

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/saibing/bingo/langserver/internal/goast"
	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/sourcegraph/go-lsp"
	"golang.org/x/tools/go/packages"
)

func checkFileURI(fileURI lsp.DocumentURI) error {
	if !util.IsURI(fileURI) {
		err := &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("%s not yet supported for out-of-workspace URI", fileURI),
		}
		return err
	}

	return nil
}

func (h *LangHandler) typeCheck(ctx context.Context, fileURI lsp.DocumentURI, position lsp.Position) (*packages.Package, token.Pos, error) {
	pos := token.NoPos

	if err := checkFileURI(fileURI); err != nil {
		return nil, pos, err
	}

	pkg, f, err := h.project.TypeCheck(ctx, fileURI)
	if err != nil {
		return nil, pos, err
	}

	if f == nil {
		pos, err = h.getPosFromPkg(pkg, fileURI, position)
	} else {
		pos, err = h.getPosFromFile(pkg, f, position)
	}
	return pkg, pos, err
}

func (h *LangHandler) getPosFromFile(pkg *packages.Package, f source.File, position lsp.Position) (token.Pos, error) {
	tok := f.GetToken()
	pos := fromProtocolPosition(tok, position)
	return pos, nil
}

func (h *LangHandler) getPosFromPkg(pkg *packages.Package, fileURI lsp.DocumentURI, position lsp.Position) (token.Pos, error) {
	pos := token.NoPos
	fAST, err := h.getAstFromPkg(pkg, fileURI)
	if err != nil {
		return pos, err
	}

	fToken := pkg.Fset.File(fAST.Pos())
	if fToken == nil {
		return pos, fmt.Errorf("%s token file does not exist", fileURI)
	}

	pos = fromProtocolPosition(fToken, position)
	return pos, nil
}

func (h *LangHandler) loadPackageAndAst(ctx context.Context, fileURI lsp.DocumentURI) (*packages.Package, *ast.File, error) {
	if err := checkFileURI(fileURI); err != nil {
		return nil, nil, err
	}

	pkg, f, err := h.project.TypeCheck(ctx, fileURI)
	if err != nil {
		return nil, nil, err
	}

	var astFile *ast.File
	if f == nil {
		astFile, err = h.getAstFromPkg(pkg, fileURI)
	} else {
		astFile = f.GetAST()
	}

	return pkg, astFile, err
}

func (h *LangHandler) getAstFromPkg(pkg *packages.Package, fileURI lsp.DocumentURI) (*ast.File, error) {
	fAST := goast.GetSyntaxFile(pkg, util.UriToRealPath(fileURI))
	if fAST == nil {
		return nil, fmt.Errorf("%s ast file does not exist", fileURI)
	}

	return fAST, nil
}
