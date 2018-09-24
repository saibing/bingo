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
		//test(t, basicPkgDir, "a.go:1:23", "func A()")
		//test(t, basicPkgDir, "b.go:1:17", "func B()")
		//test(t, basicPkgDir, "b.go:1:23", "func A()")
	})

	/*t.Run("detailed hover", func(t *testing.T) {
		test(t, detailedPkgDir, "a.go:1:28", "struct field F string")
		test(t, detailedPkgDir, "a.go:1:17", `type T struct; struct {
    F string
}`)
	})

	t.Run("xtest hover", func(t *testing.T) {
		test(t, xtestPkgDir, "a.go:1:16", "var A int")
		test(t, xtestPkgDir, "x_test.go:1:40", "var X int")
		test(t, xtestPkgDir, "x_test.go:1:46", "var A int")
		test(t, xtestPkgDir, "a_test.go:1:16", "var X int")
		test(t, xtestPkgDir, "a_test.go:1:20", "var A int")
	})

	t.Run("test hover", func(t *testing.T) {
		test(t, testPkgDir, "a_test.go:1:37", "var X int")
		test(t, testPkgDir, "a_test.go:1:43", "var B int")
	})

	t.Run("subdirectory hover", func(t *testing.T) {
		test(t, subdirectoryPkgDir, "a.go:1:17", "func A()")
		test(t, subdirectoryPkgDir, "a.go:1:23", "func A()")
		test(t, subdirectoryPkgDir, "d2/b.go:1:86", "func B()")
		test(t, subdirectoryPkgDir, "d2/b.go:1:94", "func A()")
		test(t, subdirectoryPkgDir, "d2/b.go:1:99", "func B()")
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, multiplePkgDir, "a.go:1:17", "func A()")
		test(t, multiplePkgDir, "a.go:1:23", "func A()")
	})

	t.Run("go root", func(t *testing.T) {
		test(t, gorootPkgDir, "a.go:1:40", "func Println(a ...interface{}) (n int, err error); Println formats using the default formats for its operands and writes to standard output. Spaces are always added between operands and a newline is appended. It returns the number of bytes written and any write error encountered. \n\n")
	})

	t.Run("go project", func(t *testing.T) {
		test(t, goprojectPkgDir, "a/a.go:1:17", "func A()")
		test(t, goprojectPkgDir, "b/b.go:1:89", "func A()")
	})

	t.Run("go module", func(t *testing.T) {
		test(t, gomodulePkgDir, "a.go:1:57", "func D()")
		test(t, gomodulePkgDir, "b.go:1:63", "func D()")
		test(t, gomodulePkgDir, "c.go:1:63", "func D1() D2")
		test(t, gomodulePkgDir, "c.go:1:68", "struct field D2 int")
	})*/
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