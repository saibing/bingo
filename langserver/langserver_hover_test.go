package langserver

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestHover(t *testing.T) {
	test := func(t *testing.T, pkgDir string, input string, output string) {
		testHover(t, &hoverTestCase{pkgDir: pkgDir, input: input, output: output})
	}

	t.Run("basic hover", func(t *testing.T) {
		test(t, basicPkgDir, "a.go:1:9", "package p")
		test(t, basicPkgDir, "a.go:1:17", "func A()")
		test(t, basicPkgDir, "a.go:1:23", "func A()")
		test(t, basicPkgDir, "b.go:1:17", "func B()")
		test(t, basicPkgDir, "b.go:1:23", "func A()")
	})

	t.Run("detailed hover", func(t *testing.T) {
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
	})

	t.Run("hover docs", func(t *testing.T) {
		test(t, hoverDocsPkgDir, "a.go:7:9", "package p; Package p is a package with lots of great things. \n\n")
		//"a.go:9:9": "", TODO: handle hovering on import statements (ast.BasicLit)
		test(t, hoverDocsPkgDir, "a.go:12:5", "var logit func(); logit is pkg2.X \n\n")
		test(t, hoverDocsPkgDir, "a.go:12:13", "package pkg2 (\"github.com/saibing/dep/pkg2\"); Package pkg2 shows dependencies. \n\nHow to \n\n```\nExample Code!\n\n```\n")
		test(t, hoverDocsPkgDir, "a.go:12:18", "func X(); X does the unknown. \n\n")
		test(t, hoverDocsPkgDir, "a.go:15:6", "type T struct; T is a struct. \n\n; struct {\n    F string\n    H Header\n}")
		test(t, hoverDocsPkgDir, "a.go:17:2", "struct field F string; F is a string field. \n\n")
		test(t, hoverDocsPkgDir, "a.go:20:2", "struct field H github.com/saibing/dep/pkg2.Header; H is a header. \n\n")
		test(t, hoverDocsPkgDir, "a.go:20:4", "package pkg2 (\"github.com/saibing/dep/pkg2\"); Package pkg2 shows dependencies. \n\nHow to \n\n```\nExample Code!\n\n```\n")
		test(t, hoverDocsPkgDir, "a.go:24:5", "var Foo string; Foo is the best string. \n\n")
		test(t, hoverDocsPkgDir, "a.go:31:2", "var I2 int; I2 is an int \n\n")

		test(t, hoverDocsPkgDir, "q.go:3:2", "struct field Q string; Q is a string field. \n\n")
		test(t, hoverDocsPkgDir, "q.go:5:2", "struct field X int; X is documented. \n\nX has comments. \n\n")
	})

	t.Run("hover issue", func(t *testing.T) {
		test(t, issuePkgDir, "223.go:13:17", "func (*Hello).Bye() int")
		test(t, issuePkgDir, "261.go:11:15", "var t T")
	})

	t.Run("go1.9 type alias", func(t *testing.T) {
		test(t, typealiasPkgDir, "a.go:1:17", "type A struct; struct {\n    a int\n}")
		test(t, typealiasPkgDir, "b.go:1:17", "type B struct; struct {\n    a int\n}")
		test(t, typealiasPkgDir, "b.go:1:20", "")
		test(t, typealiasPkgDir, "b.go:1:21", "type A struct; struct {\n    a int\n}")
	})

	t.Run("unexpected paths hover", func(t *testing.T) {
		test(t, unexpectedPkgDir,"a.go", "func A()")
	})
}

type hoverTestCase struct {
	pkgDir string
	input  string
	output string
}

func testHover(tb testing.TB, c *hoverTestCase) {
	tbRun(tb, fmt.Sprintf("hover-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(c.pkgDir)
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
		t.Fatalf("got %q, want %q", hover, want)
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
