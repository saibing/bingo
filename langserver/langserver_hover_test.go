package langserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sourcegraph/ctxvfs"
	"github.com/sourcegraph/go-langserver/langserver/internal/gocode"
	"github.com/sourcegraph/go-langserver/langserver/util"
	"github.com/sourcegraph/go-langserver/pkg/lsp"
	"github.com/sourcegraph/go-langserver/pkg/lspext"
	"github.com/sourcegraph/jsonrpc2"
)

type hoverTestCase struct {
	pkgDir  lsp.DocumentURI
	cases   lspTestCases
}

var hoverTestCases = map[string]hoverTestCase{
	"go basic": {
		pkgDir: "test/pkg/basic",
		cases: lspTestCases{
			overrideGodefHover: map[string]string{
				//"a.go:1:9":  "package p", // TODO(slimsag): sub-optimal "no declaration found for p"
				"a.go:1:17": "func A()",
				"a.go:1:23": "func A()",
				"b.go:1:17": "func B()",
				"b.go:1:23": "func A()",
			},
			wantHover: map[string]string{
				"a.go:1:9":  "package p",
				"a.go:1:17": "func A()",
				"a.go:1:23": "func A()",
				"b.go:1:17": "func B()",
				"b.go:1:23": "func A()",
			},
		},
	},
}

type myTestCase struct {
	pkgDir  lsp.DocumentURI
	cases map[string]string
}

func TestBasic(t *testing.T) {

	test := func(t *testing.T) {

	}

	t.Run("go basic hover")

	for label, test := range hoverTestCases {
		t.Run(label, func(t *testing.T) {


			doLspTests(t, ctx, h, conn, test.pkgDir, test.cases)
		})
	}
}


