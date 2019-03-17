package langserver

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"

	"github.com/saibing/bingo/langserver/internal/source"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
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

func (h *LangHandler) typeCheck(ctx context.Context, fileURI lsp.DocumentURI, position lsp.Position) (source.Package, token.Pos, error) {
	pos := token.NoPos

	if err := checkFileURI(fileURI); err != nil {
		return nil, pos, err
	}

	pkg, f, err := h.project.TypeCheck(ctx, fileURI)
	if err != nil {
		return nil, pos, err
	}

	if pkg == nil {
		return nil, pos, fmt.Errorf("package for %s is null", fileURI)
	}

	if pkg.IsIllTyped() {
		return nil, pos, fmt.Errorf("package for %s is ill typed", fileURI)
	}

	if f == nil {
		pos, err = h.getPosFromPkg(pkg, fileURI, position)
	} else {
		pos, err = h.getPosFromFile(ctx, pkg, f, position)
	}
	return pkg, pos, err
}

func (h *LangHandler) getPosFromFile(ctx context.Context, pkg source.Package, f source.File, position lsp.Position) (token.Pos, error) {
	tok := f.GetToken(ctx)
	pos := fromProtocolPosition(tok, position)
	return pos, nil
}

func (h *LangHandler) getPosFromPkg(pkg source.Package, fileURI lsp.DocumentURI, position lsp.Position) (token.Pos, error) {
	pos := token.NoPos
	fAST, err := h.getAstFromPkg(pkg, fileURI)
	if err != nil {
		return pos, err
	}

	fToken := pkg.GetFileSet().File(fAST.Pos())
	if fToken == nil {
		return pos, fmt.Errorf("%s token file does not exist", fileURI)
	}

	pos = fromProtocolPosition(fToken, position)
	return pos, nil
}

func (h *LangHandler) loadPackageAndAst(ctx context.Context, fileURI lsp.DocumentURI) (source.Package, *ast.File, error) {
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
		astFile = f.GetAST(ctx)
	}

	return pkg, astFile, err
}

func (h *LangHandler) getAstFromPkg(pkg source.Package, fileURI lsp.DocumentURI) (*ast.File, error) {
	fAST := source.GetSyntaxFile(pkg, util.UriToRealPath(fileURI))
	if fAST == nil {
		return nil, fmt.Errorf("%s ast file does not exist", fileURI)
	}

	return fAST, nil
}
