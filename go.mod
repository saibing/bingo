module github.com/saibing/bingo

require (
	github.com/fsnotify/fsnotify v1.4.7
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/gorilla/websocket v1.4.0 // indirect
	github.com/mattn/go-colorable v0.0.9 // indirect
	github.com/mattn/go-isatty v0.0.4 // indirect
	github.com/pmezard/go-difflib v1.0.0
	github.com/slimsag/godocmd v0.0.0-20161025000126-a1005ad29fe3
	github.com/sourcegraph/go-lsp v0.0.0-20181119182933-0c7d621186c1
	github.com/sourcegraph/jsonrpc2 v0.0.0-20180831160525-549eb959f029
	golang.org/x/sys v0.0.0-20190115152922-a457fd036447 // indirect
	golang.org/x/tools v0.0.0-20190114222345-bf090417da8b
	gopkg.in/inconshreveable/log15.v2 v2.0.0-20180818164646-67afb5ed74ec
)

replace golang.org/x/tools => github.com/saibing/tools v1.8.0
