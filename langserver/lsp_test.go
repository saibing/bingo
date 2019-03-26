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
	h          jsonrpc2.Handler
	conn       *jsonrpc2.Conn
	connServer *jsonrpc2.Conn
	ctx        context.Context
	exported   *packagestest.Exported
}

func newTestContext(style cache.CacheStyle) *TestContext {
	cfg := NewDefaultConfig()
	cfg.DisableFuncSnippet = false
	cfg.GlobalCacheStyle = string(style)

	h := NewHandler(cfg)
	ctx := context.Background()
	return &TestContext{
		h:   h,
		ctx: ctx,
	}
}

func (tx *TestContext) setup(t *testing.T) {
	t.Helper()
	tx.exported = packagestest.Export(t, packagestest.Modules, testdata)
	tx.initServer(t)
}

func (tx *TestContext) tearDown() {
	if tx.exported != nil {
		fmt.Printf("clean up module project %s\n", tx.root())
		tx.exported.Cleanup()
	}

	if tx.conn != nil {
		if err := tx.conn.Close(); err != nil {
			log.Fatal("conn.Close:", err)
		}
	}

	if tx.connServer != nil {
		if err := tx.connServer.Close(); err != nil {
			log.Fatal("connServer.Close:", err)
		}
	}
}

func (tx *TestContext) root() string {
	return tx.exported.Config.Dir
}

func (tx *TestContext) initServer(t *testing.T) {
	t.Helper()
	rootDir := tx.root()
	os.Chdir(rootDir)
	root := util.PathToURI(filepath.ToSlash(rootDir))
	t.Log("rootUri:", root)

	// Prepare the connection.
	client, server := net.Pipe()
	tx.connServer = jsonrpc2.NewConn(tx.ctx, jsonrpc2.NewBufferedStream(server, jsonrpc2.VSCodeObjectCodec{}), tx.h)
	tx.conn = jsonrpc2.NewConn(tx.ctx, jsonrpc2.NewBufferedStream(client, jsonrpc2.VSCodeObjectCodec{}), tx.h)

	tdCap := lsp.TextDocumentClientCapabilities{}
	tdCap.Completion.CompletionItemKind.ValueSet = []lsp.CompletionItemKind{lsp.CIKConstant}
	params := InitializeParams{
		InitializeParams: lsp.InitializeParams{
			RootURI:      root,
			Capabilities: lsp.ClientCapabilities{TextDocument: tdCap},
		},

		RootImportPath: rootImportPath,
	}
	if err := tx.conn.Call(tx.ctx, "initialize", params, nil); err != nil {
		t.Fatal("conn.Call initialize:", err)
	}
}

// tbRun calls (testing.T).Run or (testing.B).Run.
func tbRun(t testing.TB, name string, f func(testing.TB)) bool {
	t.Helper()
	switch tb := t.(type) {
	case *testing.B:
		return tb.Run(name, func(b *testing.B) {
			b.Helper()
			f(b)
		})
	case *testing.T:
		return tb.Run(name, func(t *testing.T) {
			t.Helper()
			f(t)
		})
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
