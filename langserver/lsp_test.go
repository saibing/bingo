package langserver

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages/packagestest"

	"github.com/saibing/bingo/langserver/internal/cache"
	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/sourcegraph/go-lsp"
	"github.com/sourcegraph/jsonrpc2"

	_ "net/http/pprof"
)

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

func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	tearDown()
	os.Exit(code)
}

func tearDown() {
	completionContext.tearDown()
	definitionContext.tearDown()
	symbolContext.tearDown()
	formatContext.tearDown()
	hoverContext.tearDown()
	implementationContext.tearDown()
	referencesContext.tearDown()
	renameContext.tearDown()
	signatureContext.tearDown()
	typeDefinitionContext.tearDown()
	workspaceReferencesContext.tearDown()
	workspaceSymbolContext.tearDown()
	xDefinitionContext.tearDown()
}

type TestContext struct {
	h        *LangHandler
	conn     *jsonrpc2.Conn
	ctx      context.Context
	exported *packagestest.Exported
}

func newTestContext(style cache.CacheStyle) *TestContext {
	cfg := NewDefaultConfig()
	cfg.DisableFuncSnippet = false
	cfg.GlobalCacheStyle = string(style)

	h := &LangHandler{
		DefaultConfig: cfg,
		HandlerShared: &HandlerShared{},
	}

	ctx := context.Background()
	tx := &TestContext{h: h, ctx: ctx}
	return tx
}

func (tx *TestContext) setup(t *testing.T) {
	tx.exported = packagestest.Export(t, packagestest.Modules, testdata)
	tx.initServer()
}

func (tx *TestContext) tearDown() {
	if tx.exported != nil {
		fmt.Printf("clean up module project %s\n", tx.exported.Config.Dir)
		tx.exported.Cleanup()
	}

	if tx.conn != nil {
		if err := tx.conn.Close(); err != nil {
			log.Fatal("conn.Close", err)
		}
	}
}

func (tx *TestContext) root() string {
	return tx.exported.Config.Dir
}

func (tx *TestContext) initServer() {
	rootDir := tx.exported.Config.Dir
	os.Chdir(rootDir)
	root := util.PathToURI(filepath.ToSlash(rootDir))
	fmt.Printf("root uri is %s\n", root)

	addr, done := tx.startLanguageServer()
	defer done()
	tx.conn = dialLanguageServer(addr)
	// Prepare the connection.

	tdCap := lsp.TextDocumentClientCapabilities{}
	tdCap.Completion.CompletionItemKind.ValueSet = []lsp.CompletionItemKind{lsp.CIKConstant}
	if err := tx.conn.Call(tx.ctx, "initialize", InitializeParams{
		InitializeParams: lsp.InitializeParams{
			RootURI:      root,
			Capabilities: lsp.ClientCapabilities{TextDocument: tdCap},
		},

		RootImportPath: rootImportPath,
	}, nil); err != nil {
		log.Fatal("conn.Call", err)
	}
}

func (tx *TestContext) startLanguageServer() (addr string, done func()) {
	// go func() {
	// 	log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	// }()

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
		err := tx.serveServer(l)
		if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			log.Fatal("jsonrpc2.Serve:", err)
		}
	}()
	return l.Addr().String(), func() {
		if err := l.Close(); err != nil {
			log.Fatal("close listener:", err)
		}
	}
}

func (tx *TestContext) serveServer(lis net.Listener, opt ...jsonrpc2.ConnOpt) error {
	h := jsonrpc2.HandlerWithError(tx.h.handle)

	for {
		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		jsonrpc2.NewConn(tx.ctx, jsonrpc2.NewBufferedStream(conn, jsonrpc2.VSCodeObjectCodec{}), h, opt...)
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
