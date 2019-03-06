package langserver

import (
	"context"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestTypeDefinition(t *testing.T) {
	t.Parallel()

	setup(t)

	test := func(t *testing.T, input string, output string) {
		testTypeDefinition(t, &definitionTestCase{input: input, output: output})
	}

	t.Run("type definition lookup", func(t *testing.T) {
		test(t, "lookup/a/a.go:1:58", "lookup/a/a.go:1:17-1:18")
		test(t, "lookup/b/b.go:1:115", "lookup/a/a.go:1:17-1:18")
		test(t, "lookup/c/c.go:1:117", "lookup/a/a.go:1:17-1:18")
		test(t, "lookup/d/d.go:1:135", "")
	})
}

func testTypeDefinition(tb testing.TB, c *definitionTestCase) {
	tbRun(tb, fmt.Sprintf("typeDefinition-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testTypeDefinition", err)
		}
		doTypeDefinitionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output, "")
	})
}

func doTypeDefinitionTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want, trimPrefix string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := callTypeDefinition(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if definition != "" {
		definition = filepath.ToSlash(util.UriToRealPath(lsp.DocumentURI(definition)))
		if trimPrefix != "" {
			definition = strings.TrimPrefix(definition, util.UriToPath(util.PathToURI(trimPrefix)))
		}
	}
	if want != "" && !strings.Contains(path.Base(want), ":") {
		// our want is just a path, so we only check that matches. This is
		// used by our godef tests into GOROOT. The GOROOT changes over time,
		// but the file for a symbol is usually pretty stable.
		dir := path.Dir(definition)
		base := strings.Split(path.Base(definition), ":")[0]
		definition = path.Join(dir, base)
	}

	if want != "" {
		want = makePath(exported.Config.Dir, want)
	}
	if definition != want {
		t.Errorf("got %q, want %q", definition, want)
	}
}

func callTypeDefinition(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res locations
	err := c.Call(ctx, "textDocument/typeDefinition", lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, loc := range res {
		if loc.URI == "" {
			continue
		}
		if i != 0 {
			str += ", "
		}
		str += fmt.Sprintf("%s:%d:%d-%d:%d", loc.URI, loc.Range.Start.Line+1, loc.Range.Start.Character+1, loc.Range.End.Line+1, loc.Range.End.Character+1)
	}
	return str, nil
}
