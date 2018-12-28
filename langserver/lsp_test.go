package langserver

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/tools/go/packages/packagestest"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"

	_ "net/http/pprof"
)

var h *LangHandler
var conn *jsonrpc2.Conn
var ctx context.Context

var exported *packagestest.Exported

var testdata = []packagestest.Module{
	{
		Name: "github.com/saibing/bingo/langserver/test/pkg",
		Files: map[string]interface{}{
			"basic/a.go": `package p; func A() { A() }`,
			"basic/b.go": `package p; func B() { A() }`,

			"builtin/a.go":`package p; func A() { println("hello") }`,

			"detailed/a.go": `package p; type T struct { F string }`,

			"exported_on_unexported/a.go": `package p; type t struct { F string }`,

			"gomodule/a.go": `package a; import "github.com/saibing/dep"; var _ = dep.D; var _ = dep.D`,
			"gomodule/b.go": `package a; import "github.com/saibing/dep/subp"; var _ = subp.D`,
			"gomodule/c.go": `package a; import "github.com/saibing/dep/dep1"; var _ = dep1.D1().D2`,

			"goproject/a/a.go": `package a; func A() {}`,
			"goproject/b/b.go": `package b; import "github.com/saibing/bingo/langserver/test/pkg/goproject/a"; var _ = a.A`,

			"goroot/a.go": `package p; import "fmt"; var _ = fmt.Println; var x int`,

			"implementations/i0.go":    `package p; type I0 interface { M0() }`,
			"implementations/i1.go":    `package p; type I1 interface { M1() }`,
			"implementations/i2.go":    `package p; type I2 interface { M1(); M2() }`,
			"implementations/t0.go":    `package p; type T0 struct{}`,
			"implementations/t1.go":    `package p; type T1 struct {}; func (T1) M1() {}; func (T1) M3(){}`,
			"implementations/t1e.go":   `package p; type T1E struct { T1 }; var _ = (T1E{}).M1`,
			"implementations/t1p.go":   `package p; type T1P struct {}; func (*T1P) M1() {}`,
			"implementations/p2/p2.go": `package p2; type T2 struct{}; func (T2) M1() {}`,

			"lookup/a/a.go": `package a; type A int; func A1() A { var A A = 1; return A }`,
			"lookup/b/b.go": `package b; import "github.com/saibing/bingo/langserver/test/pkg/lookup/a"; func Dummy() a.A { x := a.A1(); return x }`,
			"lookup/c/c.go": `package c; import "github.com/saibing/bingo/langserver/test/pkg/lookup/a"; func Dummy() **a.A { var x **a.A; return x }`,
			"lookup/d/d.go": `package d; import "github.com/saibing/bingo/langserver/test/pkg/lookup/a"; func Dummy() map[string]a.A { var x map[string]a.A; return x }`,

			"multiple/a.go": `package p; func A() { A() }`,
			"multiple/main.go": `// +build ignore
			
package main;  func B() { p.A(); B() }`,

			"workspace_multiple/a.go": `package p; import "fmt"; var _ = fmt.Println; var x int`,
			"workspace_multiple/b.go": `package p; import "fmt"; var _ = fmt.Println; var y int`,
			"workspace_multiple/c.go": `package p; import "fmt"; var _ = fmt.Println; var z int`,

			"subdirectory/a.go":    `package d; func A() { A() }`,
			"subdirectory/d2/b.go": `package d2; import "github.com/saibing/bingo/langserver/test/pkg/subdirectory"; func B() { d.A(); B() }`,

			"typealias/a.go": `package p; type A struct{ a int }`,
			"typealias/b.go": `package p; type B = A`,

			"unexpected_paths/a.go": `package p; func A() { A() }`,

			"xreferences/a.go": `package p; import "fmt"; var _ = fmt.Println; var x int`,
			"xreferences/b.go": `package p; import "fmt"; var _ = fmt.Println; var y int`,
			"xreferences/c.go": `package p; import "fmt"; var _ = fmt.Println; var z int`,

			"test/a.go":      `package p; var A int`,
			"test/a_test.go": `package p; import "testing"; import "github.com/saibing/bingo/langserver/test/pkg/test/b"; var X = b.B; func TestB(t *testing.T) {}`,
			"test/b/b.go":    `package b; var B int; func C() int { return B };`,
			"test/c/c.go":    `package c; import "github.com/saibing/bingo/langserver/test/pkg/test/b"; var X = b.B;`,

			"xtest/a.go":      `package p; var A int`,
			"xtest/a_test.go": `package p; var X = A`,
			"xtest/b_test.go": `package p; func Y() int { return X }`,
			"xtest/x_test.go": `package p_test; import "github.com/saibing/bingo/langserver/test/pkg/xtest"; var X = p.A`,
			"xtest/y_test.go": `package p_test; func Y() int { return X }`,

			"renaming/a.go": `package p
import "fmt"

func main() {
	str := A()
	fmt.Println(str)
}

func A() string {
	return "test"
}
`,

			"symbols/abc.go": `package a

type XYZ struct {}

func (x XYZ) ABC() {}

var (
	A = 1
)

const (
	B = 2
)

type (
	_ struct{}
	C struct{}
)

type UVW interface {}

type T string`,
			"symbols/bcd.go": `package a

type YZA struct {}

func (y YZA) BCD() {}`,
			"symbols/cde.go": `package a

var(
	a, b string
	c int
)`,
			"symbols/xyz.go": `package a

func yza() {}`,

			"signature/a.go": `package p

// Comments for A
func A(foo int, bar func(baz int) int) int {
	return bar(foo)
}


func B() {}

// Comments for C
func C(x int, y int) int {
	return x+y
}
`,
			"signature/b.go": `package p; func main() { B(); A(); A(0,); A(0); C(1,2) }`,

			"issue/223.go": `package main

import (
	"fmt"
)

func main() {

	b := &Hello{
		a: 1,
	}

	fmt.Println(b.Bye())
}

type Hello struct {
	a int
}

func (h *Hello) Bye() int {
	return h.a
}`,
			"issue/261.go": `package main

import "fmt"

type T string
type TM map[string]T

func main() {
	var tm TM
	for _, t := range tm {
		fmt.Println(t)
	}
}`,

			"docs/a.go": `// Copyright 2015 someone.
// Copyrights often span multiple lines.

// Some additional non-package docs.

// Package p is a package with lots of great things.
package p

import "github.com/saibing/dep/pkg2"

// logit is pkg2.X
var logit = pkg2.X

// T is a struct.
type T struct {
	// F is a string field.
	F string

	// H is a header.
	H pkg2.Header
}

// Foo is the best string.
var Foo string

var (
	// I1 is an int
	I1 = 1

	// I2 is an int
	I2 = 3
)`,
			"docs/q.go": `package p
type T2 struct {
	Q string // Q is a string field.
	// X is documented.
	X int // X has comments.
}`,

			"different/abc.go": `package a
type XYZ struct {}`,
			"different/bcd.go": `package a
func (x XYZ) ABC() {}`,

			"completion/a.go": `package p

import "strings"

func s2() {
	_ = strings.Title("s")
	_ = new(strings.Replacer)
}

const s1 = 42

var s3 int
var s4 func()`,
			"completion/b.go": `package p; import "fmt"; var _ = fmt.Printl`,
		},
	},
}

