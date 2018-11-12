package main // import "github.com/saibing/bingo"

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/saibing/bingo/langserver"
	"github.com/sourcegraph/jsonrpc2"

	_ "net/http/pprof"
)

var (
	mode         = flag.String("mode", "stdio", "communication mode (stdio|tcp)")
	addr         = flag.String("addr", ":4389", "server listen address (tcp)")
	trace        = flag.Bool("trace", false, "print all requests and responses")
	logfile      = flag.String("logfile", "", "also log to this file (in addition to stderr)")
	printVersion = flag.Bool("version", false, "print version and exit")
	pprof        = flag.String("pprof", "", "start a pprof http server (https://golang.org/pkg/net/http/pprof/)")

	// Default Config, can be overridden by InitializationOptions
	maxparallelism     = flag.Int("maxparallelism", 0, "use at max N parallel goroutines to fulfill requests. Can be overridden by InitializationOptions.")
	diagnostics        = flag.Bool("diagnostics", false, "enable diagnostics (extra memory burden). Can be overridden by InitializationOptions.")
	funcSnippetEnabled = flag.Bool("func-snippet-enabled", true, "enable argument snippets on func completion. Can be overridden by InitializationOptions.")
	formatTool         = flag.String("format-tool", "goimports", "which tool is used to format documents. Supported: goimports and gofmt. Can be overridden by InitializationOptions.")
)

// version is the version field we report back. If you are releasing a new version:
// 1. Create commit without -dev suffix.
// 2. Create commit with version incremented and -dev suffix
// 3. Push to master
// 4. Tag the commit created in (1) with the value of the version string
const version = "v2-dev"

func main() {
	flag.Parse()
	log.SetFlags(0)

	// Start pprof server, if desired.
	if *pprof != "" {
		go func() {
			log.Println(http.ListenAndServe(*pprof, nil))
		}()
	}

	cfg := langserver.NewDefaultConfig()
	cfg.FuncSnippetEnabled = *funcSnippetEnabled
	cfg.DiagnosticsEnabled = *diagnostics
	cfg.FormatTool = *formatTool

	if *maxparallelism > 0 {
		cfg.MaxParallelism = *maxparallelism
	}

	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cfg langserver.Config) error {
	if *printVersion {
		fmt.Println(version)
		return nil
	}

	var logW io.Writer
	if *logfile == "" {
		logW = os.Stderr
	} else {
		f, err := os.Create(*logfile)
		if err != nil {
			return err
		}
		defer f.Close()
		logW = io.MultiWriter(os.Stderr, f)
	}
	log.SetOutput(logW)

	var connOpt []jsonrpc2.ConnOpt
	if *trace {
		connOpt = append(connOpt, jsonrpc2.LogMessages(log.New(logW, "", 0)))
	}

	handler := langserver.NewHandler(cfg)

	switch *mode {
	case "tcp":
		lis, err := net.Listen("tcp", *addr)
		if err != nil {
			return err
		}
		defer lis.Close()

		log.Println("langserver-go: listening on", *addr)
		for {
			conn, err := lis.Accept()
			if err != nil {
				return err
			}
			jsonrpc2.NewConn(context.Background(), jsonrpc2.NewBufferedStream(conn, jsonrpc2.VSCodeObjectCodec{}), handler, connOpt...)
		}

	case "stdio":
		log.Println("langserver-go: reading on stdin, writing on stdout")
		<-jsonrpc2.NewConn(context.Background(), jsonrpc2.NewBufferedStream(stdrwc{}, jsonrpc2.VSCodeObjectCodec{}), handler, connOpt...).DisconnectNotify()
		log.Println("connection closed")
		return nil

	default:
		return fmt.Errorf("invalid mode %q", *mode)
	}
}

type stdrwc struct{}

func (stdrwc) Read(p []byte) (int, error) {
	return os.Stdin.Read(p)
}

func (stdrwc) Write(p []byte) (int, error) {
	return os.Stdout.Write(p)
}

func (stdrwc) Close() error {
	if err := os.Stdin.Close(); err != nil {
		return err
	}
	return os.Stdout.Close()
}
