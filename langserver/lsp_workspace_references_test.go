package langserver

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/sourcegraph/go-lsp/lspext"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func matchDir(path string) string {
	dir := makePath(exported.Config.Dir, path)
	return string(util.PathToURI(dir))
}

func TestWorkspaceReferences(t *testing.T) {
	setup(t)

	test := func(t *testing.T, data map[*lspext.WorkspaceReferencesParams][]string) {
		for k, v := range data {
			testWorkspaceReferences(t, &workspaceReferencesTestCase{input: k, output: v})
		}
	}

	t.Run("xtest workspace references", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			{Query: lspext.SymbolDescriptor{}}: {
				"xtest/x_test.go:1:24-1:76 -> id:github.com/saibing/bingo/langserver/test/pkg/xtest name: package:github.com/saibing/bingo/langserver/test/pkg/xtest packageName:p recv: vendor:false",
				"xtest/x_test.go:1:88-1:89 -> id:github.com/saibing/bingo/langserver/test/pkg/xtest/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/xtest packageName:p recv: vendor:false",
			},
		})
	})

	t.Run("subdirectory workspace references", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			// Non-matching name query.
			{Query: lspext.SymbolDescriptor{"name": "nope"}}: {},

			// Matching against invalid field name.
			{Query: lspext.SymbolDescriptor{"nope": "A"}}: {},

			// Matching against an invalid dirs hint.
			{Query: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/subdirectory"}, Hints: map[string]interface{}{"dirs": []string{matchDir("subdirectory/d3")}}}: {},

			// Matching against a dirs hint with multiple dirs.
			{Query: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/subdirectory"}, Hints: map[string]interface{}{"dirs": []string{matchDir("subdirectory/d2"), matchDir("subdirectory/invalid")}}}: {
				"subdirectory/d2/b.go:1:20-1:79 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory name: package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false",
				"subdirectory/d2/b.go:1:94-1:95 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false",
			},

			// Matching against a dirs hint.
			{Query: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/subdirectory"}, Hints: map[string]interface{}{"dirs": []string{matchDir("subdirectory/d2")}}}: {
				"subdirectory/d2/b.go:1:20-1:79 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory name: package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false",
				"subdirectory/d2/b.go:1:94-1:95 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false",
			},

			// Matching against single field.
			{Query: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/subdirectory"}}: {
				"subdirectory/d2/b.go:1:20-1:79 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory name: package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false",
				"subdirectory/d2/b.go:1:94-1:95 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false",
			},

			// Matching against no fields.
			{Query: lspext.SymbolDescriptor{}}: {
				"subdirectory/d2/b.go:1:20-1:79 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory name: package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false",
				"subdirectory/d2/b.go:1:94-1:95 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false",
			},
			{
				Query: lspext.SymbolDescriptor{
					"name":        "",
					"package":     "github.com/saibing/bingo/langserver/test/pkg/subdirectory",
					"packageName": "d",
					"recv":        "",
					"vendor":      false,
				},
			}: {"subdirectory/d2/b.go:1:20-1:79 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory name: package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false"},
			{
				Query: lspext.SymbolDescriptor{
					"name":        "A",
					"package":     "github.com/saibing/bingo/langserver/test/pkg/subdirectory",
					"packageName": "d",
					"recv":        "",
					"vendor":      false,
				},
			}: {"subdirectory/d2/b.go:1:94-1:95 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false"},
		})
	})

	t.Run("go root", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			{Query: lspext.SymbolDescriptor{}}: {
				"goroot/a.go:1:19-1:24" + " -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
				"goroot/a.go:1:38-1:45" + " -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
			},
		})
	})

	t.Run("go project", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			{Query: lspext.SymbolDescriptor{}}: {
				"goproject/b/b.go:1:19-1:77 -> id:github.com/saibing/bingo/langserver/test/pkg/goproject/a name: package:github.com/saibing/bingo/langserver/test/pkg/goproject/a packageName:a recv: vendor:false",
				"goproject/b/b.go:1:89-1:90 -> id:github.com/saibing/bingo/langserver/test/pkg/goproject/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/goproject/a packageName:a recv: vendor:false",
			},
		})
	})

	t.Run("go module", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			{Query: lspext.SymbolDescriptor{}}: {
				"gomodule/a.go:1:19-1:43 -> id:github.com/saibing/dep name: package:github.com/saibing/dep packageName:dep recv: vendor:false",
				"gomodule/a.go:1:57-1:58 -> id:github.com/saibing/dep/-/D name:D package:github.com/saibing/dep packageName:dep recv: vendor:false",
				"gomodule/a.go:1:72-1:73 -> id:github.com/saibing/dep/-/D name:D package:github.com/saibing/dep packageName:dep recv: vendor:false",
				"gomodule/b.go:1:19-1:48 -> id:github.com/saibing/dep/subp name: package:github.com/saibing/dep/subp packageName:subp recv: vendor:false",
				"gomodule/b.go:1:63-1:64 -> id:github.com/saibing/dep/subp/-/D name:D package:github.com/saibing/dep/subp packageName:subp recv: vendor:false",
				"gomodule/c.go:1:19-1:48 -> id:github.com/saibing/dep/dep1 name: package:github.com/saibing/dep/dep1 packageName:dep1 recv: vendor:false",
				"gomodule/c.go:1:68-1:70 -> id:github.com/saibing/dep/dep2/-/D2/D2 name:D2 package:github.com/saibing/dep/dep2 packageName:dep2 recv:D2 vendor:false",
				"gomodule/c.go:1:63-1:65 -> id:github.com/saibing/dep/dep1/-/D1 name:D1 package:github.com/saibing/dep/dep1 packageName:dep1 recv: vendor:false",
			},
		})
	})

	t.Run("workspace references multiple files", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			{Query: lspext.SymbolDescriptor{}}: {
				"workspace_multiple/a.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
				"workspace_multiple/a.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
				"workspace_multiple/b.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
				"workspace_multiple/b.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
				"workspace_multiple/c.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
				"workspace_multiple/c.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
			},
		})
	})
}

