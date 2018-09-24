package langserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/saibing/bingo/langserver/internal/caches"
	"golang.org/x/tools/go/packages"

	"golang.org/x/tools/go/buildutil"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/refactor/importgraph"

	"github.com/opentracing/opentracing-go"
	"github.com/saibing/bingo/langserver/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/saibing/bingo/pkg/lspext"
	"github.com/saibing/bingo/pkg/tools"
	"github.com/sourcegraph/jsonrpc2"
)

func (h *LangHandler) handleTextDocumentReferences(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lsp.ReferenceParams) ([]lsp.Location, error) {
	if !util.IsURI(params.TextDocument.URI) {
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: fmt.Sprintf("textDocument/references not yet supported for out-of-workspace URI (%q)", params.TextDocument.URI),
		}
	}

	pkg, start, err := h.loadPackage(ctx, conn, params.TextDocument.URI, params.Position)
	if err != nil {
		// Invalid nodes means we tried to click on something which is
		// not an ident (eg comment/string/etc). Return no information.
		if _, ok := err.(*invalidNodeError); ok {
			return []lsp.Location{}, nil
		}
		return nil, err
	}

	_, node, err := getPathNode(pkg, start, start)
	if err != nil {
		return nil, err
	}

	// NOTICE: Code adapted from golang.org/x/tools/cmd/guru
	// referrers.go.
	obj := pkg.TypesInfo.ObjectOf(node)
	if obj == nil {
		return nil, errors.New("references object not found")
	}

	if obj.Pkg() == nil {
		if _, builtin := obj.(*types.Builtin); builtin {
			// We don't support builtin references due to the massive number
			// of references, so ignore the missing package error.
			return []lsp.Location{}, nil
		}
		return nil, fmt.Errorf("no package found for object %s", obj)
	}
	defPkg := strings.TrimSuffix(obj.Pkg().Path(), "_test")

	pkgInWorkspace := func(path string) bool {
		if h.init.RootImportPath == "" {
			return true
		}
		return util.PathHasPrefix(path, h.init.RootImportPath)
	}

	// findRefCtx is used in the findReferences function. It has its own
	// context so we can stop finding references once we have reached our
	// limit.
	findRefCtx, stop := context.WithCancel(ctx)
	defer stop()

	var (
		// locsC receives the final collected references via
		// refStreamAndCollect.
		locsC = make(chan []lsp.Location)

		// refs is a stream of raw references found by findReferences or findReferencesPkgLevel.
		refs = make(chan *ast.Ident)

		// findRefErr is non-nil if findReferences fails.
		findRefErr error
	)

	// Start a goroutine to read from the refs chan. It will read all the
	// refs until the chan is closed. It is responsible to stream the
	// references back to the client, as well as build up the final slice
	// which we return as the response.
	go func() {
		locsC <- refStreamAndCollect(ctx, conn, req, pkg.Fset, refs, params.Context.XLimit, stop)
		close(locsC)
	}()

	// Don't include declare if it is outside of workspace.
	if params.Context.IncludeDeclaration && util.PathHasPrefix(defPkg, h.init.RootImportPath) {
		refs <- &ast.Ident{NamePos: obj.Pos(), Name: obj.Name()}
	}

	findRefErr = findReferences(findRefCtx, pkg.Fset, h.packageCache, pkgInWorkspace, obj, refs)
	if findRefErr != nil {
		// If we are canceled, cancel loop early
		return nil, findRefErr
	}

	// Tell refStreamAndCollect that we are done finding references. It
	// will then send the all the collected references to locsC.
	close(refs)
	locs := <-locsC

	// If we find references then we can ignore findRefErr. It should only
	// be non-nil due to timeouts or our last findReferences doesn't find
	// the def.
	if len(locs) == 0 && findRefErr != nil {
		return nil, findRefErr
	}

	if locs == nil {
		locs = []lsp.Location{}
	}

	return locs, nil
}