const (
	goroot         = "goroot"
	gomodule       = "gomodule"
	rootImportPath = "github.com/saibing/bingo/langserver/test/pkg"
)

func getGOPATH() string {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		return filepath.Join(os.Getenv("HOME"), "go")
	}

	paths := strings.Split(gopath, string(os.PathListSeparator))
	return paths[0]
}

var (
	gopathDir    = getGOPATH()
	githubModule = "pkg/mod/github.com/saibing/dep@v1.0.2"
	gomoduleDir  = filepath.Join(gopathDir, githubModule)
)

func initServer(rootDir string) {
	root := util.PathToURI(filepath.ToSlash(rootDir))
	fmt.Printf("root uri is %s\n", root)
	cfg := NewDefaultConfig()
	cfg.DisableFuncSnippet = false
	cfg.EnableGlobalCache = false

	h = &LangHandler{
		DefaultConfig: cfg,
		HandlerShared: &HandlerShared{},
	}

	addr, done := startLanguageServer(jsonrpc2.HandlerWithError(h.handle))
	defer done()
	conn = dialLanguageServer(addr)
	// Prepare the connection.
	ctx = context.Background()
	tdCap := lsp.TextDocumentClientCapabilities{}
	tdCap.Completion.CompletionItemKind.ValueSet = []lsp.CompletionItemKind{lsp.CIKConstant}
	if err := conn.Call(ctx, "initialize", InitializeParams{
		InitializeParams: lsp.InitializeParams{
			RootURI:      root,
			Capabilities: lsp.ClientCapabilities{TextDocument: tdCap},
		},

		RootImportPath:       rootImportPath,
	}, nil); err != nil {
		log.Fatal("conn.Call", err)
	}
}

func TestMain(m *testing.M) {
	flag.Parse()

	exported = packagestest.Export2("bingo", packagestest.Modules, testdata)
	defer exported.Cleanup()

	defer func() {
		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Fatal("conn.Close", err)
			}
		}
	}()

	initServer(exported.Config.Dir)
	code := m.Run()
	os.Exit(code)
}

func startLanguageServer(h jsonrpc2.Handler) (addr string, done func()) {
	go func() {
		log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	}()

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
		log.Fatal("net.Listen", err)
	}
	go func() {
		if err := serveServer(context.Background(), l, h); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			log.Fatal("jsonrpc2.Serve:", err)
		}
	}()
	return l.Addr().String(), func() {
		if err := l.Close(); err != nil {
			log.Fatal("close listener:", err)
		}
	}
}

func serveServer(ctx context.Context, lis net.Listener, h jsonrpc2.Handler, opt ...jsonrpc2.ConnOpt) error {
	for {
		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		jsonrpc2.NewConn(ctx, jsonrpc2.NewBufferedStream(conn, jsonrpc2.VSCodeObjectCodec{}), h, opt...)
	}
}

func dialLanguageServer(addr string, h ...*jsonrpc2.HandlerWithErrorConfigurer) *jsonrpc2.Conn {
	conn, err := (&net.Dialer{}).Dial("tcp", addr)
	if err != nil {
		log.Fatal(err)
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

func parsePos(s string) (file string, line, char int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		err = fmt.Errorf("invalid pos %q (%d parts)", s, len(parts))
		return
	}
	file = parts[0]
	line, err = strconv.Atoi(parts[1])
	if err != nil {
		err = fmt.Errorf("invalid line in %q: %s", s, err)
		return
	}
	char, err = strconv.Atoi(parts[2])
	if err != nil {
		err = fmt.Errorf("invalid char in %q: %s", s, err)
		return
	}
	return file, line - 1, char - 1, nil // LSP is 0-indexed
}

func uriJoin(base lsp.DocumentURI, file string) lsp.DocumentURI {
	return lsp.DocumentURI(string(base) + "/" + file)
}

func qualifiedName(s lsp.SymbolInformation) string {
	if s.ContainerName != "" {
		return s.ContainerName + "." + s.Name
	}
	return s.Name
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

func makePath(elem ...string) string {
	path := filepath.Join(elem...)
	return util.LowerDriver(filepath.ToSlash(path))
}
