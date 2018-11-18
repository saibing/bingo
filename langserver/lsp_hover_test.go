package langserver

import (
	"context"
	"fmt"
	"golang.org/x/tools/go/packages/packagestest"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestHover(t *testing.T) {
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
		testHover(t, &hoverTestCase{input: input, output: output})
	}

	t.Run("basic hover", func(t *testing.T) {
		test(t, "basic/a.go:1:9", "package p")
		test(t, "basic/a.go:1:17", "func A()")
		test(t, "basic/a.go:1:23", "func A()")
		test(t, "basic/b.go:1:17", "func B()")
		test(t, "basic/b.go:1:23", "func A()")
	})

	t.Run("detailed hover", func(t *testing.T) {
		test(t, "detailed/a.go:1:28", "struct field F string")
		test(t, "detailed/a.go:1:17", `type T struct; struct {
    F string
}`)
	})

	t.Run("xtest hover", func(t *testing.T) {
		test(t, "xtest/a.go:1:16", "var A int")
		test(t, "xtest/x_test.go:1:40", "package p")
		test(t, "xtest/x_test.go:1:82", "var X int")
		test(t, "xtest/x_test.go:1:88", "var A int")
		test(t, "xtest/a_test.go:1:16", "var X int")
		test(t, "xtest/a_test.go:1:20", "var A int")
	})

	t.Run("test hover", func(t *testing.T) {
		test(t, "test/a_test.go:1:96", "var X int")
		test(t, "test/a_test.go:1:102", "var B int")
	})

	t.Run("subdirectory hover", func(t *testing.T) {
		test(t, "subdirectory/a.go:1:17", "func A()")
		test(t, "subdirectory/a.go:1:23", "func A()")
		test(t, "subdirectory/d2/b.go:1:86", "func B()")
		test(t, "subdirectory/d2/b.go:1:94", "func A()")
		test(t, "subdirectory/d2/b.go:1:99", "func B()")
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, "multiple/a.go:1:17", "func A()")
		test(t, "multiple/a.go:1:23", "func A()")
	})

	t.Run("go root", func(t *testing.T) {
		test(t, "goroot/a.go:1:40", "func Println(a ...interface{}) (n int, err error)")
	})

	t.Run("go project", func(t *testing.T) {
		test(t, "goproject/a/a.go:1:17", "func A()")
		test(t, "goproject/b/b.go:1:89", "func A()")
	})

	t.Run("go module", func(t *testing.T) {
		test(t, "gomodule/a.go:1:57", "func D()")
		test(t, "gomodule/b.go:1:63", "func D()")
		test(t, "gomodule/c.go:1:63", "func D1() D2")
		test(t, "gomodule/c.go:1:68", "struct field D2 int")
	})

	t.Run("hover docs", func(t *testing.T) {
		test(t, "docs/a.go:7:9", "package p; Package p is a package with lots of great things. \n\n")
		//"a.go:9:9": "", TODO: handle hovering on import statements (ast.BasicLit)
		test(t, "docs/a.go:12:5", "var logit func(); logit is pkg2.X \n\n")
		test(t, "docs/a.go:12:13", "package pkg2 (\"github.com/saibing/dep/pkg2\")")
		test(t, "docs/a.go:12:18", "func X()")
		test(t, "docs/a.go:15:6", "type T struct; T is a struct. \n\n; struct {\n    F string\n    H Header\n}")
		test(t, "docs/a.go:17:2", "struct field F string; F is a string field. \n\n")
		test(t, "docs/a.go:20:2", "struct field H github.com/saibing/dep/pkg2.Header; H is a header. \n\n")
		test(t, "docs/a.go:20:4", "package pkg2 (\"github.com/saibing/dep/pkg2\")")
		test(t, "docs/a.go:24:5", "var Foo string; Foo is the best string. \n\n")
		test(t, "docs/a.go:31:2", "var I2 int; I2 is an int \n\n")

		test(t, "docs/q.go:3:2", "struct field Q string; Q is a string field. \n\n")
		test(t, "docs/q.go:5:2", "struct field X int; X is documented. \n\nX has comments. \n\n")
	})

	t.Run("hover issue", func(t *testing.T) {
		test(t, "issue/223.go:13:17", "func (*Hello).Bye() int")
		test(t, "issue/261.go:11:15", "var t T")
	})

	t.Run("go1.9 type alias", func(t *testing.T) {
		test(t, "typealias/a.go:1:17", "type A struct; struct {\n    a int\n}")
		test(t, "typealias/b.go:1:17", "type B struct; struct {\n    a int\n}")
		test(t, "typealias/b.go:1:20", "type B struct; struct {\n    a int\n}")
		test(t, "typealias/b.go:1:21", "type A struct; struct {\n    a int\n}")
	})

	t.Run("unexpected paths hover", func(t *testing.T) {
		test(t, "unexpected_paths/a.go:1:17", "func A()")
	})
}

type hoverTestCase struct {
	input  string
	output string
}

func testHover(tb testing.TB, c *hoverTestCase) {
	tbRun(tb, fmt.Sprintf("hover-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testHover", err)
		}
		doHoverTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doHoverTest(t testing.TB, ctx context.Context, conn *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	hover, err := callHover(ctx, conn, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if hover != want {
		t.Fatalf("\ngot %q, \nwant %q", hover, want)
	}
}

func callHover(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res struct {
		Contents markedStrings `json:"contents"`
		lsp.Hover
	}
	err := c.Call(ctx, "textDocument/hover", lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, ms := range res.Contents {
		if i != 0 {
			str += "; "
		}
		str += ms.Value
	}
	return str, nil
}
