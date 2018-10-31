package langserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/saibing/bingo/pkg/lspext"
	"github.com/sourcegraph/ctxvfs"
	"github.com/sourcegraph/jsonrpc2"
)

type serverTestCase struct {
	skip    bool
	rootURI lsp.DocumentURI
	fs      map[string]string
	mountFS map[string]map[string]string // mount dir -> map VFS
	cases   lspTestCases
}

var serverTestCases = map[string]serverTestCase{
	"go basic": {
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a.go": "package p; func A() { A() }",
			"b.go": "package p; func B() { A() }",
		},
		cases: lspTestCases{
			wantFormatting: map[string]map[string]string{
				"a.go": map[string]string{
					"0:0-1:0": "package p\n\nfunc A() { A() }\n",
				},
			},
		},
	},
	"go xtest": {
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a.go":      "package p; var A int",
			"x_test.go": `package p_test; import "test/pkg"; var X = p.A`,
			"y_test.go": "package p_test; func Y() int { return X }",

			// non xtest to ensure we don't mix up xtest and test.
			"a_test.go": `package p; var X = A`,
			"b_test.go": "package p; func Y() int { return X }",
		},
		cases: lspTestCases{
			wantWorkspaceReferences: map[*lspext.WorkspaceReferencesParams][]string{
				{Query: lspext.SymbolDescriptor{}}: {
					"/src/test/pkg/x_test.go:1:24-1:34 -> id:test/pkg name: package:test/pkg packageName:p recv: vendor:false",
					"/src/test/pkg/x_test.go:1:46-1:47 -> id:test/pkg/-/A name:A package:test/pkg packageName:p recv: vendor:false",
				},
			},
		},
	},
	"go subdirectory in repo": {
		rootURI: "file:///src/test/pkg/d",
		fs: map[string]string{
			"a.go":    "package d; func A() { A() }",
			"d2/b.go": `package d2; import "test/pkg/d"; func B() { d.A(); B() }`,
		},
		cases: lspTestCases{
			wantWorkspaceReferences: map[*lspext.WorkspaceReferencesParams][]string{
				// Non-matching name query.
				{Query: lspext.SymbolDescriptor{"name": "nope"}}: {},

				// Matching against invalid field name.
				{Query: lspext.SymbolDescriptor{"nope": "A"}}: {},

				// Matching against an invalid dirs hint.
				{Query: lspext.SymbolDescriptor{"package": "test/pkg/d"}, Hints: map[string]interface{}{"dirs": []string{"file:///src/test/pkg/d/d3"}}}: {},

				// Matching against a dirs hint with multiple dirs.
				{Query: lspext.SymbolDescriptor{"package": "test/pkg/d"}, Hints: map[string]interface{}{"dirs": []string{"file:///src/test/pkg/d/d2", "file:///src/test/pkg/d/invalid"}}}: {
					"/src/test/pkg/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false",
					"/src/test/pkg/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false",
				},

				// Matching against a dirs hint.
				{Query: lspext.SymbolDescriptor{"package": "test/pkg/d"}, Hints: map[string]interface{}{"dirs": []string{"file:///src/test/pkg/d/d2"}}}: {
					"/src/test/pkg/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false",
					"/src/test/pkg/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false",
				},

				// Matching against single field.
				{Query: lspext.SymbolDescriptor{"package": "test/pkg/d"}}: {
					"/src/test/pkg/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false",
					"/src/test/pkg/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false",
				},

				// Matching against no fields.
				{Query: lspext.SymbolDescriptor{}}: {
					"/src/test/pkg/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false",
					"/src/test/pkg/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false",
				},
				{
					Query: lspext.SymbolDescriptor{
						"name":        "",
						"package":     "test/pkg/d",
						"packageName": "d",
						"recv":        "",
						"vendor":      false,
					},
				}: {"/src/test/pkg/d/d2/b.go:1:20-1:32 -> id:test/pkg/d name: package:test/pkg/d packageName:d recv: vendor:false"},
				{
					Query: lspext.SymbolDescriptor{
						"name":        "A",
						"package":     "test/pkg/d",
						"packageName": "d",
						"recv":        "",
						"vendor":      false,
					},
				}: {"/src/test/pkg/d/d2/b.go:1:47-1:48 -> id:test/pkg/d/-/A name:A package:test/pkg/d packageName:d recv: vendor:false"},
			},
		},
	},
	"goroot": {
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a.go": `package p; import "fmt"; var _ = fmt.Println; var x int`,
		},
		mountFS: map[string]map[string]string{
			"/goroot": {
				"src/fmt/print.go":       "package fmt; func Println(a ...interface{}) (n int, err error) { return }",
				"src/builtin/builtin.go": "package builtin; type int int",
			},
		},
		cases: lspTestCases{
			wantWorkspaceReferences: map[*lspext.WorkspaceReferencesParams][]string{
				{Query: lspext.SymbolDescriptor{}}: {
					"/src/test/pkg/a.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
					"/src/test/pkg/a.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
				},
			},
		},
	},
	"gopath": {
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a/a.go": `package a; func A() {}`,
			"b/b.go": `package b; import "test/pkg/a"; var _ = a.A`,
		},
		cases: lspTestCases{
			wantWorkspaceReferences: map[*lspext.WorkspaceReferencesParams][]string{
				{Query: lspext.SymbolDescriptor{}}: {
					"/src/test/pkg/b/b.go:1:19-1:31 -> id:test/pkg/a name: package:test/pkg/a packageName:a recv: vendor:false",
					"/src/test/pkg/b/b.go:1:43-1:44 -> id:test/pkg/a/-/A name:A package:test/pkg/a packageName:a recv: vendor:false",
				},
			},
		},
	},
	"go external dep": {
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a.go": `package a; import "github.com/d/dep"; var _ = dep.D; var _ = dep.D`,
		},
		mountFS: map[string]map[string]string{
			"/src/github.com/d/dep": {
				"d.go": "package dep; func D() {}; var _ = D",
			},
		},
		cases: lspTestCases{
			wantWorkspaceReferences: map[*lspext.WorkspaceReferencesParams][]string{
				{Query: lspext.SymbolDescriptor{}}: {
					"/src/test/pkg/a.go:1:19-1:37 -> id:github.com/d/dep name: package:github.com/d/dep packageName:dep recv: vendor:false",
					"/src/test/pkg/a.go:1:51-1:52 -> id:github.com/d/dep/-/D name:D package:github.com/d/dep packageName:dep recv: vendor:false",
					"/src/test/pkg/a.go:1:66-1:67 -> id:github.com/d/dep/-/D name:D package:github.com/d/dep packageName:dep recv: vendor:false",
				},
			},
		},
	},
	"go external dep at subtree": {
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a.go": `package a; import "github.com/d/dep/subp"; var _ = subp.D`,
		},
		mountFS: map[string]map[string]string{
			"/src/github.com/d/dep": {
				"subp/d.go": "package subp; func D() {}",
			},
		},
		cases: lspTestCases{
			wantWorkspaceReferences: map[*lspext.WorkspaceReferencesParams][]string{
				{Query: lspext.SymbolDescriptor{}}: {
					"/src/test/pkg/a.go:1:19-1:42 -> id:github.com/d/dep/subp name: package:github.com/d/dep/subp packageName:subp recv: vendor:false",
					"/src/test/pkg/a.go:1:57-1:58 -> id:github.com/d/dep/subp/-/D name:D package:github.com/d/dep/subp packageName:subp recv: vendor:false",
				},
			},
		},
	},
	"go nested external dep": { // a depends on dep1, dep1 depends on dep2
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a.go": `package a; import "github.com/d/dep1"; var _ = dep1.D1().D2`,
		},
		mountFS: map[string]map[string]string{
			"/src/github.com/d/dep1": {
				"d1.go": `package dep1; import "github.com/d/dep2"; func D1() dep2.D2 { return dep2.D2{} }`,
			},
			"/src/github.com/d/dep2": {
				"d2.go": "package dep2; type D2 struct { D2 int }",
			},
		},
		cases: lspTestCases{
			wantWorkspaceReferences: map[*lspext.WorkspaceReferencesParams][]string{
				{Query: lspext.SymbolDescriptor{}}: {
					"/src/test/pkg/a.go:1:19-1:38 -> id:github.com/d/dep1 name: package:github.com/d/dep1 packageName:dep1 recv: vendor:false",
					"/src/test/pkg/a.go:1:58-1:60 -> id:github.com/d/dep2/-/D2/D2 name:D2 package:github.com/d/dep2 packageName:dep2 recv:D2 vendor:false",
					"/src/test/pkg/a.go:1:53-1:55 -> id:github.com/d/dep1/-/D1 name:D1 package:github.com/d/dep1 packageName:dep1 recv: vendor:false",
				},
			},
		},
	},
	"workspace references multiple files": {
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a.go": `package p; import "fmt"; var _ = fmt.Println; var x int`,
			"b.go": `package p; import "fmt"; var _ = fmt.Println; var y int`,
			"c.go": `package p; import "fmt"; var _ = fmt.Println; var z int`,
		},
		mountFS: map[string]map[string]string{
			"/goroot": {
				"src/fmt/print.go":       "package fmt; func Println(a ...interface{}) (n int, err error) { return }",
				"src/builtin/builtin.go": "package builtin; type int int",
			},
		},
		cases: lspTestCases{
			wantWorkspaceReferences: map[*lspext.WorkspaceReferencesParams][]string{
				{Query: lspext.SymbolDescriptor{}}: {
					"/src/test/pkg/a.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
					"/src/test/pkg/a.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
					"/src/test/pkg/b.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
					"/src/test/pkg/b.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
					"/src/test/pkg/c.go:1:19-1:24 -> id:fmt name: package:fmt packageName:fmt recv: vendor:false",
					"/src/test/pkg/c.go:1:38-1:45 -> id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false",
				},
			},
		},
	},
	"signatures": {
		rootURI: "file:///src/test/pkg",
		fs: map[string]string{
			"a.go": `package p

				// Comments for A
				func A(foo int, bar func(baz int) int) int {
					return bar(foo)
				}


				func B() {}

				// Comments for C
				func C(x int, y int) int {
					return x+y
				}`,
			"b.go": "package p; func main() { B(); A(); A(0,); A(0); C(1,2) }",
		},
		cases: lspTestCases{
			wantSignatures: map[string]string{
				"b.go:1:28": "func() 0",
				"b.go:1:33": "func(foo int, bar func(baz int) int) int Comments for A\n 0",
				"b.go:1:40": "func(foo int, bar func(baz int) int) int Comments for A\n 1",
				"b.go:1:46": "func(foo int, bar func(baz int) int) int Comments for A\n 0",
				"b.go:1:51": "func(x int, y int) int Comments for C\n 0",
				"b.go:1:53": "func(x int, y int) int Comments for C\n 1",
				"b.go:1:54": "func(x int, y int) int Comments for C\n 1",
			},
		},
	},
	"unexpected paths": {
		// notice the : and @ symbol
		rootURI: "file:///src/t:est/@hello/pkg",
		skip:    runtime.GOOS == "windows", // this test is not supported on windows
		fs: map[string]string{
			"a.go": "package p; func A() { A() }",
		},
		cases: lspTestCases{
			wantHover: map[string]string{
				"a.go:1:17": "func A()",
			},
			wantReferences: map[string][]string{
				"a.go:1:17": {
					"/src/t:est/@hello/pkg/a.go:1:17",
					"/src/t:est/@hello/pkg/a.go:1:23",
				},
			},
		},
	},
}

