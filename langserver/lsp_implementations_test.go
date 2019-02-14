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

	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/go-lsp/lspext"
	"github.com/sourcegraph/jsonrpc2"

	"github.com/saibing/bingo/langserver/internal/util"
)

func TestImplementations(t *testing.T) {
	setup(t)

	test := func(t *testing.T, input string, output []string) {
		testImplementations(t, &implementationsTestCase{input: input, output: output})
	}

	t.Run("interfaces and implementations", func(t *testing.T) {
		test(t, "implementations/i0.go:1:17", []string{})
		test(t, "implementations/i0.go:1:32", []string{})
		test(t, "implementations/i1.go:1:17", []string{
			"implementations/i2.go:1:17:to",
			"implementations/t1.go:1:17:to",
			"implementations/t1e.go:1:17:to",
			"implementations/t1p.go:1:17:to",
			"implementations/p2/p2.go:1:18:to",
		})
		test(t, "implementations/i1.go:1:32", []string{
			"implementations/i2.go:1:32:to:method",
			"implementations/t1.go:1:41:to:method",
			"implementations/t1p.go:1:44:to:method",
			"implementations/p2/p2.go:1:41:to:method",
		})
		test(t, "implementations/i2.go:1:32", []string{"implementations/i1.go:1:32:from:method"})
		test(t, "implementations/i2.go:1:38", []string{})
		test(t, "implementations/t0.go:1:17", []string{})
		test(t, "implementations/t1.go:1:17", []string{"implementations/i1.go:1:17:from"})
		test(t, "implementations/t1.go:1:41", []string{"implementations/i1.go:1:32:from:method"})
		test(t, "implementations/t1.go:1:59", []string{})
		test(t, "implementations/t1e.go:1:17", []string{"implementations/i1.go:1:17:from"})
		test(t, "implementations/t1e.go:1:52", []string{"implementations/i1.go:1:32:from:method"})
		test(t, "implementations/t1p.go:1:17", []string{"implementations/i1.go:1:17:from:ptr"})
		test(t, "implementations/t1p.go:1:44", []string{"implementations/i1.go:1:32:from:method"})
	})

}

type implementationsTestCase struct {
	input  string
	output []string
}

func testImplementations(tb testing.TB, c *implementationsTestCase) {
	tbRun(tb, fmt.Sprintf("implementations-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
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
		impls[i] = filepath.ToSlash(util.UriToRealPath(lsp.DocumentURI(impls[i])))
	}
	sort.Strings(impls)

	for i := range want {
		want[i] = makePath(exported.Config.Dir, want[i])
	}
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
