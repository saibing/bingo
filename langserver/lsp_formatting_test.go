package langserver

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"

	"github.com/saibing/bingo/langserver/internal/util"
)

func TestFormatting(t *testing.T) {
	setup(t)

	test := func(t *testing.T, input string, output map[string]string) {
		testFormatting(t, &formattingTestCase{input: input, output: output})
	}

	t.Run("basic", func(t *testing.T) {
		test(t, "basic/a.go", map[string]string{
			"0:0-1:0": "package p\n\nfunc A() { A() }\n",
		})
	})
}

type formattingTestCase struct {
	input  string
	output map[string]string
}

func testFormatting(tb testing.TB, c *formattingTestCase) {
	tbRun(tb, fmt.Sprintf("formatting-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
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
