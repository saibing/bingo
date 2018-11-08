package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/caches"
	"go/build"
	"go/scanner"
	"go/token"
	"golang.org/x/tools/go/packages"
	"path"
	"path/filepath"
	"strings"

	"github.com/opentracing/opentracing-go"

	"github.com/saibing/bingo/langserver/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

// buildPackageForNamedFileInMultiPackageDir returns a package that
// refer to the package named by filename. If there are multiple
// (e.g.) main packages in a dir in separate files, this lets you
// synthesize a *packages.Package that just refers to one. It's necessary
// to handle that case.
func buildPackageForNamedFileInMultiPackageDir(bpkg *packages.Package, m *build.MultiplePackageError, filename string) (*packages.Package, error) {
	copy := *bpkg
	bpkg = &copy

	// First, find which package name each filename is in.
	fileToPkgName := make(map[string]string, len(m.Files))
	for i, f := range m.Files {
		fileToPkgName[f] = m.Packages[i]
	}

	pkgName := fileToPkgName[filename]
	if pkgName == "" {
		return nil, fmt.Errorf("package %q in %s has no file %q", bpkg.PkgPath, filepath.Dir(filename), filename)
	}

	filterToFilesInPackage := func(files []string, pkgName string) []string {
		var keep []string
		for _, f := range files {
			if fileToPkgName[f] == pkgName {
				keep = append(keep, f)
			}
		}
		return keep
	}

	// Trim the *GoFiles fields to only those files in the same
	// package.
	bpkg.Name = pkgName
	if pkgName == "main" {
		// TODO(sqs): If the package name is "main", and there are
		// multiple main packages that are separate programs (and,
		// e.g., expected to be run directly run `go run main1.go
		// main2.go`), then this will break because it will try to
		// compile them all together. There's no good way to handle
		// that case that I can think of, other than with heuristics.
	}
	var nonXTestPkgName string
	if strings.HasSuffix(pkgName, "_test") {
		nonXTestPkgName = strings.TrimSuffix(pkgName, "_test")
	} else {
		nonXTestPkgName = pkgName
	}
	bpkg.GoFiles = filterToFilesInPackage(bpkg.GoFiles, nonXTestPkgName)
	return bpkg, nil
}

func isMultiplePackageError(err error) bool {
	_, ok := err.(*build.MultiplePackageError)
	return ok
}


type cursorToken struct {
	pos token.Pos
	tok token.Token
	lit string
}

func (h *LangHandler) loadPackage(ctx context.Context, conn jsonrpc2.JSONRPC2, fileURI lsp.DocumentURI, position lsp.Position) (*packages.Package, cursorToken, error) {
	parentSpan := opentracing.SpanFromContext(ctx)
	span := parentSpan.Tracer().StartSpan("langserver-go: load program",
		opentracing.Tags{"fileURI": fileURI},
		opentracing.ChildOf(parentSpan.Context()),
	)
	ctx = opentracing.ContextWithSpan(ctx, span)
	defer span.Finish()

	ctok := cursorToken{}
	if !util.IsURI(fileURI) {
		return nil, ctok, fmt.Errorf("typechecking of out-of-workspace URI (%q) is not yet supported", fileURI)
	}

	filename := h.FilePath(fileURI)

	bctx := h.BuildContext(ctx)
	pkg, err := h.load(ctx, bctx, conn, filename)
	if mpErr, ok := err.(*build.MultiplePackageError); ok {
		pkg, err = buildPackageForNamedFileInMultiPackageDir(pkg, mpErr, path.Base(filename))
		if err != nil {
			return nil, ctok, err
		}
	} else if err != nil {
		return nil, ctok, err
	}

	if pkg == nil {
		return nil, ctok, fmt.Errorf("%s does not exist", filename)
	}


	ctok, err = h.startPos(ctx, pkg, fileURI, position)
	return pkg, ctok, err
	//isIgnoredFile := true
	//for _, f := range pkg.CompiledGoFiles {
	//	if path.Base(filename) == path.Base(f) {
	//		isIgnoredFile = false
	//		break
	//	}
	//}
	//
	//if isIgnoredFile {
	//	return nil, ctok, fmt.Errorf("file %s is ignored by the build", filename)
	//}

	// collect all loaded files, required to remove existing diagnostics from our cache
	//files := fsetToFiles(pkg.Fset)
	//if err := h.publishDiagnostics(ctx, conn, error2Diagnostics(pkg.Errors), files); err != nil {
	//	log.Printf("warning: failed to send diagnostics: %s.", err)
	//}
}