func TestServer(t *testing.T) {
	for label, test := range serverTestCases {
		t.Run(label, func(t *testing.T) {
			if test.skip {
				t.Skip()
				return
			}

			cfg := NewDefaultConfig()
			cfg.FuncSnippetEnabled = true
			cfg.GocodeCompletionEnabled = true
			cfg.UseBinaryPkgCache = false

			h := &LangHandler{
				DefaultConfig: cfg,
				HandlerShared: &HandlerShared{},
			}

			addr, done := startServer(t, jsonrpc2.HandlerWithError(h.handle))
			defer done()
			conn := dialServer(t, addr)
			defer func() {
				if err := conn.Close(); err != nil {
					t.Fatal("conn.Close:", err)
				}
			}()

			rootFSPath := util.UriToPath(test.rootURI)

			// Prepare the connection.
			ctx := context.Background()
			tdCap := lsp.TextDocumentClientCapabilities{}
			tdCap.Completion.CompletionItemKind.ValueSet = []lsp.CompletionItemKind{lsp.CIKConstant}
			if err := conn.Call(ctx, "initialize", InitializeParams{
				InitializeParams: lsp.InitializeParams{
					RootURI:      test.rootURI,
					Capabilities: lsp.ClientCapabilities{TextDocument: tdCap},
				},
				NoOSFileSystemAccess: true,
				RootImportPath:       strings.TrimPrefix(rootFSPath, "/src/"),
				BuildContext: &InitializeBuildContextParams{
					GOOS:     "linux",
					GOARCH:   "amd64",
					GOPATH:   "/",
					GOROOT:   "/goroot",
					Compiler: runtime.Compiler,
				},
			}, nil); err != nil {
				t.Fatal("initialize:", err)
			}

			h.Mu.Lock()
			h.FS.Bind(rootFSPath, mapFS(test.fs), "/", ctxvfs.BindReplace)
			for mountDir, fs := range test.mountFS {
				h.FS.Bind(mountDir, mapFS(fs), "/", ctxvfs.BindAfter)
			}
			h.Mu.Unlock()

			lspTests(t, ctx, h, conn, test.rootURI, test.cases)
		})
	}
}

