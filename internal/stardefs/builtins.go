// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package stardefs

import (
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	DEFAULT_LAYOUT = "default"
)

func CreateAppBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name, layout starlark.String
	var pages *starlark.List
	if err := starlark.UnpackArgs("App", args, kwargs, "name", &name, "layout?", &layout, "pages?", &pages); err != nil {
		return nil, err
	}

	if layout == "" {
		layout = DEFAULT_LAYOUT
	}
	if pages == nil {
		pages = starlark.NewList([]starlark.Value{})
	}

	fields := starlark.StringDict{
		"name":   name,
		"layout": layout,
		"pages":  pages,
	}
	return starlarkstruct.FromStringDict(starlark.String("App"), fields), nil
}

func CreatePageBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, html starlark.String
	var handler starlark.Callable
	var fragments *starlark.List
	var method starlark.String
	if err := starlark.UnpackArgs("Page", args, kwargs, "path", &path, "html", &html, "handler?", &handler, "fragments?", &fragments, "method?", &method); err != nil {
		return nil, err
	}

	if method == "" {
		method = "GET"
	}
	if fragments == nil {
		fragments = starlark.NewList([]starlark.Value{})
	}

	fields := starlark.StringDict{
		"path":      path,
		"html":      html,
		"fragments": fragments,
		"method":    method,
	}
	if handler != nil {
		fields["handler"] = handler
	}
	return starlarkstruct.FromStringDict(starlark.String("Page"), fields), nil
}

func CreateFragmentBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, block, html starlark.String
	var handler starlark.Callable
	var method starlark.String
	if err := starlark.UnpackArgs("Fragment", args, kwargs, "path", &path, "block", &block, "handler?", &handler, "html?", &html, "method?", &method); err != nil {
		return nil, err
	}

	fields := starlark.StringDict{
		"path":   path,
		"block":  block,
		"html":   html,
		"method": method,
	}
	if handler != nil {
		fields["handler"] = handler
	}
	if method == "" {
		method = "GET"
	}
	return starlarkstruct.FromStringDict(starlark.String("Fragment"), fields), nil
}
