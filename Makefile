SHELL:=/bin/sh

.PHONY: devtools
devtools:
	@echo "\033[92m  ---> Installing golangci-lint (https://github.com/golangci/golangci-lint) ... \033[0m"
	curl -sfL "https://install.goreleaser.com/github.com/golangci/golangci-lint.sh" | sh -s -- -b $(shell go env GOPATH)/bin v1.12.5

.PHONY: lint
lint:
	@echo "\033[92m  ---> Linting ... \033[0m"
	golangci-lint run --config ./.golangci.yml ./...
