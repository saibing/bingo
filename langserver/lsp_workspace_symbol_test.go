package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/pkg/lspext"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

const exportedOnUnexported = "exported_on_unexported"
const gorootnoexport = "gorootnoexport"

func TestWorkspaceSymbol(t *testing.T) {
	test := func(t *testing.T, data map[*lspext.WorkspaceSymbolParams][]string) {
		for k, v := range data {
			testWorkspaceSymbol(t, &workspaceSymbolTestCase{input: k, output: v})
		}
	}

	t.Run("basic workspace symbol", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceSymbolParams][]string{
			{Query: ""}: {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},
			{Query: "A"}:           {"basic/a.go:function:A:1:17"},
			{Query: "B"}:           {"basic/b.go:function:B:1:17"},
			{Query: "is:exported"}: {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},
			{Query: "dir:basic/"}:       {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},
			{Query: "dir:basic/ A"}:     {"basic/a.go:function:A:1:17"},
			{Query: "dir:basic/ B"}:     {"basic/b.go:function:B:1:17"},

			// non-nil SymbolDescriptor + no keys.
			{Symbol: make(lspext.SymbolDescriptor)}: {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},

			//Individual filter fields.
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic"}}: {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"name": "A"}}:           {"basic/a.go:function:A:1:17"},
			{Symbol: lspext.SymbolDescriptor{"name": "B"}}:           {"basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"packageName": "p"}}:    {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"recv": ""}}:            {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"vendor": false}}:       {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},

			// Combined filter fields.
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic"}}:                                                               {"basic/a.go:function:A:1:17", "basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic", "name": "A"}}:                                                  {"basic/a.go:function:A:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic", "name": "A", "packageName": "p"}}:                              {"basic/a.go:function:A:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic", "name": "A", "packageName": "p", "recv": ""}}:                  {"basic/a.go:function:A:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic", "name": "A", "packageName": "p", "recv": "", "vendor": false}}: {"basic/a.go:function:A:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic", "name": "B"}}:                                                  {"basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic", "name": "B", "packageName": "p"}}:                              {"basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic", "name": "B", "packageName": "p", "recv": ""}}:                  {"basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/basic", "name": "B", "packageName": "p", "recv": "", "vendor": false}}: {"basic/b.go:function:B:1:17"},

			// By ID.
			{Symbol: lspext.SymbolDescriptor{"id": "github.com/saibing/bingo/langserver/test/pkg/basic/-/B"}}: {"basic/b.go:function:B:1:17"},
			{Symbol: lspext.SymbolDescriptor{"id": "github.com/saibing/bingo/langserver/test/pkg/basic/-/A"}}: {"basic/a.go:function:A:1:17"},
		})
	})

	t.Run("detailed workspace symbol", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceSymbolParams][]string{
			{Query: ""}:            {"detailed/a.go:class:T:1:17", "detailed/a.go:field:T.F:1:28"},
			{Query: "T"}:           {"detailed/a.go:class:T:1:17", "detailed/a.go:field:T.F:1:28"},
			{Query: "F"}:           {"detailed/a.go:field:T.F:1:28"},
			{Query: "is:exported"}: {"detailed/a.go:class:T:1:17", "detailed/a.go:field:T.F:1:28"},
		})
	})

	t.Run("exported defs unexported type", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceSymbolParams][]string{
			{Query: "is:exported"}: {exportedOnUnexported},
		})
	})

	t.Run("subdirectory workspace symbol", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceSymbolParams][]string{
			{Query: ""}:            {"subdirectory/a.go:function:A:1:17", "subdirectory/d2/b.go:function:B:1:86"},
			{Query: "is:exported"}: {"subdirectory/a.go:function:A:1:17", "subdirectory/d2/b.go:function:B:1:86"},
			{Query: "dir:subdirectory"}:        {"subdirectory/a.go:function:A:1:17"},
			{Query: "dir:subdirectory/"}:       {"subdirectory/a.go:function:A:1:17"},
			{Query: "dir:./subdirectory"}:       {"subdirectory/a.go:function:A:1:17"},
			{Query: "dir:subdirectory/d2"}:     {"subdirectory/d2/b.go:function:B:1:86"},
			{Query: "dir:./subdirectory/d2"}:    {"subdirectory/d2/b.go:function:B:1:86"},
		})
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceSymbolParams][]string{
			{Query: ""}:            {"multiple/a.go:function:A:1:17"},
			{Query: "is:exported"}: {"multiple/a.go:function:A:1:17"},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/multiple", "name": "A", "packageName": "p", "recv": "", "vendor": false}}: {"multiple/a.go:function:A:1:17"},
		})
	})

	t.Run("go root", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceSymbolParams][]string{
			{Query: ""}: {
				"goroot/a.go:variable:x:1:51",
			},
			{Query: "is:exported"}: {gorootnoexport},
			{Symbol: lspext.SymbolDescriptor{"package": "github.com/saibing/bingo/langserver/test/pkg/goroot", "name": "x", "packageName": "p", "recv": "", "vendor": false}}: {"goroot/a.go:variable:x:1:51"},
		})
	})

	t.Run("go project", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceSymbolParams][]string{
			{Query: ""}:            {"goproject/a/a.go:function:A:1:17"},
			{Query: "is:exported"}: {"goproject/a/a.go:function:A:1:17"},
		})
	})

	t.Run("go symbols", func(t *testing.T) {
		test(t, map[*lspext.WorkspaceSymbolParams][]string{
			{Query: ""}:            {"symbols/abc.go:variable:A:8:2", "symbols/abc.go:constant:B:12:2", "symbols/abc.go:class:C:17:2", "symbols/abc.go:class:T:22:6", "symbols/abc.go:interface:UVW:20:6", "symbols/abc.go:class:XYZ:3:6", "symbols/bcd.go:class:YZA:3:6", "symbols/cde.go:variable:a:4:2", "symbols/cde.go:variable:b:4:5", "symbols/cde.go:variable:c:5:2", "symbols/xyz.go:function:yza:3:6", "symbols/abc.go:method:XYZ.ABC:5:14", "symbols/bcd.go:method:YZA.BCD:5:14"},
			{Query: "xyz"}:         {"symbols/abc.go:class:XYZ:3:6", "symbols/abc.go:method:XYZ.ABC:5:14", "symbols/xyz.go:function:yza:3:6"},
			{Query: "yza"}:         {"symbols/bcd.go:class:YZA:3:6", "symbols/xyz.go:function:yza:3:6", "symbols/bcd.go:method:YZA.BCD:5:14"},
			{Query: "abc"}:         {"symbols/abc.go:method:XYZ.ABC:5:14", "symbols/abc.go:variable:A:8:2", "symbols/abc.go:constant:B:12:2", "symbols/abc.go:class:C:17:2", "symbols/abc.go:class:T:22:6", "symbols/abc.go:interface:UVW:20:6", "symbols/abc.go:class:XYZ:3:6"},
			{Query: "bcd"}:         {"symbols/bcd.go:method:YZA.BCD:5:14", "symbols/bcd.go:class:YZA:3:6"},
			{Query: "cde"}:         {"symbols/cde.go:variable:a:4:2", "symbols/cde.go:variable:b:4:5", "symbols/cde.go:variable:c:5:2"},
			{Query: "is:exported"}: {"symbols/abc.go:variable:A:8:2", "symbols/abc.go:constant:B:12:2", "symbols/abc.go:class:C:17:2", "symbols/abc.go:class:T:22:6", "symbols/abc.go:interface:UVW:20:6", "symbols/abc.go:class:XYZ:3:6", "symbols/bcd.go:class:YZA:3:6", "symbols/abc.go:method:XYZ.ABC:5:14", "symbols/bcd.go:method:YZA.BCD:5:14"},
		})
	})
}

