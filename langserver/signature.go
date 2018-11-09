package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/source"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"

	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleTextDocumentSignatureHelp(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.TextDocumentPositionParams) (*lsp.SignatureHelp, error) {
	if !util.IsURI(params.TextDocument.URI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("textDocument/signatureHelp not yet supported for out-of-workspace URI (%q)", params.TextDocument.URI),
		}
	}

	pkg, pos, err := h.typeCheck(params)
	if err != nil {
		return nil, err
	}

	pathNodes, _ := util.PathEnclosingInterval(pkg, pos, pos)
	call := util.CallExpr(pkg.Fset, pathNodes)
	if call == nil {
		return nil, nil
	}
	t := pkg.TypesInfo.TypeOf(call.Fun)
	signature, ok := t.(*types.Signature)
	if !ok {
		return nil, nil
	}
	info := lsp.SignatureInformation{Label: shortType(signature)}
	sParams := signature.Params()
	info.Parameters = make([]lsp.ParameterInformation, sParams.Len())
	for i := 0; i < sParams.Len(); i++ {
		info.Parameters[i] = lsp.ParameterInformation{Label: shortParam(sParams.At(i))}
	}
	activeParameter := len(call.Args)
	for index, arg := range call.Args {
		if arg.End() >= pos {
			activeParameter = index
			break
		}
	}

	funcIdent, funcOk := call.Fun.(*ast.Ident)
	if !funcOk {
		selExpr, selOk := call.Fun.(*ast.SelectorExpr)
		if selOk {
			funcIdent = selExpr.Sel
			funcOk = true
		}
	}
	if funcIdent != nil && funcOk {
		funcObj := pkg.TypesInfo.ObjectOf(funcIdent)
		path, _, _ := util.GetObjectPathNode(pkg, funcObj)
		for i := 0; i < len(path); i++ {
			a, b := path[i].(*ast.FuncDecl)
			if b && a.Doc != nil {
				info.Documentation = a.Doc.Text()
				break
			}
		}
	}

	return &lsp.SignatureHelp{Signatures: []lsp.SignatureInformation{info}, ActiveSignature: 0, ActiveParameter: activeParameter}, nil
}


func (h *LangHandler) typeCheck(params lsp.TextDocumentPositionParams) (*packages.Package, token.Pos, error) {
	f := h.overlay.view.GetFile(source.URI(string(params.TextDocument.URI)))
	pkg, err := f.GetPackage()
	if err != nil {
		return nil, token.NoPos, err
	}
	tok, err := f.GetToken()
	if err != nil {
		return nil, token.NoPos, err
	}

	pos := fromProtocolPosition(tok, params.Position)
	return pkg, pos, nil
}

// shortTyoe returns shorthand type notation without specifying type's import path
func shortType(t types.Type) string {
	return types.TypeString(t, func(*types.Package) string {
		return ""
	})
}

// shortParam returns shorthand parameter notation in form "name type" without specifying type's import path
func shortParam(param *types.Var) string {
	ret := param.Name()
	if ret != "" {
		ret += " "
	}
	return ret + shortType(param.Type())
}
