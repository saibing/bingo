package langserver

import (
	"context"
	"fmt"
	"golang.org/x/tools/go/packages/packagestest"
	"log"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestReferences(t *testing.T) {
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

	test := func(t *testing.T, input string, output []string) {
		testReferences(t, &referencesTestCase{input: input, output: output})
	}

	t.Run("basic", func(t *testing.T) {
		test(t, "basic/a.go:1:17", []string{"basic/a.go:1:17", "basic/a.go:1:23", "basic/b.go:1:23"})
		test(t, "basic/a.go:1:23", []string{"basic/a.go:1:17", "basic/a.go:1:23", "basic/b.go:1:23"})
		test(t, "basic/b.go:1:17", []string{"basic/b.go:1:17"})
		test(t, "basic/b.go:1:23", []string{"basic/a.go:1:17", "basic/a.go:1:23", "basic/b.go:1:23"})
	})

	t.Run("xtest", func(t *testing.T) {
		//test(t, "xtest/a.go:1:16", []string{"xtest/a.go:1:16", "xtest/a_test.go:1:20", "xtest/x_test.go:1:88"})
		//test(t, "xtest/x_test.go:1:88", []string{"xtest/a.go:1:16", "xtest/a_test.go:1:20", "xtest/x_test.go:1:88"})
		//test(t, "xtest/x_test.go:1:82", []string{"xtest/x_test.go:1:82", "xtest/y_test.go:1:39"})
		test(t, "xtest/a_test.go:1:16", []string{"xtest/a.go:1:16", "xtest/a_test.go:1:20", "xtest/x_test.go:1:88"})
		//test(t, "xtest/a_test.go:1:20", []string{"xtest/a_test.go:1:16", "xtest/b_test.go:1:34"})
	})

	t.Run("test", func(t *testing.T) {
		test(t, "test/a_test.go:1:102", []string{"test/a_test.go:1:102", "test/b/b.go:1:16", "test/b/b.go:1:45", "test/c/c.go:1:84"})
		test(t, "test/a_test.go:1:100", []string{"test/a_test.go:1:19", "test/a_test.go:1:41"})
		test(t, "test/a_test.go:1:110", []string{"test/a_test.go:1:51"})
	})

	t.Run("go project", func(t *testing.T) {
		test(t, "goproject/a/a.go:1:17", []string{"goproject/a/a.go:1:17", "goproject/b/b.go:1:89"})
		test(t, "goproject/b/b.go:1:89", []string{"goproject/a/a.go:1:17", "goproject/b/b.go:1:89"})
		test(t, "goproject/b/b.go:1:87", []string{"goproject/b/b.go:1:19", "goproject/b/b.go:1:87"})
	})

	t.Run("go module", func(t *testing.T) {
		test(t, "gomodule/a.go:1:57", []string{"gomodule/a.go:1:57", "gomodule/a.go:1:72", githubModule + "/d.go:1:19"})
	})

	t.Run("unexpected paths", func(t *testing.T) {
		test(t, "unexpected_paths/a.go", []string{"/src/t:est/@hello/pkg/a.go:1:17", "/src/t:est/@hello/pkg/a.go:1:23"})
	})
}

type referencesTestCase struct {
	input  string
	output []string
}

func testReferences(tb testing.TB, c *referencesTestCase) {
	tbRun(tb, fmt.Sprintf("references-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testReferences", err)
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
		references[i] = filepath.ToSlash(util.UriToRealPath(lsp.DocumentURI(references[i])))
	}

	for i := range want {
		if strings.HasPrefix(want[i], githubModule) {
			want[i] = filepath.ToSlash(filepath.Join(gopathDir, want[i]))
		} else {
			want[i] = filepath.ToSlash(filepath.Join(exported.Config.Dir, want[i]))
		}
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