// reverseImportGraph returns the reversed import graph for the workspace
// under the RootPath. Computing the reverse import graph is IO intensive, as
// such we may send down more than one import graph. The later a graph is
// sent, the more accurate it is. The channel will be closed, and the last
// graph sent is accurate. The reader does not have to read all the values.
func (h *LangHandler) reverseImportGraph(ctx context.Context, conn jsonrpc2.JSONRPC2) <-chan importgraph.Graph {
	// Ensure our buffer is big enough to prevent deadlock
	c := make(chan importgraph.Graph, 2)

	go func() {
		// This should always be related to the go import path for
		// this repo. For sourcegraph.com this means we share the
		// import graph across commits. We want this behaviour since
		// we assume that they don't change drastically across
		// commits.
		cacheKey := "importgraph:" + string(h.init.Root())

		h.mu.Lock()
		tryCache := h.importGraph == nil
		once := h.importGraphOnce
		h.mu.Unlock()
		if tryCache {
			g := make(importgraph.Graph)
			if hit := h.cacheGet(ctx, conn, cacheKey, g); hit {
				// \o/
				c <- g
			}
		}

		parentCtx := ctx
		once.Do(func() {
			// Note: We use a background context since this
			// operation should not be cancelled due to an
			// individual request.
			span := startSpanFollowsFromContext(parentCtx, "BuildReverseImportGraph")
			ctx := opentracing.ContextWithSpan(context.Background(), span)
			defer span.Finish()

			bctx := h.BuildContext(ctx)
			findPackageWithCtx := h.getFindPackageFunc()
			findPackage := func(bctx *build.Context, importPath, fromDir string, mode build.ImportMode) (*build.Package, error) {
				return findPackageWithCtx(ctx, bctx, importPath, fromDir, mode)
			}
			g := tools.BuildReverseImportGraph(bctx, findPackage, h.FilePath(h.init.Root()))
			h.mu.Lock()
			h.importGraph = g
			h.mu.Unlock()

			// Update cache in background
			go h.cacheSet(ctx, conn, cacheKey, g)
		})
		h.mu.Lock()
		// TODO(keegancsmith) h.importGraph may have been reset after once
		importGraph := h.importGraph
		h.mu.Unlock()
		c <- importGraph

		close(c)
	}()

	return c
}

// refStreamAndCollect returns all refs read in from chan until it is
// closed. While it is reading, it will also occasionally stream out updates of
// the refs received so far.
func refStreamAndCollect(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, fset *token.FileSet, refs <-chan *ast.Ident, limit int, stop func()) []lsp.Location {
	if limit == 0 {
		// If we don't have a limit, just set it to a value we should never exceed
		limit = math.MaxInt32
	}

	id := lsp.ID{
		Num:      req.ID.Num,
		Str:      req.ID.Str,
		IsString: req.ID.IsString,
	}
	initial := json.RawMessage(`[{"op":"replace","path":"","value":[]}]`)
	_ = conn.Notify(ctx, "$/partialResult", &lspext.PartialResultParams{
		ID:    id,
		Patch: &initial,
	})

	var (
		locs []lsp.Location
		pos  int
	)
	send := func() {
		if pos >= len(locs) {
			return
		}
		patch := make([]referenceAddOp, 0, len(locs)-pos)
		for _, l := range locs[pos:] {
			patch = append(patch, referenceAddOp{
				OP:    "add",
				Path:  "/-",
				Value: l,
			})
		}
		pos = len(locs)
		_ = conn.Notify(ctx, "$/partialResult", &lspext.PartialResultParams{
			ID: id,
			// We use referencePatch so the build server can rewrite URIs
			Patch: referencePatch(patch),
		})
	}

	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case n, ok := <-refs:
			if !ok {
				// send a final update
				send()
				return locs
			}
			if len(locs) >= limit {
				stop()
				continue
			}
			locs = append(locs, goRangeToLSPLocation(fset, n.Pos(), n.End()))
		case <-tick.C:
			send()
		}
	}
}