// lspTests runs all test suites for LSP functionality.
func doLspTests(t testing.TB, ctx context.Context, h *LangHandler, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, cases lspTestCases) {
	for pos, want := range cases.wantHover {
		tbRun(t, fmt.Sprintf("hover-%s", strings.Replace(pos, "/", "-", -1)), func(t testing.TB) {
			hoverTest(t, ctx, c, rootURI, pos, want)
		})
	}

	// Godef-based definition & hover testing
	wantGodefDefinition := cases.overrideGodefDefinition
	if len(wantGodefDefinition) == 0 {
		wantGodefDefinition = cases.wantDefinition
	}
	wantGodefHover := cases.overrideGodefHover
	if len(wantGodefHover) == 0 {
		wantGodefHover = cases.wantHover
	}

	if len(wantGodefDefinition) > 0 || (len(wantGodefHover) > 0 && h != nil) || len(cases.wantCompletion) > 0 {
		h.config.UseBinaryPkgCache = true

		// Copy the VFS into a temp directory, which will be our $GOPATH.
		tmpDir, err := ioutil.TempDir("", "godef-definition")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)
		if err := copyDirToOS(ctx, h.FS, tmpDir, "/"); err != nil {
			t.Fatal(err)
		}

		// Important: update build.Default.GOPATH, since it is compiled into
		// the binary we must update it here at runtime. Otherwise, godef would
		// look for $GOPATH/pkg .a files inside the $GOPATH that was set during
		// 'go test' instead of our tmp directory.
		build.Default.GOPATH = tmpDir
		gocode.SetBuildContext(&build.Default)
		tmpRootPath := filepath.Join(tmpDir, util.UriToPath(rootURI))

		// Install all Go packages in the $GOPATH.
		oldGOPATH := os.Getenv("GOPATH")
		os.Setenv("GOPATH", tmpDir)
		out, err := exec.Command("go", "install", "-v", "all").CombinedOutput()
		os.Setenv("GOPATH", oldGOPATH)
		t.Logf("$ GOPATH='%s' go install -v all\n%s", tmpDir, out)
		if err != nil {
			t.Fatal(err)
		}

		testOSToVFSPath = func(osPath string) string {
			return strings.TrimPrefix(osPath, util.UriToPath(util.PathToURI(tmpDir)))
		}

		// Run the tests.
		for pos, want := range wantGodefDefinition {
			if strings.HasPrefix(want, "/goroot") {
				want = strings.Replace(want, "/goroot", path.Clean(util.UriToPath(util.PathToURI(build.Default.GOROOT))), 1)
			}
			tbRun(t, fmt.Sprintf("godef-definition-%s", strings.Replace(pos, "/", "-", -1)), func(t testing.TB) {
				definitionTest(t, ctx, c, util.PathToURI(tmpRootPath), pos, want, tmpDir)
			})
		}
		for pos, want := range wantGodefHover {
			tbRun(t, fmt.Sprintf("godef-hover-%s", strings.Replace(pos, "/", "-", -1)), func(t testing.TB) {
				hoverTest(t, ctx, c, util.PathToURI(tmpRootPath), pos, want)
			})
		}
		for pos, want := range cases.wantCompletion {
			tbRun(t, fmt.Sprintf("completion-%s", strings.Replace(pos, "/", "-", -1)), func(t testing.TB) {
				completionTest(t, ctx, c, util.PathToURI(tmpRootPath), pos, want)
			})
		}

		h.config.UseBinaryPkgCache = false
	}

	for pos, want := range cases.wantDefinition {
		tbRun(t, fmt.Sprintf("definition-%s", strings.Replace(pos, "/", "-", -1)), func(t testing.TB) {
			definitionTest(t, ctx, c, rootURI, pos, want, "")
		})
	}

	for pos, want := range cases.wantTypeDefinition {
		tbRun(t, fmt.Sprintf("typedefinition-%s", strings.Replace(pos, "/", "-", -1)), func(t testing.TB) {
			typeDefinitionTest(t, ctx, c, rootURI, pos, want, "")
		})
	}

	for pos, want := range cases.wantXDefinition {
		tbRun(t, fmt.Sprintf("xdefinition-%s", strings.Replace(pos, "/", "-", -1)), func(t testing.TB) {
			xdefinitionTest(t, ctx, c, rootURI, pos, want)
		})
	}

	for pos, want := range cases.wantReferences {
		tbRun(t, fmt.Sprintf("references-%s", pos), func(t testing.TB) {
			referencesTest(t, ctx, c, rootURI, pos, want)
		})
	}

	for pos, want := range cases.wantImplementation {
		tbRun(t, fmt.Sprintf("implementation-%s", pos), func(t testing.TB) {
			implementationTest(t, ctx, c, rootURI, pos, want)
		})
	}

	for file, want := range cases.wantSymbols {
		tbRun(t, fmt.Sprintf("symbols-%s", file), func(t testing.TB) {
			symbolsTest(t, ctx, c, rootURI, file, want)
		})
	}

	for params, want := range cases.wantWorkspaceSymbols {
		tbRun(t, fmt.Sprintf("workspaceSymbols(%v)", *params), func(t testing.TB) {
			workspaceSymbolsTest(t, ctx, c, rootURI, *params, want)
		})
	}

	for pos, want := range cases.wantSignatures {
		tbRun(t, fmt.Sprintf("signature-%s", strings.Replace(pos, "/", "-", -1)), func(t testing.TB) {
			signatureTest(t, ctx, c, rootURI, pos, want)
		})
	}

	for params, want := range cases.wantWorkspaceReferences {
		tbRun(t, fmt.Sprintf("workspaceReferences"), func(t testing.TB) {
			workspaceReferencesTest(t, ctx, c, rootURI, *params, want)
		})
	}

	for file, want := range cases.wantFormatting {
		tbRun(t, fmt.Sprintf("formatting-%s", file), func(t testing.TB) {
			formattingTest(t, ctx, c, rootURI, file, want)
		})
	}
}

// tbRun calls (testing.T).Run or (testing.B).Run.
func tbRun(t testing.TB, name string, f func(testing.TB)) bool {
	switch tb := t.(type) {
	case *testing.B:
		return tb.Run(name, func(b *testing.B) { f(b) })
	case *testing.T:
		return tb.Run(name, func(t *testing.T) { f(t) })
	default:
		panic(fmt.Sprintf("unexpected %T, want *testing.B or *testing.T", tb))
	}
}

func uriJoin(base lsp.DocumentURI, file string) lsp.DocumentURI {
	return lsp.DocumentURI(string(base) + "/" + file)
}

func hoverTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	hover, err := callHover(ctx, c, uriJoin(rootURI, file), line, char)
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