func startServer(t testing.TB, h jsonrpc2.Handler) (addr string, done func()) {
	bindAddr := ":0"
	if os.Getenv("CI") != "" || runtime.GOOS == "windows" {
		// CircleCI has issues with IPv6 (e.g., "dial tcp [::]:39984:
		// connect: network is unreachable").
		// Similar error is happens on Windows:
		// "dial tcp [::]:61898: connectex: The requested address is not valid in its context."
		bindAddr = "127.0.0.1:0"
	}
	l, err := net.Listen("tcp", bindAddr)
	if err != nil {
		t.Fatal("Listen:", err)
	}
	go func() {
		if err := serve(context.Background(), l, h); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			t.Fatal("jsonrpc2.Serve:", err)
		}
	}()
	return l.Addr().String(), func() {
		if err := l.Close(); err != nil {
			t.Fatal("close listener:", err)
		}
	}
}

func serve(ctx context.Context, lis net.Listener, h jsonrpc2.Handler, opt ...jsonrpc2.ConnOpt) error {
	for {
		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		jsonrpc2.NewConn(ctx, jsonrpc2.NewBufferedStream(conn, jsonrpc2.VSCodeObjectCodec{}), h, opt...)
	}
}

func dialServer(t testing.TB, addr string, h ...*jsonrpc2.HandlerWithErrorConfigurer) *jsonrpc2.Conn {
	conn, err := (&net.Dialer{}).Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	handler := jsonrpc2.HandlerWithError(func(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) (interface{}, error) {
		// no-op
		return nil, nil
	})
	if len(h) == 1 {
		handler = h[0]
	}

	return jsonrpc2.NewConn(
		context.Background(),
		jsonrpc2.NewBufferedStream(conn, jsonrpc2.VSCodeObjectCodec{}),
		handler,
	)
}

