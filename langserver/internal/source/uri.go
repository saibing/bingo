// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// NOTICE: Code adapted from golang.org/x/tools/internal/lsp/source/uri.go

package source

import (
	"fmt"
	"github.com/saibing/bingo/langserver/internal/util"
	"github.com/saibing/bingo/pkg/lsp"
	"net/url"
	"path/filepath"
	"strings"
	"runtime"
)

const fileSchemePrefix = "file://"

// URI represents the full uri for a file.
type URI string

// Filename gets the file path for the URI.
// It will return an error if the uri is not valid, or if the URI was not
// a file URI
func (uri URI) Filename() (string, error) {
	return toFilename(string(uri))
}

func toFilename(uri string) (string, error) {
	if !strings.HasPrefix(uri, fileSchemePrefix) {
		return "", fmt.Errorf("only file URI's are supported, got %v", uri)
	}

	uri = uri[len(fileSchemePrefix):]
	if util.IsWindows() && uri[0] == '/' {
		uri = uri[1:]
	}

	uri, err := url.PathUnescape(uri)
	if err != nil {
		return uri, err
	}

	uri = filepath.FromSlash(uri)
	return uri, nil
	//uri = util.UriToRealPath(lsp.DocumentURI(uri))
	//return uri, nil
}

// ToURI returns a protocol URI for the supplied path.
// It will always have the file scheme.
func ToURI(path string) URI {
	const prefix = "$GOROOT"
	if strings.EqualFold(prefix, path[:len(prefix)]) {
		suffix := path[len(prefix):]
		//TODO: we need a better way to get the GOROOT that uses the packages api
		path = runtime.GOROOT() + suffix
	}
	uri := filepath.ToSlash(path)

	if uri[0] != '/' {
		uri = "/" + uri
	}

	return URI(fileSchemePrefix + uri)
	//uri := URI(util.PathToURI(path))
	//return uri
}

// FromDocumentURI create a URI from lsp.DocumentURI
func FromDocumentURI(uri lsp.DocumentURI) URI {
	s, _ := toFilename(string(uri))
	return ToURI(s)
}
