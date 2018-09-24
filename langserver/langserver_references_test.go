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

	"github.com/saibing/bingo/langserver/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestReferences(t *testing.T) {
	test := func(t *testing.T, pkgDir string, input string, output []string) {
		testReferences(t, &referencesTestCase{pkgDir: pkgDir, input: input, output: output})
	}

	t.Run("basic", func(t *testing.T) {
		test(t, basicPkgDir, "a.go:1:17", []string{basicOutput("a.go:1:17"), basicOutput("a.go:1:23"), basicOutput("b.go:1:23")})
		test(t, basicPkgDir, "a.go:1:23", []string{basicOutput("a.go:1:17"), basicOutput("a.go:1:23"), basicOutput("b.go:1:23")})
		test(t, basicPkgDir, "b.go:1:17", []string{basicOutput("b.go:1:17")})
		test(t, basicPkgDir, "b.go:1:23", []string{basicOutput("a.go:1:17"), basicOutput("a.go:1:23"), basicOutput("b.go:1:23")})
	})


	t.Run("xtest", func(t *testing.T) {
		test(t, xtestPkgDir, "a.go:1:16", []string{xtestOutput("a.go:1:16"), xtestOutput("a_test.go:1:20"), xtestOutput("x_test.go:1:46")})
		test(t, xtestPkgDir, "x_test.go:1:46", []string{xtestOutput("a.go:1:16"), xtestOutput("a_test.go:1:20"), xtestOutput("x_test.go:1:46")})
		test(t, xtestPkgDir, "x_test.go:1:40", []string{xtestOutput("x_test.go:1:40"), xtestOutput("y_test.go:1:39")})
		test(t, xtestPkgDir, "a_test.go:1:16", []string{xtestOutput("a.go:1:16"), xtestOutput("a_test.go:1:20"), xtestOutput("x_test.go:1:46")})
		test(t, xtestPkgDir, "a_test.go:1:20", []string{xtestOutput("a_test.go:1:16"), xtestOutput("b_test.go:1:34")})
	})

	t.Run("test hover", func(t *testing.T) {
		test(t, testPkgDir, "a_test.go:1:43", []string{testOutput("a_test.go:1:43"), testOutput("b/b.go:1:16"), testOutput("b/b.go:1:45"), testOutput("c/c.go:1:43")})
		test(t, testPkgDir, "a_test.go:1:41", []string{testOutput("a_test.go:1:19"), testOutput("a_test.go:1:41")})
		test(t, testPkgDir, "a_test.go:1:51", []string{testOutput("a_test.go:1:51")})
	})

	t.Run("go project", func(t *testing.T) {
		test(t, goprojectPkgDir, "a/a.go:1:17", []string{goprojectOutput("a/a.go:1:17"), goprojectOutput("b/b.go:1:89")})
		test(t, goprojectPkgDir, "b/b.go:1:89", []string{goprojectOutput("a/a.go:1:17"), goprojectOutput("b/b.go:1:89")})
		test(t, goprojectPkgDir, "b/b.go:1:87", []string{goprojectOutput("b/b.go:1:19"), goprojectOutput("b/b.go:1:87")})
	})

	t.Run("go module", func(t *testing.T) {
		test(t, gomodulePkgDir, "a.go:1:57", []string{gomoduleOutput("a.go:1:57"), gomoduleOutput("a.go:1:72")})
	})
}

type referencesTestCase struct {
	pkgDir string
	input  string
	output []string
}

func testReferences(tb testing.TB, c *referencesTestCase) {
	tbRun(tb, fmt.Sprintf("hover-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(c.pkgDir)
		if err != nil {
			log.Fatal("testHover", err)
		}
		doReferencesTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doReferencesTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos string, want []string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	references, err := callReferences(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	for i := range references {
		references[i] = util.UriToPath(lsp.DocumentURI(references[i]))
	}
	sort.Strings(references)
	sort.Strings(want)
	if !reflect.DeepEqual(references, want) {
		t.Errorf("\ngot\n\t%q\nwant\n\t%q", references, want)
	}
}

func callReferences(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) ([]string, error) {
	var res locations
	err := c.Call(ctx, "textDocument/references", lsp.ReferenceParams{
		Context: lsp.ReferenceContext{IncludeDeclaration: true},
		TextDocumentPositionParams: lsp.TextDocumentPositionParams{
			TextDocument: lsp.TextDocumentIdentifier{URI: uri},
			Position:     lsp.Position{Line: line, Character: char},
		},
	}, &res)
	if err != nil {
		return nil, err
	}
	str := make([]string, len(res))
	for i, loc := range res {
		str[i] = fmt.Sprintf("%s:%d:%d", loc.URI, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	return str, nil
}