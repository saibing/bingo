package langserver

import (
	"context"
	"fmt"
	"go/build"
	"go/token"
	"golang.org/x/tools/go/packages"
	"path"
	"path/filepath"
	"strings"

	"github.com/opentracing/opentracing-go"

	"github.com/saibing/bingo/langserver/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"

	"golang.org/x/tools/go/loader"
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

// TODO(sqs): allow typechecking just a specific file not in a package, too
func typecheck(ctx context.Context, fset *token.FileSet, bctx *build.Context, bpkg *build.Package, findPackage FindPackageFunc) (*loader.Program, diagnostics, error) {

	return nil, nil, nil
}

func isMultiplePackageError(err error) bool {
	_, ok := err.(*build.MultiplePackageError)
	return ok
}

func (h *LangHandler) loadPackage(ctx context.Context, conn jsonrpc2.JSONRPC2, fileURI lsp.DocumentURI, position lsp.Position) (*packages.Package, token.Pos, error) {
	parentSpan := opentracing.SpanFromContext(ctx)
	span := parentSpan.Tracer().StartSpan("langserver-go: load program",
		opentracing.Tags{"fileURI": fileURI},
		opentracing.ChildOf(parentSpan.Context()),
	)
	ctx = opentracing.ContextWithSpan(ctx, span)
	defer span.Finish()

	start := token.NoPos
	if !util.IsURI(fileURI) {
		return nil, start, fmt.Errorf("typechecking of out-of-workspace URI (%q) is not yet supported", fileURI)
	}

	filename := h.FilePath(fileURI)

	bctx := h.BuildContext(ctx)
	pkg, err := h.load(ctx, bctx, conn, filename)
	if mpErr, ok := err.(*build.MultiplePackageError); ok {
		pkg, err = buildPackageForNamedFileInMultiPackageDir(pkg, mpErr, path.Base(filename))
		if err != nil {
			return nil, start, err
		}
	} else if err != nil {
		return nil, start, err
	}

	//isIgnoredFile := true
	//for _, f := range pkg.CompiledGoFiles {
	//	if path.Base(filename) == path.Base(f) {
	//		isIgnoredFile = false
	//		break
	//	}
	//}
	//
	//if isIgnoredFile {
	//	return nil, start, fmt.Errorf("file %s is ignored by the build", filename)
	//}

	// collect all loaded files, required to remove existing diagnostics from our cache
	//files := fsetToFiles(pkg.Fset)
	//if err := h.publishDiagnostics(ctx, conn, error2Diagnostics(pkg.Errors), files); err != nil {
	//	log.Printf("warning: failed to send diagnostics: %s.", err)
	//}

	contents, err := h.readFile(ctx, fileURI)
	if err != nil {
		return nil, start, err
	}
	offset, valid, why := offsetForPosition(contents, position)
	if !valid {
		return nil, start, fmt.Errorf("invalid position: %s:%d:%d (%s)", filename, position.Line, position.Character, why)
	}

	start = util.PosForFileOffset(pkg.Fset, filename, offset)
	if start == token.NoPos {
		return nil, start, fmt.Errorf("invalid location: %s:#%d", filename, offset)
	}

	return pkg, start, nil
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

	return h.packageCache.Load(ctx, conn, pkgDir, h.overlay.m)
}