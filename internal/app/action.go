// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import "go.starlark.net/starlark"

type Action struct {
	name        string
	description string
	path        string
	run         starlark.Callable
	validate    starlark.Callable
}
