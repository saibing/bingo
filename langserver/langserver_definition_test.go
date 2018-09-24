package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/pkg/lspext"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestDefinition(t *testing.T) {
	test := func(t *testing.T, pkgDir string, input string, output string) {
		testDefinition(t, &definitionTestCase{pkgDir: pkgDir, input: input, output: output})
	}

	t.Run("basic definition", func(t *testing.T) {
		test(t, basicPkgDir, "a.go:1:17", basicOutput("a.go:1:17-1:18"))
		test(t, basicPkgDir, "a.go:1:23", basicOutput("a.go:1:17-1:18"))
		test(t, basicPkgDir, "b.go:1:17", basicOutput("b.go:1:17-1:18"))
		test(t, basicPkgDir, "b.go:1:23", basicOutput("a.go:1:17-1:18"))
	})

	t.Run("subdirectory definition", func(t *testing.T) {
		test(t, subdirectoryPkgDir, "a.go:1:17", subdirectoryOutput("a.go:1:17-1:18"))
		test(t, subdirectoryPkgDir, "a.go:1:23", subdirectoryOutput("a.go:1:17-1:18"))
		test(t, subdirectoryPkgDir, "d2/b.go:1:86", subdirectoryOutput("d2/b.go:1:86-1:87"))
		test(t, subdirectoryPkgDir, "d2/b.go:1:94", subdirectoryOutput("a.go:1:17-1:18"))
		test(t, subdirectoryPkgDir, "d2/b.go:1:99", subdirectoryOutput("d2/b.go:1:86-1:87"))
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, multiplePkgDir, "a.go:1:17", multipleOutput("a.go:1:17-1:18"))
		test(t, multiplePkgDir, "a.go:1:23", multipleOutput("a.go:1:17-1:18"))
	})

	t.Run("go root", func(t *testing.T) {
		test(t, gorootPkgDir, "a.go:1:40", gorootOutput("src/fmt/print.go:263:6-263:13"))
	})

	t.Run("go project", func(t *testing.T) {
		test(t, goprojectPkgDir, "a/a.go:1:17", goprojectOutput("a/a.go:1:17-1:18"))
		test(t, goprojectPkgDir, "b/b.go:1:89", goprojectOutput("a/a.go:1:17-1:18"))
	})

	t.Run("go module", func(t *testing.T) {
		test(t, gomodulePkgDir, "a.go:1:57", gomoduleOutput("d.go:1:19-1:20"))
		test(t, gomodulePkgDir, "b.go:1:63", gomoduleOutput("subp/d.go:1:20-1:21"))
		test(t, gomodulePkgDir, "c.go:1:63", gomoduleOutput("dep1/d1.go:1:58-1:60"))
		test(t, gomodulePkgDir, "c.go:1:68", gomoduleOutput("dep2/d2.go:1:32-1:34"))
	})

	t.Run("type definition lookup", func(t *testing.T) {
		test(t, lookupPkgDir, "b/b.go:1:115", lookupOutput("b/b.go:1:95-1:96"))
	})
}

type definitionTestCase struct {
	pkgDir string
	input  string
	output string
}

func testDefinition(tb testing.TB, c *definitionTestCase) {
	tbRun(tb, fmt.Sprintf("definition-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(c.pkgDir)
		if err != nil {
			log.Fatal("testDefinition", err)
		}
		doDefinitionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doDefinitionTest(t testing.TB, ctx context.Context, conn *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := callDefinition(ctx, conn, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if result != want {
		t.Fatalf("got %q, want %q", result, want)
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


func TestXDefinition(t *testing.T) {
	test := func(t *testing.T, pkgDir string, input string, output string) {
		testXDefinition(t, &definitionTestCase{pkgDir: pkgDir, input: input, output: output})
	}

	t.Run("basic definition", func(t *testing.T) {
		test(t, basicPkgDir, "a.go:1:17", basicOutput("a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false"))
		test(t, basicPkgDir, "a.go:1:23", basicOutput("a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false"))
		test(t, basicPkgDir, "b.go:1:17", basicOutput("b.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false"))
		test(t, basicPkgDir, "b.go:1:23", basicOutput("a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false"))
	})

	t.Run("subdirectory definition", func(t *testing.T) {
		test(t, subdirectoryPkgDir, "a.go:1:17", subdirectoryOutput("a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false"))
		test(t, subdirectoryPkgDir, "a.go:1:23", subdirectoryOutput("a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false"))
		test(t, subdirectoryPkgDir, "d2/b.go:1:86", subdirectoryOutput("d2/b.go:1:86 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2 packageName:d2 recv: vendor:false"))
		test(t, subdirectoryPkgDir, "d2/b.go:1:94", subdirectoryOutput("a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false"))
		test(t, subdirectoryPkgDir, "d2/b.go:1:99", subdirectoryOutput("d2/b.go:1:86 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2 packageName:d2 recv: vendor:false"))
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, multiplePkgDir, "a.go:1:17", multipleOutput("a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/multiple/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/multiple packageName:p recv: vendor:false"))
		test(t, multiplePkgDir, "a.go:1:23", multipleOutput("a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/multiple/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/multiple packageName:p recv: vendor:false"))
	})

	t.Run("go root", func(t *testing.T) {
		test(t, gorootPkgDir, "a.go:1:40", gorootOutput("src/fmt/print.go:263:6 id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false"))
	})

	t.Run("go project", func(t *testing.T) {
		test(t, goprojectPkgDir, "a/a.go:1:17", goprojectOutput("a/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/goproject/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/goproject/a packageName:a recv: vendor:false"))
		test(t, goprojectPkgDir, "b/b.go:1:89", goprojectOutput("a/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/goproject/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/goproject/a packageName:a recv: vendor:false"))
	})

	t.Run("go module", func(t *testing.T) {
		test(t, gomodulePkgDir, "a.go:1:57", gomoduleOutput("d.go:1:19 id:github.com/saibing/dep/-/D name:D package:github.com/saibing/dep packageName:dep recv: vendor:false"))
		test(t, gomodulePkgDir, "b.go:1:63", gomoduleOutput("subp/d.go:1:20 id:github.com/saibing/dep/subp/-/D name:D package:github.com/saibing/dep/subp packageName:subp recv: vendor:false"))
		test(t, gomodulePkgDir, "c.go:1:63", gomoduleOutput("dep1/d1.go:1:58 id:github.com/saibing/dep/dep1/-/D1 name:D1 package:github.com/saibing/dep/dep1 packageName:dep1 recv: vendor:false"))
		test(t, gomodulePkgDir, "c.go:1:68", gomoduleOutput("dep2/d2.go:1:32 id:github.com/saibing/dep/dep2/-/D2/D2 name:D2 package:github.com/saibing/dep/dep2 packageName:dep2 recv:D2 vendor:false"))
	})

	t.Run("type definition lookup", func(t *testing.T) {
		test(t, lookupPkgDir, "b/b.go:1:115", lookupOutput("b/b.go:1:95 id:github.com/saibing/bingo/langserver/test/pkg/lookup/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/lookup/a packageName:a recv: vendor:false"))
	})
}


func testXDefinition(tb testing.TB, c *definitionTestCase) {
	tbRun(tb, fmt.Sprintf("xdefinition-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(c.pkgDir)
		if err != nil {
			log.Fatal("testXDefinition", err)
		}
		doXDefinitionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doXDefinitionTest(t testing.TB, ctx context.Context, conn *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	result, err := callXDefinition(ctx, conn, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if result != want {
		t.Fatalf("got %q, want %q", result, want)
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