type lspTestCases struct {
	wantHover, overrideGodefHover           map[string]string
	wantDefinition, overrideGodefDefinition map[string]string
	wantTypeDefinition, wantXDefinition     map[string]string
	wantCompletion                          map[string]string
	wantReferences                          map[string][]string
	wantImplementation                      map[string][]string
	wantSymbols                             map[string][]string
	wantSignatures                          map[string]string
	wantWorkspaceReferences                 map[*lspext.WorkspaceReferencesParams][]string
	wantFormatting                          map[string]map[string]string
}

// lspTests runs all test suites for LSP functionality.
func lspTests(t testing.TB, ctx context.Context, h *LangHandler, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, cases lspTestCases) {
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

func uriJoin(base lsp.DocumentURI, file string) lsp.DocumentURI {
	return lsp.DocumentURI(string(base) + "/" + file)
}


func signatureTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
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

func workspaceReferencesTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, params lspext.WorkspaceReferencesParams, want []string) {
	references, err := callWorkspaceReferences(ctx, c, params)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(references, want) {
		t.Errorf("\ngot  %q\nwant %q", references, want)
	}
}

func formattingTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, file string, want map[string]string) {
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



func qualifiedName(s lsp.SymbolInformation) string {
	if s.ContainerName != "" {
		return s.ContainerName + "." + s.Name
	}
	return s.Name
}

func callWorkspaceReferences(ctx context.Context, c *jsonrpc2.Conn, params lspext.WorkspaceReferencesParams) ([]string, error) {
	var references []lspext.ReferenceInformation
	err := c.Call(ctx, "workspace/xreferences", params, &references)
	if err != nil {
		return nil, err
	}
	refs := make([]string, len(references))
	for i, r := range references {
		locationURI := util.UriToPath(r.Reference.URI)
		start := r.Reference.Range.Start
		end := r.Reference.Range.End
		refs[i] = fmt.Sprintf("%s:%d:%d-%d:%d -> %v", locationURI, start.Line+1, start.Character+1, end.Line+1, end.Character+1, r.Symbol)
	}
	return refs, nil
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

func callFormatting(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI) ([]lsp.TextEdit, error) {
	var edits []lsp.TextEdit
	err := c.Call(ctx, "textDocument/formatting", lsp.DocumentFormattingParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
	}, &edits)
	return edits, err
}

type markedStrings []lsp.MarkedString

func (v *markedStrings) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return errors.New("invalid empty JSON")
	}
	if data[0] == '[' {
		var ms []markedString
		if err := json.Unmarshal(data, &ms); err != nil {
			return err
		}
		for _, ms := range ms {
			*v = append(*v, lsp.MarkedString(ms))
		}
		return nil
	}
	*v = []lsp.MarkedString{{}}
	return json.Unmarshal(data, &(*v)[0])
}

type markedString lsp.MarkedString

func (v *markedString) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return errors.New("invalid empty JSON")
	}
	if data[0] == '{' {
		return json.Unmarshal(data, (*lsp.MarkedString)(v))
	}

	// String
	*v = markedString{}
	return json.Unmarshal(data, &v.Value)
}

type locations []lsp.Location

func (v *locations) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return errors.New("invalid empty JSON")
	}
	if data[0] == '[' {
		return json.Unmarshal(data, (*[]lsp.Location)(v))
	}
	*v = []lsp.Location{{}}
	return json.Unmarshal(data, &(*v)[0])
}


// mapFS lets us easily instantiate a VFS with a map[string]string
// (which is less noisy than map[string][]byte in test fixtures).
func mapFS(m map[string]string) ctxvfs.FileSystem {
	m2 := make(map[string][]byte, len(m))
	for k, v := range m {
		m2[k] = []byte(v)
	}
	return ctxvfs.Map(m2)
}
