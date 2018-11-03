package gocode

import (
	"go/build"
	"go/token"

	"golang.org/x/tools/go/packages"
)

var bctx go_build_context

func InitDaemon(bc *build.Context) {
	bctx = pack_build_context(bc)
	g_config.ProposeBuiltins = true
	g_config.Autobuild = true
	g_daemon = new(daemon)
	g_daemon.resetCache(nil, 0)
}

func AutoComplete(pkg *packages.Package, start token.Pos, file []byte, filename string, offset int) ([]candidate, int) {
	return serverAutoComplete(pkg, start, file, filename, offset, bctx)
}

// dumb vars for unused parts of the package
var (
	fals    = true
	g_debug = &fals
)

// dumb types for unused parts of the package
type (
	RPC struct{}
)
