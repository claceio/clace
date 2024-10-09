# Copyright (c) ClaceIO, LLC
# SPDX-License-Identifier: Apache-2.0

SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules
CL_HOME := `pwd`

.DEFAULT_GOAL := help
ifeq ($(origin .RECIPEPREFIX), undefined)
  $(error This Make does not support .RECIPEPREFIX. Please use GNU Make 4.0 or later)
endif
.RECIPEPREFIX = >
TAG := 

.PHONY: help test unit int release

help: ## Display this help section
> @awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-38s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: unit int ## Run all tests

covtest: covunit covint ## Run all tests with coverage
> go tool covdata percent -i=$(CL_HOME)/coverage/client,$(CL_HOME)/coverage/unit,$(CL_HOME)/coverage/int
> go tool covdata textfmt -i=$(CL_HOME)/coverage/client,$(CL_HOME)/coverage/unit,$(CL_HOME)/coverage/int -o $(CL_HOME)/coverage/profile
> go tool cover -func coverage/profile

unit: ## Run unit tests
> go test ./...

covunit: ## Run unit tests with coverage
> rm -rf $(CL_HOME)/coverage/unit && mkdir -p $(CL_HOME)/coverage/unit
> go test -cover ./... -args -test.gocoverdir="$(CL_HOME)/coverage/unit"
> go tool covdata percent -i=$(CL_HOME)/coverage/unit
> go tool covdata textfmt -i=$(CL_HOME)/coverage/unit -o $(CL_HOME)/coverage/profile
> go tool cover -func coverage/profile


int: ## Run integration tests
> CL_HOME=$(CL_HOME) ./tests/run_cli_tests.sh

covint: ## Run integration tests with coverage
> rm -rf $(CL_HOME)/coverage/int && mkdir -p $(CL_HOME)/coverage/int
> rm -rf $(CL_HOME)/coverage/client && mkdir -p $(CL_HOME)/coverage/client
> CL_HOME=. GOCOVERDIR=$(CL_HOME)/coverage/int ./tests/run_cli_tests.sh
> go tool covdata percent -i=$(CL_HOME)/coverage/client,$(CL_HOME)/coverage/int
> go tool covdata textfmt -i=$(CL_HOME)/coverage/client,$(CL_HOME)/coverage/int -o $(CL_HOME)/coverage/profile
> go tool cover -func coverage/profile

release: ## Tag and push a release
> @if [ -z "$(TAG)" ]; then \
>    echo "Error: TAG is not set"; \
>    exit 1; \
> fi
> git tag -a v$(TAG) -m "Release v$(TAG)"; git push origin v$(TAG)
