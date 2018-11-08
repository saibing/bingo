// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package source

import (
	"github.com/saibing/bingo/langserver/util"
	"github.com/saibing/bingo/pkg/lsp"
)

// NOTICE: Code adapted from https://github.com/golang/tools/blob/master/internal/lsp/source/uri.go.

// FromURI gets the file path for a given URI.
// It will return an error if the uri is not valid, or if the URI was not
// a file URI
func FromURI(uri lsp.DocumentURI) (string, error) {
	return util.UriToPath(uri), nil
}

// ToURI returns a protocol URI for the supplied path.
// It will always have the file scheme.
func ToURI(path string) lsp.DocumentURI {
	return util.PathToURI(path)
}
