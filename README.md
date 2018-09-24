# Bingo

Bingo is a [Go](https://golang.org) language server that speaks
[Language Server Protocol](https://github.com/Microsoft/language-server-protocol).

This project was largely inspired by [go-langserver](https://github.com/sourcegraph/go-langserver),
but Bingo more simpler, more faster, more smarter!

Bingo supports editor features as follow:

- [x] textDocument/hover
- [x] textDocument/definition
- [x] textDocument/xdefinition
- [x] textDocument/typeDefinition
- [x] textDocument/references
- [x] textDocument/implementation
- [ ] textDocument/formatting
- [ ] textDocument/documentSymbol
- [ ] textDocument/completion
- [ ] textDocument/signatureHelp
- [ ] workspace/symbol
- [ ] workspace/xreferences


Bingo only support go module, so you need install [Go 1.11 or above](https://golang.google.cn/dl/),
to build and install the standalone `bingo` run

```bash
git clone https://github.com/saibing/bingo.git
cd bingo
go build
```


