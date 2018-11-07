package langserver

import (
	"context"
	"fmt"
	"github.com/opentracing/opentracing-go"
	"go/build"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/saibing/bingo/langserver/internal/caches"
	"golang.org/x/tools/go/packages"

	"github.com/opentracing/opentracing-go/ext"
	"github.com/saibing/bingo/langserver/internal/refs"
	"github.com/saibing/bingo/pkg/lspext"
	"github.com/sourcegraph/jsonrpc2"
)

// workspaceReferencesTimeout is the timeout used for workspace/xreferences
// calls.
const workspaceReferencesTimeout = time.Minute

func (h *LangHandler) handleWorkspaceReferences(ctx context.Context, conn jsonrpc2.JSONRPC2, req *jsonrpc2.Request, params lspext.WorkspaceReferencesParams) ([]referenceInformation, error) {
	// TODO: Add support for the cancelRequest LSP method instead of using
	// hard-coded timeouts like this here.
	//
	// See: https://github.com/Microsoft/language-server-protocol/blob/master/protocol.md#cancelRequest
	ctx, cancel := context.WithTimeout(ctx, workspaceReferencesTimeout)
	defer cancel()
	rootPath := h.FilePath(h.init.Root())
	bctx := h.BuildContext(ctx)

	var results = refResult{results: make([]referenceInformation, 0)}
	f := func(pkg *packages.Package) error {
		err := h.workspaceRefsFromPkg(ctx, bctx, conn, params, pkg, rootPath, &results)
		if err != nil {
			log.Printf("workspaceRefsFromPkg: %v: %v", pkg, err)
		}
		return err
	}

	err := h.packageCache.Iterate(f)
	if err != nil {
		return nil, err
	}

	limit := params.Limit
	if limit <= 0 {
		// If we don't have a limit, just set it to a value we should never exceed
		limit = math.MaxInt32
	}

	results.resultsMu.Lock()
	r := results.results
	results.resultsMu.Unlock()
	if len(r) > limit {
		r = r[:limit]
	}

	return r, nil
}

// workspaceRefsFromPkg collects all the references made to dependencies from
// the specified package and returns the results.
func (h *LangHandler) workspaceRefsFromPkg(ctx context.Context, bctx *build.Context, conn jsonrpc2.JSONRPC2, params lspext.WorkspaceReferencesParams, pkg *packages.Package, rootPath string, results *refResult) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	span, ctx := opentracing.StartSpanFromContext(ctx, "workspaceRefsFromPkg")
	defer func() {
		if err != nil {
			ext.Error.Set(span, true)
			span.SetTag("err", err.Error())
		}
		span.Finish()
	}()
	span.SetTag("pkg", pkg)

	// Compute workspace references.
	findPackage := h.getFindModulePackageFunc()
	cfg := &refs.Config{
		FileSet:  pkg.Fset,
		Pkg:      pkg.Types,
		PkgFiles: pkg.Syntax,
		Info:     pkg.TypesInfo,
	}
	refsErr := cfg.Refs(func(r *refs.Ref) {
		symDesc, err := defSymbolDescriptor(ctx, conn, pkg, h.packageCache, rootPath, r.Def, findPackage)
		if err != nil {
			// Log the error, and flag it as one in the trace -- but do not
			// halt execution (hopefully, it is limited to a small subset of
			// the data).
			ext.Error.Set(span, true)
			err := fmt.Errorf("workspaceRefsFromPkg: failed to import %v: %v", r.Def.ImportPath, err)
			log.Println(err)
			span.SetTag("error", err.Error())
			return
		}
		if !symDesc.Contains(params.Query) {
			return
		}

		results.resultsMu.Lock()
		results.results = append(results.results, referenceInformation{
			Reference: goRangeToLSPLocation(pkg.Fset, r.Start, r.End),
			Symbol:    symDesc,
		})
		results.resultsMu.Unlock()
	})
	if refsErr != nil {
		// Trace the error, but do not consider it a true error. In many cases
		// it is a problem with the user's code, not our workspace reference
		// finding code.
		span.SetTag("err", fmt.Sprintf("workspaceRefsFromPkg: workspace refs failed: %v: %v", pkg, refsErr))
	}
	return nil
}

func defSymbolDescriptor(
	ctx context.Context,
	conn jsonrpc2.JSONRPC2,
	pkg *packages.Package,
	packageCache *caches.PackageCache,
	rootPath string, def refs.Def,
	findPackage FindModulePackageFunc) (*symbolDescriptor, error) {

	var err error
	defPkg, _ := pkg.Imports[def.ImportPath]
	if defPkg == nil {
		defPkg, err = findPackage(ctx, conn, packageCache, def.ImportPath, rootPath)
		if err != nil {
			return nil, err
		}
	}

	// NOTE: fields must be kept in sync with symbol.go:symbolEqual
	desc := &symbolDescriptor{
		Vendor:      false,
		Package:     defPkg.PkgPath,
		PackageName: def.PackageName,
		Recv:        "",
		Name:        "",
		ID:          "",
	}

	fields := strings.Fields(def.Path)
	switch {
	case len(fields) == 0:
		// reference to just a package
		desc.ID = fmt.Sprintf("%s", desc.Package)
	case len(fields) >= 2:
		desc.Recv = fields[0]
		desc.Name = fields[1]
		desc.ID = fmt.Sprintf("%s/-/%s/%s", desc.Package, desc.Recv, desc.Name)
	case len(fields) >= 1:
		desc.Name = fields[0]
		desc.ID = fmt.Sprintf("%s/-/%s", desc.Package, desc.Name)
	default:
		panic("invalid def.Path response from internal/refs")
	}
	return desc, nil
}

// refResult is a utility struct for collecting workspace reference results.
type refResult struct {
	results   []referenceInformation
	resultsMu sync.Mutex
}
