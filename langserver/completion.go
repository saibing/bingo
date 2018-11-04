package langserver

import (
	"context"
	"fmt"
	"go/ast"
	"go/build"
	"go/types"
	"regexp"
	"sort"
	"strings"

	"github.com/saibing/bingo/langserver/internal/caches"
	"golang.org/x/tools/go/packages"

	"github.com/saibing/bingo/langserver/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

var (
	CIKConstantSupported = lsp.CIKVariable // or lsp.CIKConstant if client supported
	funcArgsRegexp       = regexp.MustCompile(`func\(([^)]+)\)`)
)

func (h *LangHandler) handleTextDocumentCompletion(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.CompletionParams) (*lsp.CompletionList, error) {
	if !util.IsURI(params.TextDocument.URI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("textDocument/complete not yet supported for out-of-workspace URI (%q)", params.TextDocument.URI),
		}
	}

	completeList, err := h.complete(ctx, conn, req, params)
	if err != nil {
		return nil, err
	}

	sort.Slice(completeList.Items, func(i, j int) bool {
		return completeList.Items[i].Label < completeList.Items[j].Label
	})

	return completeList, nil
}

func (h *LangHandler) complete(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.CompletionParams) (*lsp.CompletionList, error) {
	pkg, ctok, err := h.loadRealTimePackage(ctx, conn, params.TextDocument.URI, params.Position)
	if err != nil {
		// Invalid nodes means we tried to click on something which is
		// not an ident (eg comment/string/etc). Return no information.
		if _, ok := err.(*util.InvalidNodeError); ok {
			return nil, nil
		}
		// This is a common error we get in production when a user is
		// browsing a go pkg which only contains files we can't
		// analyse (usually due to build tags). To reduce signal of
		// actual bad errors, we return no error in this case.
		if _, ok := err.(*build.NoGoError); ok {
			return nil, nil
		}
		return nil, err
	}

	pathNodes, _ := util.PathEnclosingInterval(pkg, ctok.pos, ctok.pos)
	if len(pathNodes) == 0 {
		return nil, nil
	}

	node := pathNodes[0]

	rangeLen := int(ctok.pos - node.Pos())

	switch node := node.(type) {
	case *ast.CallExpr:
		return h.completeCallExpr(params, pkg, rangeLen, node)
	case *ast.Ident:
		if len(pathNodes) >= 3 {
			if selExpr, ok := pathNodes[1].(*ast.SelectorExpr); ok {
				switch n := pathNodes[2].(type) {
				case *ast.CallExpr:
					return h.completeCallExpr(params, pkg, rangeLen, n)
				default:
					return h.completeSelectorExpr(params, pkg, rangeLen, selExpr)
				}
			}
		}
	case *ast.BasicLit:
		if len(pathNodes) >= 3 {
			if _, ok := pathNodes[1].(*ast.ImportSpec); ok {
				if _, ok := pathNodes[2].(*ast.GenDecl); ok {
					return h.completeImport(params, h.packageCache, rangeLen, node)
				}
			}
		}

	case *ast.File:
		if len(node.Unresolved) > 0 {
			rangeLen = len(ctok.lit)
			return h.completeIdent(params, pkg, rangeLen, node.Unresolved[0], ctok)
		}
	}

	return nil, nil
}

func getLspRange(params lsp.CompletionParams, rangeLen int) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: params.Position.Line, Character: params.Position.Character - rangeLen},
		End:   lsp.Position{Line: params.Position.Line, Character: params.Position.Character},
	}
}

func (h *LangHandler) completeIdent(params lsp.CompletionParams, pkg *packages.Package, rangeLen int, ident *ast.Ident, ctok cursorToken) (*lsp.CompletionList, error) {
	var items []lsp.CompletionItem

	selObj := pkg.TypesInfo.ObjectOf(ident)

	switch obj := selObj.(type) {
	case *types.PkgName:
		p := obj.Imported()
		scope := p.Scope()
		names := scope.Names()
		for _, name := range names {
			if strings.HasPrefix(name, ctok.lit) {
				matchedObj := scope.Lookup(name)
				item := h.createCompletionItem(matchedObj, params, rangeLen)
				items = append(items, item)
			}
		}
	}

	return &lsp.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
	return nil, nil
}

func (h *LangHandler) completeImport(params lsp.CompletionParams, packageCache *caches.PackageCache, rangeLen int, basicLit *ast.BasicLit) (*lsp.CompletionList, error) {
	var items []lsp.CompletionItem

	value := strings.Trim(basicLit.Value, `"`)
	rangeLen--

	value = value[0:rangeLen]

	f := func(pkg *packages.Package) error {
		if strings.HasPrefix(pkg.PkgPath, value) {
			item := lsp.CompletionItem{}
			item.Label = pkg.PkgPath
			item.Kind = lsp.CIKModule
			item.TextEdit = &lsp.TextEdit{
				Range:   getLspRange(params, rangeLen),
				NewText: "",
			}

			items = append(items, item)
		}

		return nil
	}

	err := packageCache.Iterate(f)

	return &lsp.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, err
}

