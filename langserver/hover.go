package langserver

import (
	"bytes"
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/goast"
	"github.com/slimsag/godocmd"
	"go/ast"
	"go/build"
	"go/format"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleHover(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.TextDocumentPositionParams) (*lsp.Hover, error) {
	pkg, pos, err := h.typeCheck(ctx, params.TextDocument.URI, params.Position)
	if err != nil {
		// Invalid nodes means we tried to click on something which is
		// not an ident (eg comment/string/etc). Return no information.
		if _, ok := err.(*goast.InvalidNodeError); ok {
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

	pathNodes, err := goast.GetPathNodes(pkg, pos, pos)
	if err != nil {
		return nil, err
	}

	switch node := pathNodes[0].(type) {
	case *ast.Ident:
		return h.hoverIdent(pkg, node, params.Position)
	case *ast.BasicLit:
		return h.hoverBasicLit(pkg, pathNodes, node, params.Position)
	case *ast.TypeSpec:
		return h.hoverIdent(pkg, node.Name, params.Position)
	}

	return nil, goast.NewInvalidNodeError(pkg, pathNodes[0])
}

func (h *LangHandler) hoverBasicLit(pkg *packages.Package, nodes []ast.Node, basicLit *ast.BasicLit, position lsp.Position) (*lsp.Hover, error) {
	if len(nodes) == 1 {
		return nil, nil
	}

	if node, ok := nodes[1].(*ast.ImportSpec); ok {
		importPkg := h.getImportPackage(pkg, strings.Trim(node.Path.Value, `"`))
		comments := packageDoc(importPkg.Syntax, importPkg.Name)
		r := rangeForNode(pkg.Fset, node)
		return &lsp.Hover{
			Contents: maybeAddComments(comments, []lsp.MarkedString{{Language: "go", Value: "package " + importPkg.Name}}),
			Range:    &r,
		}, nil
	}

	return nil, nil
}

func (h *LangHandler) hoverIdent(pkg *packages.Package, ident *ast.Ident, position lsp.Position) (*lsp.Hover, error) {

	o := pkg.TypesInfo.ObjectOf(ident)
	t := pkg.TypesInfo.TypeOf(ident)

	if o == nil && t == nil {
		comments := packageDoc(pkg.Syntax, ident.Name)

		// Package statement idents don't have an object, so try that separately.
		r := rangeForNode(pkg.Fset, ident)
		if pkgName := packageStatementName(pkg.Fset, pkg.Syntax, ident); pkgName != "" {
			return &lsp.Hover{
				Contents: maybeAddComments(comments, []lsp.MarkedString{{Language: "go", Value: "package " + pkgName}}),
				Range:    &r,
			}, nil
		}
		return nil, fmt.Errorf("type/object not found at %+v", position)
	}

	if o != nil && !o.Pos().IsValid() {
		// Only builtins have invalid position, and don't have useful info.
		return nil, nil
	}
	// Don't package-qualify the string output.
	qf := func(*types.Package) string { return "" }

	var s string
	var extra string
	if f, ok := o.(*types.Var); ok && f.IsField() {
		// TODO(sqs): make this be like (T).F not "struct field F string".
		s = "struct " + o.String()
	} else if o != nil {
		if obj, ok := o.(*types.TypeName); ok {
			typ := obj.Type().Underlying()
			if _, ok := typ.(*types.Struct); ok {
				s = "type " + obj.Name() + " struct"
				extra = prettyPrintTypesString(types.TypeString(typ, qf))
			}
			if _, ok := typ.(*types.Interface); ok {
				s = "type " + obj.Name() + " interface"
				extra = prettyPrintTypesString(types.TypeString(typ, qf))
			}
		}
		if s == "" {
			s = types.ObjectString(o, qf)
		}

	} else if t != nil {
		s = types.TypeString(t, qf)
	}

	comments, err := h.findComments(pkg, o, ident.Name)
	if err != nil {
		return nil, err
	}
	contents := maybeAddComments(comments, []lsp.MarkedString{{Language: "go", Value: s}})
	if extra != "" {
		// If we have extra info, ensure it comes after the usually
		// more useful documentation
		contents = append(contents, lsp.MarkedString{Language: "go", Value: extra})
	}

	r := rangeForNode(pkg.Fset, ident)
	return &lsp.Hover{Contents: contents, Range: &r}, nil
}

func (h *LangHandler) getImportPackage(pkg *packages.Package, importPath string) *packages.Package {
	importPkg := pkg.Imports[importPath]
	return importPkg
}

func (h *LangHandler) lookupPackage(path string) *packages.Package {
	pkg, _ := h.getFindPackageFunc()(h.globalCache, path)
	return pkg
}

func (h *LangHandler) findComments(pkg *packages.Package, o types.Object, name string) (string, error) {
	if o == nil {
		return "", nil
	}

	// Package names must be resolved specially, so do this now to avoid
	// additional overhead.
	if v, ok := o.(*types.PkgName); ok {
		importPkg := h.getImportPackage(pkg, v.Imported().Path())
		if importPkg == nil {
			return "", fmt.Errorf("failed to import package %q", v.Imported().Path())
		}

		return packageDoc(importPkg.Syntax, name), nil
	}

	// Resolve the object o into its respective ast.Node
	pathNodes, _, _ := goast.GetObjectPathNode(pkg, o)
	if len(pathNodes) == 0 {
		return "", nil
	}

	// Pull the comment out of the comment map for the file. Do
	// not search too far away from the current path.
	var comments string
	for i := 0; i < 3 && i < len(pathNodes) && comments == ""; i++ {
		switch v := pathNodes[i].(type) {
		case *ast.Field:
			// Concat associated documentation with any inline comments
			comments = joinCommentGroups(v.Doc, v.Comment)
		case *ast.ValueSpec:
			comments = v.Doc.Text()
		case *ast.TypeSpec:
			comments = v.Doc.Text()
		case *ast.GenDecl:
			comments = v.Doc.Text()
		case *ast.FuncDecl:
			comments = v.Doc.Text()
		}
	}
	return comments, nil
}

// packageStatementName returns the package name ((*ast.Ident).Name)
// of node iff node is the package statement of a file ("package p").
func packageStatementName(fset *token.FileSet, files []*ast.File, node *ast.Ident) string {
	for _, f := range files {
		if f.Name == node {
			return node.Name
		}
	}
	return ""
}

// maybeAddComments appends the specified comments converted to Markdown godoc
// form to the specified contents slice, if the comments string is not empty.
func maybeAddComments(comments string, contents []lsp.MarkedString) []lsp.MarkedString {
	if comments == "" {
		return contents
	}
	var b bytes.Buffer
	doc.ToMarkdown(&b, comments, nil)
	return append(contents, lsp.RawMarkedString(b.String()))
}

// joinCommentGroups joins the resultant non-empty comment text from two
// CommentGroups with a newline.
func joinCommentGroups(a, b *ast.CommentGroup) string {
	aText := a.Text()
	bText := b.Text()
	if aText == "" {
		return bText
	} else if bText == "" {
		return aText
	} else {
		return aText + "\n" + bText
	}
}

// packageDoc finds the documentation for the named package from its files or
// additional files.
func packageDoc(files []*ast.File, pkgName string) string {
	for _, f := range files {
		if f.Name.Name == pkgName {
			txt := f.Doc.Text()
			if strings.TrimSpace(txt) != "" {
				return txt
			}
		}
	}
	return ""
}

// commentsToText converts a slice of []*ast.CommentGroup to a flat string,
// ensuring whitespace-only comment groups are dropped.
func commentsToText(cgroups []*ast.CommentGroup) (text string) {
	for _, c := range cgroups {
		if strings.TrimSpace(c.Text()) != "" {
			text += c.Text()
		}
	}
	return text
}

// prettyPrintTypesString is pretty printing specific to the output of
// types.*String. Instead of re-implementing the printer, we can just
// transform its output.
func prettyPrintTypesString(s string) string {
	// Don't bother including the fields if it is empty
	if strings.HasSuffix(s, "{}") {
		return ""
	}
	var b bytes.Buffer
	b.Grow(len(s))
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ';':
			b.WriteByte('\n')
			for j := 0; j < depth; j++ {
				b.WriteString("    ")
			}
			// Skip following space
			i++

		case '{':
			if i == len(s)-1 {
				// This should never happen, but in case it
				// does give up
				return s
			}

			n := s[i+1]
			if n == '}' {
				// Do not modify {}
				b.WriteString("{}")
				// We have already written }, so skip
				i++
			} else {
				// We expect fields to follow, insert a newline and space
				depth++
				b.WriteString(" {\n")
				for j := 0; j < depth; j++ {
					b.WriteString("    ")
				}
			}

		case '}':
			depth--
			if depth < 0 {
				return s
			}
			b.WriteString("\n}")

		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// packageForFile returns the import path and pkg from pkgs that contains the
// named file.
func packageForFile(pkgs map[string]*ast.Package, filename string) (string, *ast.Package, error) {
	for path, pkg := range pkgs {
		for pkgFile := range pkg.Files {
			if pkgFile == filename {
				return path, pkg, nil
			}
		}
	}
	return "", nil, fmt.Errorf("failed to find %q in packages %q", filename, pkgs)
}

// inRange tells if x is in the range of a-b inclusive.
func inRange(x, a, b token.Position) bool {
	if !util.PathEqual(x.Filename, a.Filename) || !util.PathEqual(x.Filename, b.Filename) {
		return false
	}
	return x.Offset >= a.Offset && x.Offset <= b.Offset
}

// findDocTarget walks an input *doc.Package and locates the *doc.Value,
// *doc.Type, or *doc.Func for the given target position.
func findDocTarget(fset *token.FileSet, target token.Position, in interface{}) interface{} {
	switch v := in.(type) {
	case *doc.Package:
		for _, x := range v.Consts {
			if r := findDocTarget(fset, target, x); r != nil {
				return r
			}
		}
		for _, x := range v.Types {
			if r := findDocTarget(fset, target, x); r != nil {
				return r
			}
		}
		for _, x := range v.Vars {
			if r := findDocTarget(fset, target, x); r != nil {
				return r
			}
		}
		for _, x := range v.Funcs {
			if r := findDocTarget(fset, target, x); r != nil {
				return r
			}
		}
		return nil
	case *doc.Value:
		if inRange(target, fset.Position(v.Decl.Pos()), fset.Position(v.Decl.End())) {
			return v
		}
		return nil
	case *doc.Type:
		if inRange(target, fset.Position(v.Decl.Pos()), fset.Position(v.Decl.End())) {
			return v
		}
		for _, x := range v.Consts {
			if r := findDocTarget(fset, target, x); r != nil {
				return r
			}
		}
		for _, x := range v.Vars {
			if r := findDocTarget(fset, target, x); r != nil {
				return r
			}
		}
		for _, x := range v.Funcs {
			if r := findDocTarget(fset, target, x); r != nil {
				return r
			}
		}
		for _, x := range v.Methods {
			if r := findDocTarget(fset, target, x); r != nil {
				return r
			}
		}
		return nil
	case *doc.Func:
		if inRange(target, fset.Position(v.Decl.Pos()), fset.Position(v.Decl.End())) {
			return v
		}
		return nil
	default:
		panic("unreachable")
	}
}

// fmtDocObject formats one of:
//
// *doc.Value
// *doc.Type
// *doc.Func
//
func fmtDocObject(fset *token.FileSet, x interface{}, target token.Position) ([]lsp.MarkedString, ast.Node) {
	switch v := x.(type) {
	case *doc.Value: // Vars and Consts
		// Sort the specs by distance to find the one nearest to target.
		sort.Sort(byDistance{v.Decl.Specs, fset, target})
		spec := v.Decl.Specs[0].(*ast.ValueSpec)

		// Use the doc directly above the var inside a var() block, or if there
		// is none, fall back to the doc directly above the var() block.
		doc := spec.Doc.Text()
		if doc == "" {
			doc = v.Doc
		}

		// Create a copy of the spec with no doc for formatting separately.
		cpy := *spec
		cpy.Doc = nil
		value := v.Decl.Tok.String() + " " + fmtNode(fset, &cpy)
		return maybeAddComments(doc, []lsp.MarkedString{{Language: "go", Value: value}}), spec

	case *doc.Type: // Type declarations
		spec := v.Decl.Specs[0].(*ast.TypeSpec)

		// Handle interfaces methods and struct fields separately now.
		switch s := spec.Type.(type) {
		case *ast.InterfaceType:
			// Find the method that is an exact match for our target position.
			for _, field := range s.Methods.List {
				if fset.Position(field.Pos()).Offset == target.Offset {
					// An exact match.
					value := fmt.Sprintf("func (%s).%s%s", spec.Name.Name, field.Names[0].Name, strings.TrimPrefix(fmtNode(fset, field.Type), "func"))
					return maybeAddComments(field.Doc.Text(), []lsp.MarkedString{{Language: "go", Value: value}}), field
				}
			}

		case *ast.StructType:
			// Find the field that is an exact match for our target position.
			for _, field := range s.Fields.List {
				if fset.Position(field.Pos()).Offset == target.Offset {
					// An exact match.
					value := fmt.Sprintf("struct field %s %s", field.Names[0], fmtNode(fset, field.Type))
					// Concat associated documentation with any inline comments
					comments := joinCommentGroups(field.Doc, field.Comment)
					return maybeAddComments(comments, []lsp.MarkedString{{Language: "go", Value: value}}), field
				}
			}
		}

		// Formatting of all type declarations: structs, interfaces, integers, etc.
		name := v.Decl.Tok.String() + " " + spec.Name.Name + " " + typeName(fset, spec.Type)
		res := []lsp.MarkedString{{Language: "go", Value: name}}

		doc := spec.Doc.Text()
		if doc == "" {
			doc = v.Doc
		}
		res = maybeAddComments(doc, res)

		if n := typeName(fset, spec.Type); n == "interface" || n == "struct" {
			res = append(res, lsp.MarkedString{Language: "go", Value: fmtNode(fset, spec.Type)})
		}
		return res, spec

	case *doc.Func: // Functions
		return maybeAddComments(v.Doc, []lsp.MarkedString{{Language: "go", Value: fmtNode(fset, v.Decl)}}), v.Decl
	default:
		panic("unreachable")
	}
}

// typeName returns the name of typ, shortening interface and struct types to
// just "interface" and "struct" rather than their full contents (incl. methods
// and fields).
func typeName(fset *token.FileSet, typ ast.Expr) string {
	switch typ.(type) {
	case *ast.InterfaceType:
		return "interface"
	case *ast.StructType:
		return "struct"
	default:
		return fmtNode(fset, typ)
	}
}

// fmtNode formats the given node as a string.
func fmtNode(fset *token.FileSet, n ast.Node) string {
	var buf bytes.Buffer
	err := format.Node(&buf, fset, n)
	if err != nil {
		panic("unreachable")
	}
	return buf.String()
}

// byDistance sorts specs by distance to the target position.
type byDistance struct {
	specs  []ast.Spec
	fset   *token.FileSet
	target token.Position
}

func (b byDistance) Len() int      { return len(b.specs) }
func (b byDistance) Swap(i, j int) { b.specs[i], b.specs[j] = b.specs[j], b.specs[i] }
func (b byDistance) Less(ii, jj int) bool {
	i := b.fset.Position(b.specs[ii].Pos())
	j := b.fset.Position(b.specs[jj].Pos())
	return abs(b.target.Offset-i.Offset) < abs(b.target.Offset-j.Offset)
}

// abs returns the absolute value of x.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