// findReferences will find all references to obj. It will only return
// references from packages in pkg.Imports.
func findReferences(ctx context.Context, fset *token.FileSet, packageCache *caches.PackageCache, pkgInWorkspace func(string) bool, obj types.Object, refs chan<- *ast.Ident) error {
	// Bail out early if the context is canceled
	if ctx.Err() != nil {
		return ctx.Err()
	}

	defPkg := strings.TrimSuffix(obj.Pkg().Path(), "_test")
	objPos := fset.Position(obj.Pos())

	// The remainder of this function is somewhat tricky because it
	// operates on the concurrent stream of packages observed by the
	// loader's AfterTypeCheck hook.

	var (
		mu   sync.Mutex
		qobj types.Object
	)

	f := func(pkg *packages.Package) error {
		collectPkg := pkgInWorkspace

		if _, ok := pkg.Imports[defPkg]; !ok && pkg.PkgPath != defPkg {
			return nil
		}

		// Record the query object and its package when we see
		// it. We can't reuse obj from the initial typecheck
		// because each go/loader Load invocation creates new
		// objects, and we need to test for equality later when we
		// look up refs.
		mu.Lock()
		if qobj == nil && pkg.PkgPath == defPkg {
			// Find the object by its position (slightly ugly).
			qobj = findObject(pkg.Fset, pkg.TypesInfo, objPos)
			if qobj == nil {
				// It really ought to be there; we found it once
				// already.
				return fmt.Errorf("object at %s not found in package %s", objPos, defPkg)
			}
		}
		queryObj := qobj
		mu.Unlock()

		// Look for references to the query object. Only collect
		// those that are in this workspace.
		if queryObj != nil && collectPkg(pkg.PkgPath) {
			for id, obj := range pkg.TypesInfo.Uses {
				if sameObj(queryObj, obj) {
					refs <- id
				}
			}
		}

		return nil
	}

	err := packageCache.Iterate(f)
	if err != nil {
		return err
	}

	if qobj == nil {
		return errors.New("query object not found during reloading")
	}

	return nil
}


// classify classifies objects by how far
// we have to look to find references to them.
func classify(obj types.Object) (global, pkglevel bool) {
	if obj.Exported() {
		if obj.Parent() == nil {
			// selectable object (field or method)
			return true, false
		}
		if obj.Parent() == obj.Pkg().Scope() {
			// lexical object (package-level var/const/func/type)
			return true, true
		}
	}
	// object with unexported named or defined in local scope
	return false, false
}

// allowErrors causes type errors to be silently ignored.
// (Not suitable if SSA construction follows.)
//
// NOTICE: Adapted from golang.org/x/tools.
func allowErrors(lconf *loader.Config) {
	ctxt := *lconf.Build // copy
	ctxt.CgoEnabled = false
	lconf.Build = &ctxt
	lconf.AllowErrors = true
	// AllErrors makes the parser always return an AST instead of
	// bailing out after 10 errors and returning an empty ast.File.
	lconf.ParserMode = parser.AllErrors
	lconf.TypeChecker.Error = func(err error) {}
}

// findObject returns the object defined at the specified position.
func findObject(fset *token.FileSet, info *types.Info, objposn token.Position) types.Object {
	good := func(obj types.Object) bool {
		if obj == nil {
			return false
		}
		pos := fset.Position(obj.Pos())
		return pos.Filename == objposn.Filename && pos.Offset == objposn.Offset
	}
	for _, obj := range info.Defs {
		if good(obj) {
			return obj
		}
	}
	for _, obj := range info.Implicits {
		if good(obj) {
			return obj
		}
	}
	return nil
}

func usesOf(queryObj types.Object, info *loader.PackageInfo) []*ast.Ident {
	var refs []*ast.Ident
	for id, obj := range info.Uses {
		if sameObj(queryObj, obj) {
			refs = append(refs, id)
		}
	}
	return refs
}

// same reports whether x and y are identical, or both are PkgNames
// that import the same Package.
func sameObj(x, y types.Object) bool {
	if x == y {
		return true
	}
	if x, ok := x.(*types.PkgName); ok {
		if y, ok := y.(*types.PkgName); ok {
			return x.Imported() == y.Imported()
		}
	}
	return false
}

// readFile is like ioutil.ReadFile, but
// it goes through the virtualized build.Context.
// If non-nil, buf must have been reset.
func readFile(ctxt *build.Context, filename string, buf *bytes.Buffer) ([]byte, error) {
	rc, err := buildutil.OpenFile(ctxt, filename)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if buf == nil {
		buf = new(bytes.Buffer)
	}
	if _, err := io.Copy(buf, rc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
