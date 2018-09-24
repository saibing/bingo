package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/saibing/bingo/pkg/lspext"
	"github.com/sourcegraph/jsonrpc2"
	"log"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/util"
)

func TestImplementations(t *testing.T) {
	test := func(t *testing.T, pkgDir string, input string, output []string) {
		testImplementations(t, &implementationsTestCase{pkgDir: pkgDir, input: input, output: output})
	}

	t.Run("interfaces and implementations", func(t *testing.T) {
		test(t, implementationsPkgDir, "i0.go:1:17", []string{})
		test(t, implementationsPkgDir, "i0.go:1:32", []string{})
		test(t, implementationsPkgDir, "i1.go:1:17", []string{
			implementationsOutput("i2.go:1:17:to"),
			implementationsOutput("t1.go:1:17:to"),
			implementationsOutput("t1e.go:1:17:to"),
			implementationsOutput("t1p.go:1:17:to"),
			implementationsOutput("p2/p2.go:1:18:to"),
		})
		test(t, implementationsPkgDir, "i1.go:1:32", []string{
			implementationsOutput("i2.go:1:32:to:method"),
			implementationsOutput("t1.go:1:41:to:method"),
			implementationsOutput("t1p.go:1:44:to:method"),
			implementationsOutput("p2/p2.go:1:41:to:method"),
		})
		test(t, implementationsPkgDir, "i2.go:1:32", []string{implementationsOutput("i1.go:1:32:from:method")})
		test(t, implementationsPkgDir, "i2.go:1:38", []string{})
		test(t, implementationsPkgDir, "t0.go:1:17", []string{})
		test(t, implementationsPkgDir, "t1.go:1:17", []string{implementationsOutput("i1.go:1:17:from")})
		test(t, implementationsPkgDir, "t1.go:1:41", []string{implementationsOutput("i1.go:1:32:from:method")})
		test(t, implementationsPkgDir, "t1.go:1:59", []string{})
		test(t, implementationsPkgDir, "t1e.go:1:17", []string{implementationsOutput("i1.go:1:17:from")})
		test(t, implementationsPkgDir, "t1e.go:1:52", []string{implementationsOutput("i1.go:1:32:from:method")})
		test(t, implementationsPkgDir, "t1p.go:1:17", []string{implementationsOutput("i1.go:1:17:from:ptr")})
		test(t, implementationsPkgDir, "t1p.go:1:44", []string{implementationsOutput("i1.go:1:32:from:method")})
	})


}

type implementationsTestCase struct {
	pkgDir string
	input  string
	output []string
}

func testImplementations(tb testing.TB, c *implementationsTestCase) {
	tbRun(tb, fmt.Sprintf("implementations-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(c.pkgDir)
		if err != nil {
			log.Fatal("testImplementations", err)
		}
		doImplementationTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doImplementationTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos string, want []string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	impls, err := callImplementation(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	for i := range impls {
		impls[i] = util.UriToPath(lsp.DocumentURI(impls[i]))
	}
	sort.Strings(impls)
	sort.Strings(want)
	if !reflect.DeepEqual(impls, want) {
		t.Errorf("\ngot\n\t%q\nwant\n\t%q", impls, want)
	}
}

func callImplementation(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) ([]string, error) {
	var res []lspext.ImplementationLocation
	err := c.Call(ctx, "textDocument/implementation", lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}, &res)
	if err != nil {
		return nil, err
	}
	str := make([]string, len(res))
	for i, loc := range res {
		extra := []string{loc.Type}
		if loc.Ptr {
			extra = append(extra, "ptr")
		}
		if loc.Method {
			extra = append(extra, "method")
		}
		str[i] = fmt.Sprintf("%s:%d:%d:%s", loc.URI, loc.Range.Start.Line+1, loc.Range.Start.Character+1, strings.Join(extra, ":"))
	}
	return str, nil
}
