package langserver

import (
	"context"
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

	"github.com/saibing/bingo/langserver/util"
	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

var h *LangHandler
var conn *jsonrpc2.Conn
var ctx context.Context

const (
	rootDir        = "test/pkg"
	rootImportPath = "github.com/saibing/bingo/langserver/test/pkg"

	basicPkgDir           = "test/pkg/basic"
	detailedPkgDir        = "test/pkg/detailed"
	xtestPkgDir           = "test/pkg/xtest"
	testPkgDir            = "test/pkg/test"
	subdirectoryPkgDir    = "test/pkg/subdirectory"
	multiplePkgDir        = "test/pkg/multiple"
	gorootPkgDir          = "test/pkg/goroot"
	goprojectPkgDir       = "test/pkg/goproject"
	gomodulePkgDir        = "test/pkg/gomodule"
	hoverDocsPkgDir       = "test/pkg/docs"
	issuePkgDir           = "test/pkg/issue"
	lookupPkgDir          = "test/pkg/lookup"
	implementationsPkgDir = "test/pkg/implementations"
	typealiasPkgDir       = "test/pkg/typealias"
	completionPkgDir      = "test/pkg/completion"
	exportedPkgDir 		  = "test/pkg/exported_on_unexported"
	symbolsPkgDir		  = "test/pkg/symbols"
	unexpectedPkgDir	  = "test/pkg/unexpected_paths"
	differentPkgDir       = "test/pkg/different"
)

var (
	currentWorkDir = ""
	gopathDir      = ""
)

func TestMain(m *testing.M) {
	fmt.Println("------main begin------")
	flag.Parse()

	dir, err := filepath.Abs(rootDir)
	if err != nil {
		log.Fatal("TestMain", err)
	}

	currentWorkDir, err = os.Getwd()
	if err != nil {
		log.Fatal("TestMain", err)
	}
	currentWorkDir += "/"

	gopathDir = getGOPATH()

	Init(util.PathToURI(dir))

	defer func() {
		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Fatal("conn.Close", err)
			}
		}
	}()

	exitCode := m.Run()

	fmt.Println("------main end------")
	os.Exit(exitCode)
}

func Init(root lsp.DocumentURI) {
	fmt.Printf("root uri is %s\n", root)
	cfg := NewDefaultConfig()
	cfg.FuncSnippetEnabled = true
	cfg.GocodeCompletionEnabled = true
	cfg.UseBinaryPkgCache = false

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
		NoOSFileSystemAccess: false,
		RootImportPath:       rootImportPath,
		BuildContext: &InitializeBuildContextParams{
			GOOS:     runtime.GOOS,
			GOARCH:   runtime.GOARCH,
			GOPATH:   gopathDir,
			GOROOT:   runtime.GOROOT(),
			Compiler: runtime.Compiler,
		},
	}, nil); err != nil {
		log.Fatal("conn.Call", err)
	}
}

func startLanguageServer(h jsonrpc2.Handler) (addr string, done func()) {
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

func basicOutput(suffix string) string {
	return genOutput(basicPkgDir, suffix)
}

func xtestOutput(suffix string) string {
	return genOutput(xtestPkgDir, suffix)
}

func testOutput(suffix string) string {
	return genOutput(testPkgDir, suffix)
}

func subdirectoryOutput(suffix string) string {
	return genOutput(subdirectoryPkgDir, suffix)
}

func multipleOutput(suffix string) string {
	return genOutput(multiplePkgDir, suffix)
}

func gorootOutput(suffix string) string {
	return getPlatformPath(runtime.GOROOT() + "/" + suffix)
}

func goprojectOutput(suffix string) string {
	return genOutput(goprojectPkgDir, suffix)
}

func gomoduleDepOutput(suffix string) string {
	depPath := filepath.Join(gopathDir, "pkg/mod/github.com/saibing/dep@v1.0.2")
	return getPlatformPath(depPath + "/" + suffix)
}

func gomoduleOutput(suffix string) string {
	return genOutput(gomodulePkgDir, suffix)
}

func lookupOutput(suffix string) string {
	return genOutput(lookupPkgDir, suffix)
}

func typealiasOutput(suffix string) string {
	return genOutput(typealiasPkgDir, suffix)
}

func implementationsOutput(suffix string) string {
	return genOutput(implementationsPkgDir, suffix)
}

func genOutput(pkgDir, suffix string) string {
	return getPlatformPath(currentWorkDir + pkgDir + "/" + suffix)
}

func getGOPATH() string {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		return filepath.Join(os.Getenv("HOME"), "go")
	}

	paths := strings.Split(gopath, string(os.PathListSeparator))
	return paths[0]
}


func getPlatformPath(path string) string {
	s := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "/" + s
	}

	return s
}