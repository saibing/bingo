package langserver

import (
	"context"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestDefinition(t *testing.T) {
	test := func(t *testing.T, input string, output string) {
		testDefinition(t, &definitionTestCase{input: input, output: output})
	}

	t.Run("basic definition", func(t *testing.T) {
		test(t, "basic/a.go:1:17", "basic/a.go:1:17-1:18")
		test(t, "basic/a.go:1:23", "basic/a.go:1:17-1:18")
		test(t, "basic/b.go:1:17", "basic/b.go:1:17-1:18")
		test(t, "basic/b.go:1:23", "basic/a.go:1:17-1:18")
	})

	t.Run("builtin definition", func(t *testing.T) {
		test(t, "builtin/a.go:1:26", "goroot/src/builtin/builtin.go:246:8-246:15")
	})

	t.Run("subdirectory definition", func(t *testing.T) {
		test(t, "subdirectory/a.go:1:17", "subdirectory/a.go:1:17-1:18")
		test(t, "subdirectory/a.go:1:23", "subdirectory/a.go:1:17-1:18")
		test(t, "subdirectory/d2/b.go:1:86", "subdirectory/d2/b.go:1:86-1:87")
		test(t, "subdirectory/d2/b.go:1:94", "subdirectory/a.go:1:17-1:18")
		test(t, "subdirectory/d2/b.go:1:99", "subdirectory/d2/b.go:1:86-1:87")
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, "multiple/a.go:1:17", "multiple/a.go:1:17-1:18")
		test(t, "multiple/a.go:1:23", "multiple/a.go:1:17-1:18")
	})

	t.Run("go root", func(t *testing.T) {
		test(t, "goroot/a.go:1:40", "goroot/src/fmt/print.go:263:6-263:13")
	})

	t.Run("go project", func(t *testing.T) {
		test(t, "goproject/a/a.go:1:17", "goproject/a/a.go:1:17-1:18")
		test(t, "goproject/b/b.go:1:89", "goproject/a/a.go:1:17-1:18")
	})

	t.Run("go module", func(t *testing.T) {
		test(t, "gomodule/a.go:1:57", "gomodule/d.go:1:19-1:20")
		test(t, "gomodule/b.go:1:63", "gomodule/subp/d.go:1:20-1:21")
		test(t, "gomodule/c.go:1:63", "gomodule/dep1/d1.go:1:58-1:60")
		test(t, "gomodule/c.go:1:68", "gomodule/dep2/d2.go:1:32-1:34")
	})

	t.Run("type definition lookup", func(t *testing.T) {
		test(t, "lookup/b/b.go:1:115", "lookup/b/b.go:1:95-1:96")
	})

	t.Run("go1.9 type alias", func(t *testing.T) {
		test(t, "typealias/a.go:1:17", "typealias/a.go:1:17-1:18")
		test(t, "typealias/b.go:1:17", "typealias/b.go:1:17-1:18")
		test(t, "typealias/b.go:1:20", "typealias/b.go:1:17-1:18")
		test(t, "typealias/b.go:1:21", "typealias/a.go:1:17-1:18")
	})
}

type definitionTestCase struct {
	input  string
	output string
}

func testDefinition(tb testing.TB, c *definitionTestCase) {
	tbRun(tb, fmt.Sprintf("definition-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testDefinition", err)
		}
		doDefinitionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output, "")
	})
}

func doDefinitionTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want, trimPrefix string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := callDefinition(ctx, c, uriJoin(rootURI, file), line, char)
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

	if strings.HasPrefix(want, goroot) {
		want = makePath(runtime.GOROOT(), want[len(goroot):])
	} else if strings.HasPrefix(want, gomodule) {
		want = makePath(gomoduleDir, want[len(gomodule):])
	} else if want != "" {
		want = makePath(exported.Config.Dir, want)
	}

	if definition != want {
		t.Errorf("\n%s\ngot %q, \nwant %q", pos, definition, want)
	}
}

func callDefinition(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res locations
	err := c.Call(ctx, "textDocument/definition", lsp.TextDocumentPositionParams{
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