func (h *LangHandler) loadRealTimePackage(ctx context.Context, conn jsonrpc2.JSONRPC2, fileURI lsp.DocumentURI, position lsp.Position) (*packages.Package,cursorToken, error) {
	parentSpan := opentracing.SpanFromContext(ctx)
	span := parentSpan.Tracer().StartSpan("langserver-go: load program",
		opentracing.Tags{"fileURI": fileURI},
		opentracing.ChildOf(parentSpan.Context()),
	)
	ctx = opentracing.ContextWithSpan(ctx, span)
	defer span.Finish()

	ctok := cursorToken{}
	if !util.IsURI(fileURI) {
		return nil, ctok, fmt.Errorf("typechecking of out-of-workspace URI (%q) is not yet supported", fileURI)
	}

	filename := h.FilePath(fileURI)

	bctx := h.BuildContext(ctx)
	pkg, err := h.loadRealTime(ctx, bctx, conn, filename)
	if mpErr, ok := err.(*build.MultiplePackageError); ok {
		pkg, err = buildPackageForNamedFileInMultiPackageDir(pkg, mpErr, path.Base(filename))
		if err != nil {
			return nil, ctok, err
		}
	} else if err != nil {
		return nil, ctok, err
	}

	ctok, err = h.startPos(ctx, pkg, fileURI, position)
	return pkg, ctok, err
}

func (h *LangHandler) startPos(ctx context.Context, pkg *packages.Package, fileURI lsp.DocumentURI, position lsp.Position) (cursorToken, error) {

	ctok := cursorToken{pos:token.NoPos}

	contents, err := h.readFile(ctx, fileURI)
	if err != nil {
		return ctok, err
	}

	filename := h.FilePath(fileURI)
	offset, valid, why := offsetForPosition(contents, position)
	if !valid {
		return ctok, fmt.Errorf("invalid position: %s:%d:%d (%s)", filename, position.Line, position.Character, why)
	}

	ctok.pos = util.PosForFileOffset(pkg.Fset, filename, offset)
	if ctok.pos == token.NoPos {
		return ctok, fmt.Errorf("invalid location: %s:#%d", filename, offset)
	}

	ctok.tok, ctok.lit = scanCursorToken(contents, offset)
	return ctok, nil
}

func scanCursorToken(file []byte, offset int) (token.Token, string) {
	fset := token.NewFileSet()
	var s scanner.Scanner
	s.Init(fset.AddFile("", fset.Base(), len(file)), file, nil, scanner.ScanComments)

	var foundTok token.Token
	var foundLit string
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF || fset.Position(pos).Offset >= offset {
			break
		}

		foundTok = tok
		foundLit = lit
	}

	return foundTok, foundLit
}


func (h *LangHandler) loadRealTime(ctx context.Context, bctx *build.Context, conn jsonrpc2.JSONRPC2, filename string) (*packages.Package, error) {
	pkgDir := filename
	if !bctx.IsDir(filename) {
		pkgDir = path.Dir(filename)
	}

	cfg := &packages.Config{Mode: packages.LoadSyntax, Context: ctx, Tests: true}
	pkgList, err := packages.Load(cfg, caches.GetLoadDir(pkgDir))
	if err != nil {
		return nil, err
	}

	return pkgList[0], nil
}

// ContainingPackageModule returns the package that contains the given
// filename. It is like buildutil.ContainingPackage, except that:
//
// * it returns the whole package (i.e., it doesn't use build.FindOnly)
// * it does not perform FS calls that are unnecessary for us (such
//   as searching the GOROOT; this is only called on the main
//   workspace's code, not its deps).
// * if the file is in the xtest package (package p_test not package p),
//   it returns build.Package only representing that xtest package
func (h *LangHandler) load(ctx context.Context, bctx *build.Context, conn jsonrpc2.JSONRPC2, filename string) (*packages.Package, error) {
	pkgDir := filename
	if !bctx.IsDir(filename) {
		pkgDir = path.Dir(filename)
	}

	return h.packageCache.Load(ctx, conn, pkgDir, nil)
}