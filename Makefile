## Copyright (c) Clace Inc
## SPDX-License-Identifier: Apache-2.0

IMAGE_REPO ?= claceio/clace

vet: ## Run go vet
	go vet ./...

tidy: ## Run go mod tidy
	go mod tidy

buildwindows: ## Build clace CLI for windows/amd64
	GOOS=windows GOARCH=amd64 go install github.com/claceio/clace/cmd/clace 

build386: ## Build clace CLI for linux/386
	GOOS=linux GOARCH=386 go install github.com/claceio/clace/cmd/clace 

buildlinuxarm: ## Build clace CLI for linux/arm
	GOOS=linux GOARCH=arm go install github.com/claceio/clace/cmd/clace 

buildlinuxloong64: ## Build clace CLI for linux/loong64
	GOOS=linux GOARCH=loong64 go install github.com/claceio/clace/cmd/clace 

check: staticcheck vet buildwindows build386 buildlinuxarm ## Perform basic checks and compilation tests

staticcheck: ## Run staticcheck.io checks
	go run honnef.co/go/tools/cmd/staticcheck -- $$(go list ./...)

help: ## Show this help
	@echo "\nSpecify a command. The choices are:\n"
	@grep -hE '^[0-9a-zA-Z_-]+:.*?## .*$$' ${MAKEFILE_LIST} | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[0;36m%-20s\033[m %s\n", $$1, $$2}'
	@echo ""
.PHONY: help

.DEFAULT_GOAL := help
