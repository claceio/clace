// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package starlark_type

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

type Output struct {
	Value starlark.Value
	Err   string
}

func (o Output) Attr(name string) (starlark.Value, error) {
	switch name {
	case "value":
		if o.Err != "" {
			return starlark.None, fmt.Errorf("output has error: %s", o.Err)
		}
		return o.Value, nil
	case "error":
		return starlark.String(o.Err), nil
	default:
		return starlark.None, fmt.Errorf("output has no attribute '%s'", name)
	}
}

func (o Output) AttrNames() []string {
	return []string{"value", "error"}
}

func (o Output) String() string {
	return strings.ToLower(fmt.Sprintf("%v:%s", o.Value, o.Err))
}

func (o Output) Type() string {
	return "Output"
}

func (o Output) Freeze() {
}

func (o Output) Truth() starlark.Bool {
	return o.Err != ""
}

func (o Output) Hash() (uint32, error) {
	return starlark.Tuple{o.Value, starlark.String(o.Err)}.Hash()
}

var _ starlark.Value = (*Output)(nil)
