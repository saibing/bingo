package langserver

import (
	"context"
	"fmt"
	"golang.org/x/tools/go/packages/packagestest"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/saibing/bingo/pkg/lspext"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestWorkspaceReferences(t *testing.T) {
	exported = packagestest.Export(t, packagestest.Modules, testdata)
	defer exported.Cleanup()

	defer func() {
		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Fatal("conn.Close", err)
			}
		}
	}()

	initServer(exported.Config.Dir)

	test := func(t *testing.T, data map[*lspext.WorkspaceReferencesParams][]string) {
		for k, v := range data {
			testWorkspaceReferences(t, &workspaceReferencesTestCase{input: k, output: v})
		}
	}

	t.Run("xtest workspace references", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			{Query: lspext.SymbolDescriptor{}}: {
				"xtest/x_test.go:1:24-1:34 -> id:github.com/saibing/bingo/langserver/test/pkg/xtest name: package:github.com/saibing/bingo/langserver/test/pkg/xtest packageName:p recv: vendor:false",
				"xtest/x_test.go:1:46-1:47 -> id:github.com/saibing/bingo/langserver/test/pkg/xtest/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/xtest packageName:p recv: vendor:false",
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
			{Query: lspext.SymbolDescriptor{"package": "test/pkg/d"}, Hints: map[string]interface{}{"dirs": []string{"file:///src/test/pkg/d/d3"}}}: {},

			// Matching against a dirs hint with multiple dirs.
			{Query: lspext.SymbolDescriptor{"package": "test/pkg/d"}, Hints: map[string]interface{}{"dirs": []string{"file:///src/test/pkg/d/d2", "file:///src/test/pkg/d/invalid"}}}: {
				"subdirectory/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false",
				"subdirectory/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false",
			},

			// Matching against a dirs hint.
			{Query: lspext.SymbolDescriptor{"package": "test/pkg/d"}, Hints: map[string]interface{}{"dirs": []string{"file:///src/test/pkg/d/d2"}}}: {
				"subdirectory/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false",
				"subdirectory/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false",
			},

			// Matching against single field.
			{Query: lspext.SymbolDescriptor{"package": "test/pkg/d"}}: {
				"subdirectory/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false",
				"subdirectory/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false",
			},

			// Matching against no fields.
			{Query: lspext.SymbolDescriptor{}}: {
				"subdirectory/d/d2/b.go:1:20-1:32 -> id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d name: package:test/pkg/d packageName:d recv: vendor:false",
				"subdirectory/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false",
			},
			{
				Query: lspext.SymbolDescriptor{
					"name":        "",
					"package":     "test/pkg/d",
					"packageName": "d",
					"recv":        "",
					"vendor":      false,
				},
			}: {"subdirectory/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false"},
			{
				Query: lspext.SymbolDescriptor{
					"name":        "A",
					"package":     "test/pkg/d",
					"packageName": "d",
					"recv":        "",
					"vendor":      false,
				},
			}: {"subdirectory/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false"},
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
				"goproject/b/b.go:1:19-1:31 -> id:test/pkg/a name: package:test/pkg/a packageName:a recv: vendor:false",
				"goproject/b/b.go:1:43-1:44 -> id:test/pkg/a/-/A name:A package:test/pkg/a packageName:a recv: vendor:false",
			},
		})
	})

	t.Run("go module", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			{Query: lspext.SymbolDescriptor{}}: {
				"gomodule/a.go:1:19-1:37 -> id:github.com/d/dep name: package:github.com/d/dep packageName:dep recv: vendor:false",
				"gomodule/a.go:1:51-1:52 -> id:github.com/d/dep/-/D name:D package:github.com/d/dep packageName:dep recv: vendor:false",
				"gomodule/a.go:1:66-1:67 -> id:github.com/d/dep/-/D name:D package:github.com/d/dep packageName:dep recv: vendor:false",
			},
			{Query: lspext.SymbolDescriptor{}}: {
				"gomodule/a.go:1:19-1:42 -> id:github.com/d/dep/subp name: package:github.com/d/dep/subp packageName:subp recv: vendor:false",
				"gomodule/a.go:1:57-1:58 -> id:github.com/d/dep/subp/-/D name:D package:github.com/d/dep/subp packageName:subp recv: vendor:false",
			},
			{Query: lspext.SymbolDescriptor{}}: {
				"gomodule/a.go:1:19-1:38 -> id:github.com/d/dep1 name: package:github.com/d/dep1 packageName:dep1 recv: vendor:false",
				"gomodule/a.go:1:58-1:60 -> id:github.com/d/dep2/-/D2/D2 name:D2 package:github.com/d/dep2 packageName:dep2 recv:D2 vendor:false",
				"gomodule/a.go:1:53-1:55 -> id:github.com/d/dep1/-/D1 name:D1 package:github.com/d/dep1 packageName:dep1 recv: vendor:false",
			},
		})
	})

	t.Run("workspace references multiple files", func(t *testing.T){
		test(t, map[*lspext.WorkspaceReferencesParams][]string{
			{Query: lspext.SymbolDescriptor{}}: {
				"multiple/a.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
				"multiple/a.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
				"multiple/b.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
				"multiple/b.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
				"multiple/c.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
				"multiple/c.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
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
		want[i] = filepath.ToSlash(filepath.Join(exported.Config.Dir, want[i]))
	}

	rootDir := util.UriToRealPath(rootURI)
	var results []string
	for i := range references {
		if strings.Contains(references[i], "go-build") {
			continue
		}

		reference := util.UriToRealPath(lsp.DocumentURI(references[i]))
		prefix := filepath.Join(rootDir, pkgDir)
		if strings.HasPrefix(reference, prefix) {
			results = append(results, filepath.ToSlash(reference))
		}
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