func (h *LangHandler) completeCallExpr(params lsp.CompletionParams, pkg *packages.Package, rangeLen int, call *ast.CallExpr) (*lsp.CompletionList, error) {

	var items []lsp.CompletionItem
	item := lsp.CompletionItem{}

	t := pkg.TypesInfo.TypeOf(call.Fun)
	signature, ok := t.(*types.Signature)
	if !ok {
		return nil, nil
	}

	item.Detail = signature.String()

	item.Kind = lsp.CIKFunction
	funcIdent, funcOk := call.Fun.(*ast.Ident)
	if !funcOk {
		selExpr, selOk := call.Fun.(*ast.SelectorExpr)
		if selOk {
			funcIdent = selExpr.Sel
			funcOk = true
			selIdent := selExpr.X.(*ast.Ident)
			selObj := pkg.TypesInfo.ObjectOf(selIdent)
			if _, ok := selObj.Type().(*types.Struct); ok {
				item.Kind = lsp.CIKMethod
			}
		}
	}

	if funcIdent != nil && funcOk {
		item.Label = funcIdent.Name
		funcObj := pkg.TypesInfo.ObjectOf(funcIdent)
		path, _, _ := util.GetObjectPathNode(pkg, funcObj)
		for i := 0; i < len(path); i++ {
			a, b := path[i].(*ast.FuncDecl)
			if b && a.Doc != nil {
				item.Documentation = a.Doc.Text()
				break
			}
		}
	}

	itf, newText := h.getNewText(item.Kind, item.Label, item.Detail)
	item.InsertTextFormat = itf
	item.InsertText = newText

	item.TextEdit = &lsp.TextEdit{
		Range:   getLspRange(params, rangeLen),
		NewText: newText,
	}
	items = append(items, item)

	return &lsp.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func (h *LangHandler) createCompletionItem(obj types.Object, params lsp.CompletionParams, rangeLen int) lsp.CompletionItem {
	item := lsp.CompletionItem{}

	item.Label = obj.Name()
	item.Detail = obj.String()

	switch t := obj.Type().(type) {
	case *types.Signature:
		item.Detail = t.String()
		item.Kind = lsp.CIKFunction

	case *types.Struct:
		item.Kind = lsp.CIKClass
	}

	itf, newText := h.getNewText(item.Kind, item.Label, item.Detail)
	item.InsertTextFormat = itf
	item.InsertText = newText

	item.TextEdit = &lsp.TextEdit{
		Range:   getLspRange(params, rangeLen),
		NewText: newText,
	}

	return item
}

func (h *LangHandler) completeSelectorExpr(params lsp.CompletionParams, pkg *packages.Package, rangeLen int, selExpr *ast.SelectorExpr) (*lsp.CompletionList, error) {
	var items []lsp.CompletionItem

	selIdent := selExpr.X.(*ast.Ident)
	selObj := pkg.TypesInfo.ObjectOf(selIdent)

	switch obj := selObj.(type) {
	case *types.PkgName:
		p := obj.Imported()
		scope := p.Scope()
		names := scope.Names()
		for _, name := range names {
			value := selExpr.Sel.Name[0:rangeLen]
			if strings.HasPrefix(name, value) {
				matchedObj := scope.Lookup(name)
				item := h.createCompletionItem(matchedObj, params, rangeLen)
				items = append(items, item)
			}
		}
	}

	return &lsp.CompletionList{
		IsIncomplete: false,
		Items:        items,
	}, nil
}

func (h *LangHandler) getNewText(kind lsp.CompletionItemKind, name, detail string) (lsp.InsertTextFormat, string) {
	if h.config.FuncSnippetEnabled &&
		kind == lsp.CIKFunction &&
		h.init.Capabilities.TextDocument.Completion.CompletionItem.SnippetSupport {
		args := genSnippetArgs(parseFuncArgs(detail))
		text := fmt.Sprintf("%s(%s)$0", name, strings.Join(args, ", "))
		return lsp.ITFSnippet, text
	}
	return lsp.ITFPlainText, name
}

func parseFuncArgs(def string) []string {
	m := funcArgsRegexp.FindStringSubmatch(def)
	var args []string
	if len(m) > 1 {
		args = strings.Split(m[1], ", ")
	}
	return args
}

func genSnippetArgs(args []string) []string {
	newArgs := make([]string, len(args))
	for i, a := range args {
		// Closing curly braces must be escaped
		a = strings.Replace(a, "}", "\\}", -1)
		newArgs[i] = fmt.Sprintf("${%d:%s}", i+1, a)
	}
	return newArgs
}
