package langserver

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/go-lsp/lspext"
	"github.com/sourcegraph/jsonrpc2"
)

func TestXDefinition(t *testing.T) {
	setup(t)

	test := func(t *testing.T, input string, output string) {
		testXDefinition(t, &definitionTestCase{input: input, output: output})
	}

	t.Run("basic definition", func(t *testing.T) {
		test(t, "basic/a.go:1:17", "basic/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false")
		test(t, "basic/a.go:1:23", "basic/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false")
		test(t, "basic/b.go:1:17", "basic/b.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false")
		test(t, "basic/b.go:1:23", "basic/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false")
	})

	t.Run("subdirectory definition", func(t *testing.T) {
		test(t, "subdirectory/a.go:1:17", "subdirectory/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false")
		test(t, "subdirectory/a.go:1:23", "subdirectory/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false")
		test(t, "subdirectory/d2/b.go:1:86", "subdirectory/d2/b.go:1:86 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2 packageName:d2 recv: vendor:false")
		test(t, "subdirectory/d2/b.go:1:94", "subdirectory/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false")
		test(t, "subdirectory/d2/b.go:1:99", "subdirectory/d2/b.go:1:86 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2 packageName:d2 recv: vendor:false")
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, "multiple/a.go:1:17", "multiple/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/multiple/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/multiple packageName:p recv: vendor:false")
		test(t, "multiple/a.go:1:23", "multiple/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/multiple/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/multiple packageName:p recv: vendor:false")
	})

	t.Run("go root", func(t *testing.T) {
		test(t, "goroot/a.go:1:40", "goroot/src/fmt/print.go:274:6 id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false")
	})

	t.Run("go project", func(t *testing.T) {
		test(t, "goproject/a/a.go:1:17", "goproject/a/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/goproject/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/goproject/a packageName:a recv: vendor:false")
		test(t, "goproject/b/b.go:1:89", "goproject/a/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/goproject/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/goproject/a packageName:a recv: vendor:false")
	})

	t.Run("go module", func(t *testing.T) {
		test(t, "gomodule/a.go:1:57", "gomodule/d.go:1:19 id:github.com/saibing/dep/-/D name:D package:github.com/saibing/dep packageName:dep recv: vendor:false")
		test(t, "gomodule/b.go:1:63", "gomodule/subp/d.go:1:20 id:github.com/saibing/dep/subp/-/D name:D package:github.com/saibing/dep/subp packageName:subp recv: vendor:false")
		test(t, "gomodule/c.go:1:63", "gomodule/dep1/d1.go:1:58 id:github.com/saibing/dep/dep1/-/D1 name:D1 package:github.com/saibing/dep/dep1 packageName:dep1 recv: vendor:false")
		test(t, "gomodule/c.go:1:68", "gomodule/dep2/d2.go:1:32 id:github.com/saibing/dep/dep2/-/D2/D2 name:D2 package:github.com/saibing/dep/dep2 packageName:dep2 recv:D2 vendor:false")
	})

	t.Run("type definition lookup", func(t *testing.T) {
		test(t, "lookup/b/b.go:1:115", "lookup/b/b.go:1:95 id:github.com/saibing/bingo/langserver/test/pkg/lookup/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/lookup/a packageName:a recv: vendor:false")
	})
}

func testXDefinition(tb testing.TB, c *definitionTestCase) {
	tbRun(tb, fmt.Sprintf("xdefinition-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testXDefinition", err)
		}
		doXDefinitionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doXDefinitionTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}

	xdefinition, err := callXDefinition(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}

	if xdefinition != "" {
		xdefinition = filepath.ToSlash(util.UriToRealPath(lsp.DocumentURI(xdefinition)))
	}

	if strings.HasPrefix(want, goroot) {
		want = makePath(runtime.GOROOT(), want[len(goroot):])
	} else if strings.HasPrefix(want, gomodule) {
		want = makePath(gomoduleDir, want[len(gomodule):])
	} else if want != "" {
		want = makePath(exported.Config.Dir, want)
	}

	if xdefinition != want {
		t.Errorf("\ngot  %q\nwant %q", xdefinition, want)
	}
}

func callXDefinition(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res []lspext.SymbolLocationInformation
	err := c.Call(ctx, "textDocument/xdefinition", lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, loc := range res {
		if loc.Location.URI == "" {
			continue
		}
		if i != 0 {
			str += ", "
		}
		str += fmt.Sprintf("%s:%d:%d %s", loc.Location.URI, loc.Location.Range.Start.Line+1, loc.Location.Range.Start.Character+1, loc.Symbol)
	}
	return str, nil
}
