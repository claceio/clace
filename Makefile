# Copyright (c) ClaceIO, LLC
# SPDX-License-Identifier: Apache-2.0

SHELL := bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DELETE_ON_ERROR:
MAKEFLAGS += --warn-undefined-variables
MAKEFLAGS += --no-builtin-rules

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

unit: ## Run unit tests
> go test ./...

int: ## Run integration tests
> CL_HOME=. ./tests/run_cli_tests.sh

release: ## Tag and push a release
> @if [ -z "$(TAG)" ]; then \
>    echo "Error: TAG is not set"; \
>    exit 1; \
> fi
> git tag -a v$(TAG) -m "Release v$(TAG)"; git push origin v$(TAG)
