package langserver

import (
	"context"
	"flag"
	"github.com/sourcegraph/go-langserver/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	flag.Parse()
	
	Init("./test/pkg")

	exitCode := m.Run()

	os.Exit(exitCode)
}

func Init(root lsp.DocumentURI) {
	cfg := NewDefaultConfig()
	cfg.FuncSnippetEnabled = true
	cfg.GocodeCompletionEnabled = true
	cfg.UseBinaryPkgCache = false

	h := &LangHandler{
		DefaultConfig: cfg,
		HandlerShared: &HandlerShared{},
	}

	addr, done := startLanguageServer(jsonrpc2.HandlerWithError(h.handle))
	defer done()
	conn := dialLanguageServer(addr)
	defer func() {
		if err := conn.Close(); err != nil {
			log.Fatal("conn.Close", err)
		}
	}()

	// Prepare the connection.
	ctx := context.Background()
	tdCap := lsp.TextDocumentClientCapabilities{}
	tdCap.Completion.CompletionItemKind.ValueSet = []lsp.CompletionItemKind{lsp.CIKConstant}
	if err := conn.Call(ctx, "initialize", InitializeParams{
		InitializeParams: lsp.InitializeParams{
			RootURI:      root,
			Capabilities: lsp.ClientCapabilities{TextDocument: tdCap},
		},
		NoOSFileSystemAccess: true,
		RootImportPath:       strings.TrimPrefix(string(root), "/src/"),
		BuildContext: &InitializeBuildContextParams{
			GOOS:     runtime.GOOS,
			GOARCH:   runtime.GOARCH,
			GOPATH:   os.Getenv("GOPATH"),
			GOROOT:   runtime.GOROOT(),
			Compiler: runtime.Compiler,
		},
	}, nil); err != nil {
		log.Fatal("conn.Call", err)
	}

	//h.Mu.Lock()
	//h.FS.Bind(rootFSPath, mapFS(test.fs), "/", ctxvfs.BindReplace)
	//for mountDir, fs := range test.mountFS {
	//	h.FS.Bind(mountDir, mapFS(fs), "/", ctxvfs.BindAfter)
	//}
	//h.Mu.Unlock()
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
