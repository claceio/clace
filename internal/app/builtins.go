// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	DEFAULT_MODULE        = "clace"
	APP                   = "app"
	PAGE                  = "page"
	FRAGMENT              = "fragment"
	REDIRECT              = "redirect"
	RENDER                = "render"
	PERMISSION            = "permission"
	DEFAULT_REDIRECT_CODE = 302
)

var (
	once    sync.Once
	builtin starlark.StringDict
)

func createAppBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var customLayout starlark.Bool
	var name starlark.String
	var pages *starlark.List
	var settings *starlark.Dict
	var permissions *starlark.List
	if err := starlark.UnpackArgs(APP, args, kwargs, "name", &name,
		"custom_layout?", &customLayout, "pages?", &pages, "settings?",
		&settings, "permissions?", &permissions); err != nil {
		return nil, err
	}

	if pages == nil {
		pages = starlark.NewList([]starlark.Value{})
	}
	if settings == nil {
		settings = starlark.NewDict(0)
	}

	if permissions == nil {
		permissions = starlark.NewList([]starlark.Value{})
	}

	fields := starlark.StringDict{
		"name":          name,
		"custom_layout": customLayout,
		"pages":         pages,
		"settings":      settings,
		"permissions":   permissions,
	}
	return starlarkstruct.FromStringDict(starlark.String(APP), fields), nil
}

func createPageBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, html, block starlark.String
	var handler starlark.Callable
	var fragments *starlark.List
	var method starlark.String
	if err := starlark.UnpackArgs(PAGE, args, kwargs, "path", &path, "html?", &html,
		"block?", &block, "handler?", &handler, "fragments?", &fragments, "method?", &method); err != nil {
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
		"block":     block,
		"fragments": fragments,
		"method":    method,
	}
	if handler != nil {
		fields["handler"] = handler
	}
	return starlarkstruct.FromStringDict(starlark.String(PAGE), fields), nil
}

func createFragmentBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, block starlark.String
	var handler starlark.Callable
	var method starlark.String
	if err := starlark.UnpackArgs(FRAGMENT, args, kwargs, "path", &path, "block", &block, "handler?", &handler, "method?", &method); err != nil {
		return nil, err
	}

	if method == "" {
		method = "GET"
	}

	fields := starlark.StringDict{
		"path":   path,
		"block":  block,
		"method": method,
	}
	if handler != nil {
		fields["handler"] = handler
	}
	return starlarkstruct.FromStringDict(starlark.String(FRAGMENT), fields), nil
}

func createRedirectBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var url starlark.String
	var code starlark.Int
	if err := starlark.UnpackArgs(REDIRECT, args, kwargs, "url", &url, "code?", &code); err != nil {
		return nil, err
	}

	if code == starlark.MakeInt(0) {
		code = starlark.MakeInt(DEFAULT_REDIRECT_CODE)
	}

	fields := starlark.StringDict{
		"url":  url,
		"code": code,
	}
	return starlarkstruct.FromStringDict(starlark.String(REDIRECT), fields), nil
}

func createPermissionBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var plugin, method starlark.String
	var arguments *starlark.List
	if err := starlark.UnpackArgs(PERMISSION, args, kwargs, "plugin", &plugin, "method", &method,
		"arguments?", &arguments); err != nil {
		return nil, err
	}

	if arguments == nil {
		arguments = starlark.NewList([]starlark.Value{})
	}

	fields := starlark.StringDict{
		"plugin":    plugin,
		"method":    method,
		"arguments": arguments,
	}
	return starlarkstruct.FromStringDict(starlark.String(PERMISSION), fields), nil
}

func CreateBuiltin() starlark.StringDict {
	once.Do(func() {
		builtin = starlark.StringDict{
			DEFAULT_MODULE: &starlarkstruct.Module{
				Name: DEFAULT_MODULE,
				Members: starlark.StringDict{
					APP:        starlark.NewBuiltin(APP, createAppBuiltin),
					PAGE:       starlark.NewBuiltin(PAGE, createPageBuiltin),
					FRAGMENT:   starlark.NewBuiltin(FRAGMENT, createFragmentBuiltin),
					REDIRECT:   starlark.NewBuiltin(REDIRECT, createRedirectBuiltin),
					PERMISSION: starlark.NewBuiltin(PERMISSION, createPermissionBuiltin),
				},
			},
		}
	})

	return builtin
}
