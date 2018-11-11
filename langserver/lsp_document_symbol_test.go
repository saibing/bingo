package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages/packagestest"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"
)

func TestDocumentSymbol(t *testing.T) {

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

	test := func(t *testing.T, data map[string][]string) {
		for k, v := range data {
			testDocumentSymbol(t, &documentSymbolTestCase{input:k, output:v})
		}
	}

	t.Run("basic document symbol", func(t *testing.T) {
		test(t, map[string][]string{
			"basic/a.go": {"basic/a.go:function:A:1:17"},
			"basic/b.go": {"basic/b.go:function:B:1:17"},
		})
	})

	t.Run("detailed document symbol", func(t *testing.T) {
		test(t, map[string][]string{
			"detailed/a.go": {"detailed/a.go:field:T.F:1:28", "detailed/a.go:class:T:1:17"},
		})
	})

	t.Run("exported defs unexported type", func(t *testing.T) {
		test(t, map[string][]string{
			"exported_on_unexported/a.go": {"exported_on_unexported/a.go:field:t.F:1:28", "exported_on_unexported/a.go:class:t:1:17"},
		})
	})

	t.Run("xtest", func(t *testing.T) {
		test(t, map[string][]string{
			"xtest/y_test.go": {"xtest/y_test.go:function:Y:1:22"},
			"xtest/b_test.go": {"xtest/b_test.go:function:Y:1:17"},
		})
	})

	t.Run("subdirectory document symbol", func(t *testing.T) {
		test(t, map[string][]string{
			"subdirectory/a.go":    {"subdirectory/a.go:function:A:1:17"},
			"subdirectory/d2/b.go": {"subdirectory/d2/b.go:function:B:1:86"},
		})
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, map[string][]string{
			"multiple/a.go": {"multiple/a.go:function:A:1:17"},
		})
	})

	t.Run("go root", func(t *testing.T) {
		test(t, map[string][]string{
			"goroot/a.go": {"goroot/a.go:variable:x:1:51"},
		})
	})

	t.Run("go project", func(t *testing.T) {
		test(t, map[string][]string{
			"goproject/a/a.go": {"goproject/a/a.go:function:A:1:17"},
			"goproject/b/b.go": {},
		})
	})

	t.Run("go symbols", func(t *testing.T) {
		test(t, map[string][]string{
			"symbols/abc.go": {
				"symbols/abc.go:class:XYZ:3:6",
				"symbols/abc.go:method:XYZ.ABC:5:14",
				"symbols/abc.go:variable:A:8:2",
				"symbols/abc.go:constant:B:12:2",
				"symbols/abc.go:class:C:17:2",
				"symbols/abc.go:interface:UVW:20:6",
				"symbols/abc.go:class:T:22:6"},
			"symbols/bcd.go": {
				"symbols/bcd.go:class:YZA:3:6",
				"symbols/bcd.go:method:YZA.BCD:5:14"},
			"symbols/cde.go": {
				"symbols/cde.go:variable:a:4:2",
				"symbols/cde.go:variable:b:4:5",
				"symbols/cde.go:variable:c:5:2"},
			"symbols/xyz.go": {
				"symbols/xyz.go:function:yza:3:6"},
		})
	})

	t.Run("unexpected paths", func(t *testing.T) {
		test(t, map[string][]string{
			"unexpected_paths/a.go": {"unexpected_paths/a.go:function:A:1:17"},
		})
	})

	t.Run("recv in different file", func(t *testing.T) {
		test(t, map[string][]string{
			"different/abc.go": {"different/abc.go:class:XYZ:2:6"},
			"different/bcd.go": {"different/bcd.go:method:XYZ.ABC:2:14"},
		})
	})
}

type documentSymbolTestCase struct {
	input  string
	output []string
}

func testDocumentSymbol(tb testing.TB, c *documentSymbolTestCase) {
	tbRun(tb, fmt.Sprintf("document-symbol-%s", c.input), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testDocumentSymbol", err)
		}
		doTestDocumentSymbol(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doTestDocumentSymbol(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, file string, want []string) {
	symbols, err := callSymbols(ctx, c, uriJoin(rootURI, file))
	if err != nil {
		t.Fatal(err)
	}
	for i := range symbols {
		symbols[i] = filepath.ToSlash(util.UriToRealPath(lsp.DocumentURI(symbols[i])))
	}

	for i, s := range want {
		want[i] = filepath.ToSlash(filepath.Join(exported.Config.Dir, s))
	}
	if !reflect.DeepEqual(symbols, want) {
		t.Errorf("got %q, want %q", symbols, want)
	}
}

func callSymbols(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI) ([]string, error) {
	var symbols []lsp.SymbolInformation
	err := c.Call(ctx, "textDocument/documentSymbol", lsp.DocumentSymbolParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
	}, &symbols)
	if err != nil {
		return nil, err
	}
	syms := make([]string, len(symbols))
	for i, s := range symbols {
		syms[i] = fmt.Sprintf("%s:%s:%s:%d:%d", s.Location.URI, strings.ToLower(s.Kind.String()), qualifiedName(s), s.Location.Range.Start.Line+1, s.Location.Range.Start.Character+1)
	}
	return syms, nil
}

