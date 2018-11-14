package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/langserver/internal/goast"
	"go/build"
	"go/token"
	"golang.org/x/tools/go/packages"
	"path"
	"path/filepath"
	"strings"

	"github.com/saibing/bingo/langserver/internal/util"

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

func (h *LangHandler) loadFromGlobalCache(ctx context.Context, conn jsonrpc2.JSONRPC2,  fileURI lsp.DocumentURI, position lsp.Position) (*packages.Package, token.Pos, error) {
	pos := token.NoPos

	if !util.IsURI(fileURI) {
		return nil, pos, fmt.Errorf("typechecking of out-of-workspace URI (%q) is not yet supported", fileURI)
	}

	filename := h.FilePath(fileURI)

	pkg, err := h.load(ctx, conn, filename)
	if mpErr, ok := err.(*build.MultiplePackageError); ok {
		pkg, err = buildPackageForNamedFileInMultiPackageDir(pkg, mpErr, path.Base(filename))
		if err != nil {
			return nil, pos, err
		}
	} else if err != nil {
		return nil, pos, err
	}

	if pkg == nil {
		return nil, pos, fmt.Errorf("%s does not exist", filename)
	}

	pos, err = h.startPos(ctx, pkg, fileURI, position)
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

func (h *LangHandler) load(ctx context.Context, conn jsonrpc2.JSONRPC2, filename string) (*packages.Package, error) {
	return h.globalCache.Load(ctx, conn, path.Dir(filename), nil)
}