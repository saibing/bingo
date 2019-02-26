# bingo

[![Go Report Card](https://goreportcard.com/badge/github.com/saibing/bingo)](https://goreportcard.com/report/github.com/saibing/bingo)

bingo is a [Go](https://golang.org) language server that speaks
[Language Server Protocol](https://github.com/Microsoft/language-server-protocol).

This project was largely inspired by [go-langserver](https://github.com/sourcegraph/go-langserver).

## Supported Features

### Feature

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
- [x] textDocument/rename
- [ ] textDocument/codeAction
- [ ] textDocument/codeLens
- [x] workspace/symbol
- [x] workspace/xreferences

## Install

### Install

bingo is a go module project, so you need install [Go 1.11 or above](https://golang.google.cn/dl/),
to  install the `bingo`, please run

```bash
git clone https://github.com/saibing/bingo.git
cd bingo
GO111MODULE=on go install
```

If you live in China and may not be able to download golang.org/x/ dependency module, please set GOPROXY as follow:

```bash
 export GOPROXY=https://athens.azurefd.net/
```

## Configuration

### bingo's flag

#### --trace

print all requests and responses

#### --logfile &lt;path&gt;

log both stdout and stderr to a file

#### --format-style &lt;style&gt;

which format style is used to format documents. Supported: gofmt and goimports

#### --diagnostics-style &lt;style&gt;

which diagnostics style is used to diagnostics current document. Supported: none, instant, onsave.

####  --cache-style &lt;style&gt;

set global cache style: none, on-demand, always.

## Language Client

### [vscode-go](https://github.com/Microsoft/vscode-go)

```json
{
    "go.useLanguageServer": true,
    "go.alternateTools": {
        "go-langserver": "bingo"
    },
    "go.languageServerFlags": [],
    "go.languageServerExperimentalFeatures": {
        "format": true,
        "autoComplete": true
    }
}
```

### [coc.nvim](https://github.com/neoclide/coc.nvim)

Please reference [Language server](https://github.com/neoclide/coc.nvim/wiki/Language-servers#go)

### [LanguageClient-neovim](https://github.com/autozimu/LanguageClient-neovim)

```vim
let g:LanguageClient_rootMarkers = {
        \ 'go': ['.git', 'go.mod'],
        \ }

let g:LanguageClient_serverCommands = {
    \ 'go': ['bingo'],
    \ }

```

## F.A.Q

### Differences between go-langserver, bingo, golsp

- [go-langserver](https://github.com/sourcegraph/go-langserver)

> go-langserver is designed for online code reading such as github.com.

- [bingo](https://github.com/saibing/bingo)

> bingo is designed for offline editors such as vscode, vim, it focuses on code editing.

- [gopls](https://github.com/golang/tools/blob/master/cmd/gopls/main.go)

> gopls is an official language server,  and it is currently in early development.