type workspaceReferencesTestCase struct {
	input  *lspext.WorkspaceReferencesParams
	output []string
}

func testWorkspaceReferences(tb testing.TB, c *workspaceReferencesTestCase) {
	tbRun(tb, fmt.Sprintf("workspace-references-%s", c.input.Query), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testWorkspaceReferences", err)
		}
		doWorkspaceReferencesTest(t, ctx, conn, util.PathToURI(dir), *c.input, c.output)
	})
}

func doWorkspaceReferencesTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, params lspext.WorkspaceReferencesParams, want []string) {
	references, err := callWorkspaceReferences(ctx, c, params)
	if err != nil {
		t.Fatal(err)
	}

	var pkgDir string
	for i := range want {
		if i == 0 {
			splits := strings.Split(want[i], "/")
			pkgDir = splits[0]
		}
		want[i] = makePath(exported.Config.Dir, want[i])
	}

	rootDir := util.UriToRealPath(rootURI)
	var results []string
	for i := range references {
		if strings.Contains(references[i], "go-build") {
			continue
		}

		reference := util.UriToRealPath(lsp.DocumentURI(references[i]))
		prefix := filepath.Join(util.LowerDriver(rootDir), pkgDir)
		if strings.HasPrefix(reference, prefix) {
			results = append(results, filepath.ToSlash(reference))
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i] < results[j]
	})
	sort.Slice(want, func(i, j int) bool {
		return want[i] < want[j]
	})

	if len(results) == 0 && len(want) == 0 {
		return
	}

	if !reflect.DeepEqual(results, want) {
		t.Errorf("\ngot  %q\nwant %q", results, want)
	}
}

func callWorkspaceReferences(ctx context.Context, c *jsonrpc2.Conn, params lspext.WorkspaceReferencesParams) ([]string, error) {
	var references []lspext.ReferenceInformation
	err := c.Call(ctx, "workspace/xreferences", params, &references)
	if err != nil {
		return nil, err
	}
	refs := make([]string, len(references))
	for i, r := range references {
		locationURI := util.UriToPath(r.Reference.URI)
		start := r.Reference.Range.Start
		end := r.Reference.Range.End
		refs[i] = fmt.Sprintf("%s:%d:%d-%d:%d -> %v", locationURI, start.Line+1, start.Character+1, end.Line+1, end.Character+1, r.Symbol)
	}
	return refs, nil
}
