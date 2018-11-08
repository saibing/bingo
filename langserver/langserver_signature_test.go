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
	test := func(t *testing.T, pkgDir string, data map[string]string) {
		for k, v := range data {
			testSignature(t, &signatureTestCase{pkgDir: pkgDir, input: k, output: v})
		}
	}

	t.Run("signature help", func(t *testing.T) {
		test(t, signatruePkgDir, map[string]string{
			"b.go:1:28": "func() 0",
			"b.go:1:33": "func(foo int, bar func(baz int) int) int Comments for A\n 0",
			"b.go:1:40": "func(foo int, bar func(baz int) int) int Comments for A\n 1",
			"b.go:1:46": "func(foo int, bar func(baz int) int) int Comments for A\n 0",
			"b.go:1:51": "func(x int, y int) int Comments for C\n 0",
			"b.go:1:53": "func(x int, y int) int Comments for C\n 1",
			"b.go:1:54": "func(x int, y int) int Comments for C\n 1",
		})
	})
}

type signatureTestCase struct {
	pkgDir string
	input  string
	output string
}

func testSignature(tb testing.TB, c *signatureTestCase) {
	tbRun(tb, fmt.Sprintf("signature-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(c.pkgDir)
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
		t.Fatalf("got %q, want %q", signature, want)
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