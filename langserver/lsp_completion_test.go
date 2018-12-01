package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"golang.org/x/tools/go/packages/packagestest"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"
)

func TestCompletion(t *testing.T) {

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

	test := func(t *testing.T, input string, output string) {
		testCompletion(t, &completionTestCase{input: input, output: output})
	}

	t.Run("basic", func(t *testing.T) {
		test(t, "basic/b.go:1:24", "1:23-1:24 A function func()")
	})

	t.Run("xtest", func(t *testing.T) {
		test(t, "xtest/x_test.go:1:87", "1:86-1:87 p module \"github.com/saibing/bingo/langserver/test/pkg/xtest\"")
		test(t, "xtest/x_test.go:1:88", "1:88-1:88 A variable int, X variable int, Y function func() int")
		test(t, "xtest/b_test.go:1:83", "1:83-1:83 A variable int, X variable int, Y function func() int")
	})

	t.Run("go subdirectory in repo", func(t *testing.T) {
		test(t, "subdirectory/d2/b.go:1:94", "1:94-1:94 A function func()")
	})

	t.Run("go root", func(t *testing.T) {
		test(t, "goroot/a.go:1:21", "1:21-1:21 fmt module \"fmt\", x variable int")
		test(t, "goroot/a.go:1:44", "1:38-1:44 Println function func(a ...interface{}) (n int, err error)")
	})

	t.Run("go project workspace", func(t *testing.T) {
		test(t, "goproject/b/b.go:1:87", "1:87-1:87 a module \"github.com/saibing/bingo/langserver/test/pkg/goproject/a\"")
		test(t, "goproject/b/b.go:1:89", "1:89-1:89 A function func()")
	})

	t.Run("go module dep", func(t *testing.T) {
		test(t, "gomodule/a.go:1:40", "1:40-1:40 dep module \"github.com/saibing/dep\"")
		test(t, "gomodule/a.go:1:57", "1:57-1:57 D function func()")

		test(t, "gomodule/b.go:1:40", "1:40-1:40 subp module \"github.com/saibing/dep/subp\"")
		test(t, "gomodule/b.go:1:63", "1:63-1:63 D function func()")

		test(t, "gomodule/c.go:1:68", "1:68-1:68 D2 field int")
	})

	t.Run("completion", func(t *testing.T) {
		test(t, "completion/a.go:6:7", "6:6-6:7 strings module \"strings\", s1 = 42 constant int, s2 function func(), s3 variable int, s4 variable func()")
		test(t, "completion/a.go:7:7", "7:6-7:7 new(T) function *T, nil variable")
		test(t, "completion/a.go:12:11", "12:8-12:11 int typeParameter , int16 typeParameter , int32 typeParameter , int64 typeParameter , int8 typeParameter ")
		test(t, "completion/b.go:1:44", "1:38-1:44 Println function func(a ...interface{}) (n int, err error)")
	})
}

type completionTestCase struct {
	input  string
	output string
}

func testCompletion(tb testing.TB, c *completionTestCase) {
	tbRun(tb, fmt.Sprintf("complete-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testCompletion", err)
		}
		doCompletionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doCompletionTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	completion, err := callCompletion(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if completion != want {
		t.Fatalf("\ngot %q, \nwant %q", completion, want)
	}
}

func callCompletion(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res lsp.CompletionList
	err := c.Call(ctx, "textDocument/completion", lsp.CompletionParams{TextDocumentPositionParams: lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, it := range res.Items {
		if i != 0 {
			str += ", "
		} else {
			e := it.TextEdit.Range
			str += fmt.Sprintf("%d:%d-%d:%d ", e.Start.Line+1, e.Start.Character+1, e.End.Line+1, e.End.Character+1)
		}
		str += fmt.Sprintf("%s %s %s", it.Label, it.Kind, it.Detail)
	}
	return str, nil
}