type workspaceSymbolTestCase struct {
	input  *lspext.WorkspaceSymbolParams
	output []string
}

func testWorkspaceSymbol(tb testing.TB, c *workspaceSymbolTestCase) {
	tbRun(tb, fmt.Sprintf("workspace-symbol-%s", c.input.Query), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testWorkspaceSymbol", err)
		}
		doWorkspaceSymbolsTest(t, ctx, conn, util.PathToURI(dir), *c.input, c.output)
	})
}

func doWorkspaceSymbolsTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, params lspext.WorkspaceSymbolParams, want []string) {
	symbols, err := callWorkspaceSymbols(ctx, c, params)
	if err != nil {
		t.Fatal(err)
	}

	rootDir := util.UriToRealPath(rootURI)

	var pkgDir string
	for i := range want {
		if want[i] == exportedOnUnexported {
			want = nil
			pkgDir = exportedOnUnexported
			break
		} else if want[i] == gorootnoexport {
			want = nil
			pkgDir = goroot
		} else {
			if i == 0 {
				splits := strings.Split(want[i], "/")
				pkgDir = splits[0]
			}
			want[i] = makePath(rootDir, want[i])
		}
	}

	var results []string
	for i := range symbols {
		symbol := util.UriToRealPath(lsp.DocumentURI(symbols[i]))
		prefix := filepath.Join(util.LowerDriver(rootDir), pkgDir)
		if strings.HasPrefix(symbol, prefix) {
			results = append(results, filepath.ToSlash(symbol))
		}
	}

	if !reflect.DeepEqual(results, want) {
		t.Errorf("\n%s\ngot %#v, \nwant %q", params.Query, results, want)
	}
}

func callWorkspaceSymbols(ctx context.Context, c *jsonrpc2.Conn, params lspext.WorkspaceSymbolParams) ([]string, error) {
	var symbols []lsp.SymbolInformation
	params.Limit = 10000
	err := c.Call(ctx, "workspace/symbol", params, &symbols)
	if err != nil {
		return nil, err
	}
	syms := make([]string, len(symbols))
	for i, s := range symbols {
		syms[i] = fmt.Sprintf("%s:%s:%s:%d:%d", s.Location.URI, strings.ToLower(s.Kind.String()), qualifiedName(s), s.Location.Range.Start.Line+1, s.Location.Range.Start.Character+1)
	}
	return syms, nil
}
