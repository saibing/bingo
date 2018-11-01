package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/util"
)

func TestFormatting(t *testing.T) {
	test := func(t *testing.T, pkgDir string, input string, output map[string]string) {
		testFormatting(t, &formattingTestCase{pkgDir: pkgDir, input: input, output: output})
	}

	t.Run("basic", func(t *testing.T) {
		test(t, basicPkgDir, "a.go", map[string]string{
				"0:0-1:0": "package p\n\nfunc A() { A() }\n",
			})
	})
}

type formattingTestCase struct {
	pkgDir string
	input  string
	output map[string]string
}

func testFormatting(tb testing.TB, c *formattingTestCase) {
	tbRun(tb, fmt.Sprintf("formatting-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(c.pkgDir)
		if err != nil {
			log.Fatal("testFormatting", err)
		}
		doFormattingTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doFormattingTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, file string, want map[string]string) {
	edits, err := callFormatting(ctx, c, uriJoin(rootURI, file))
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	for _, edit := range edits {
		got[edit.Range.String()] = edit.NewText
	}

	if reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func callFormatting(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI) ([]lsp.TextEdit, error) {
	var edits []lsp.TextEdit
	err := c.Call(ctx, "textDocument/formatting", lsp.DocumentFormattingParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
	}, &edits)
	return edits, err
}