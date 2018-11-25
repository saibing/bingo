# bingo

bingo is a [Go](https://golang.org) language server that speaks
[Language Server Protocol](https://github.com/Microsoft/language-server-protocol).

This project was largely inspired by [go-langserver](https://github.com/sourcegraph/go-langserver),
but bingo more simpler, more faster, more smarter!

bingo will support editor features as follow:

- [x] textDocument/hover
- [x] textDocument/definition
- [x] textDocument/xdefinition
- [x] textDocument/typeDefinition
- [x] textDocument/references
- [x] textDocument/implementation
- [x] textDocument/formatting
- [x] textDocument/rangeFormatting
- [x] textDocument/documentSymbol
- [x] textDocument/completion
- [x] textDocument/signatureHelp
- [x] textDocument/publishDiagnostics
- [ ] textDocument/rename
- [ ] textDocument/codeAction
- [ ] textDocument/codeLens
- [x] workspace/symbol
- [x] workspace/xreferences


bingo only support go module project, so you need install [Go 1.11 or above](https://golang.google.cn/dl/),
to build and install the `bingo` run

```bash
git clone https://github.com/saibing/bingo.git
cd bingo
go build
```

Differences between go-langserver, bingo, golsp.

- [go-langserver](https://github.com/sourcegraph/go-langserver)

go-langserver is designed for online code reading such as github.com.

- [bingo](https://github.com/saibing/bingo)

bingo is designed for offline editors such as vscode, vim, it focuses on code editing.

- [golsp](https://github.com/golang/tools/blob/master/cmd/golsp/main.go)

golsp is an official language server,  and it is currently in early development.
