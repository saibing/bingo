package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"
)

func TestSignature(t *testing.T) {
	test := func(t *testing.T, data map[string]string) {
		for k, v := range data {
			testSignature(t, &signatureTestCase{input: k, output: v})
		}
	}

	t.Run("signature help", func(t *testing.T) {
		test(t, map[string]string{
			"signature/b.go:1:28": "B() 0",
			"signature/b.go:1:29": " 0",
			"signature/b.go:1:33": "A(foo int, bar func(baz int) int) 0",
			"signature/b.go:1:40": "A(foo int, bar func(baz int) int) 1",
			"signature/b.go:1:46": "A(foo int, bar func(baz int) int) 0",
			"signature/b.go:1:51": "C(x int, y int) 0",
			"signature/b.go:1:53": "C(x int, y int) 1",
			"signature/b.go:1:54": "C(x int, y int) 1",
			"signature/c.go:1:57": "fmt.Printf(format string, a ...interface{}) 1",
			"signature/d.go:1:52": "fmt.Printf(format string, a ...interface{}) 0",
			"signature/e.go:1:48": "builtin.append(slice []builtin.Type, elems ...builtin.Type) 0",
		})
	})
}

type signatureTestCase struct {
	input  string
	output string
}

func testSignature(tb testing.TB, c *signatureTestCase) {
	tbRun(tb, fmt.Sprintf("signature-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testSignature", err)
		}
		doSignatureTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doSignatureTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := callSignature(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if signature != want {
		t.Fatalf("\ngot %q, \nwant %q", signature, want)
	}
}

func callSignature(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res lsp.SignatureHelp
	err := c.Call(ctx, "textDocument/signatureHelp", lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, si := range res.Signatures {
		if i != 0 {
			str += "; "
		}
		str += si.Label
		if si.Documentation != "" {
			str += " " + si.Documentation
		}
	}
	str += fmt.Sprintf(" %d", res.ActiveParameter)
	return str, nil
}
