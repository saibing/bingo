package langserver

import (
	"go/build"
	"golang.org/x/net/context"

	"github.com/saibing/bingo/langserver/internal/util"
)

// BuildContext creates a build.Context which uses the overlay FS and the InitializeParams.BuildContext overrides.
func (h *LangHandler) BuildContext(ctx context.Context) *build.Context {
	var bctx *build.Context
	if override := h.init.BuildContext; override != nil {
		bctx = &build.Context{
			GOOS:        override.GOOS,
			GOARCH:      override.GOARCH,
			GOPATH:      override.GOPATH,
			GOROOT:      override.GOROOT,
			CgoEnabled:  override.CgoEnabled,
			UseAllFiles: override.UseAllFiles,
			Compiler:    override.Compiler,
			BuildTags:   override.BuildTags,

			// Enable analysis of all go version build tags that
			// our compiler should understand.
			ReleaseTags: build.Default.ReleaseTags,
		}
	} else {
		// make a copy since we will mutate it
		copy := build.Default
		bctx = &copy
	}

	h.Mu.Lock()
	fs := h.FS
	h.Mu.Unlock()

	util.PrepareContext(bctx, ctx, fs)
	return bctx
}



