// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package apptype

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	DEFAULT_MODULE        = "ace"
	DOC_MODULE            = "doc"
	TABLE_MODULE          = "table"
	PARAM_MODULE          = "param"
	APP                   = "app"
	HTML                  = "html"
	API                   = "api"
	PROXY                 = "proxy"
	FRAGMENT              = "fragment"
	STYLE                 = "style"
	REDIRECT              = "redirect"
	PERMISSION            = "permission"
	RESPONSE              = "response"
	LIBRARY               = "library"
	ACTION                = "action"
	CONTAINER_URL         = "<CONTAINER_URL>" // special url to use for proxying to the container
	DEFAULT_REDIRECT_CODE = 303
)

const (
	// Constants included in the ace builtin module
	GET       = "GET"
	POST      = "POST"
	PUT       = "PUT"
	DELETE    = "DELETE"
	HTML_TYPE = "HTML"
	JSON      = "JSON"
	TEXT      = "TEXT"
	READ      = "READ"
	WRITE     = "WRITE"
)

var (
	once    sync.Once
	builtin starlark.StringDict
)

func createAppBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var customLayout starlark.Bool
	var name starlark.String
	var routes, actions *starlark.List
	var settings *starlark.Dict
	var permissions, libraries *starlark.List
	var style *starlarkstruct.Struct
	var containerConfig starlark.Value
	if err := starlark.UnpackArgs(APP, args, kwargs, "name", &name,
		"routes?", &routes, "style?", &style, "permissions?", &permissions, "libraries?", &libraries, "settings?",
		&settings, "custom_layout?", &customLayout, "container?", &containerConfig, "actions?", &actions); err != nil {
		return nil, fmt.Errorf("error unpacking app args: %w", err)
	}

	if routes == nil {
		routes = starlark.NewList([]starlark.Value{})
	}
	if actions == nil {
		actions = starlark.NewList([]starlark.Value{})
	}
	if libraries == nil {
		libraries = starlark.NewList([]starlark.Value{})
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
		"routes":        routes,
		"settings":      settings,
		"permissions":   permissions,
		"libraries":     libraries,
		"actions":       actions,
	}

	if style != nil {
		fields["style"] = style
	}

	if containerConfig != nil {
		fields["container"] = containerConfig
	}

	return starlarkstruct.FromStringDict(starlark.String(APP), fields), nil
}

func createHtmlBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, html, block starlark.String
	var handler starlark.Callable
	var fragments *starlark.List
	var method starlark.String
	if err := starlark.UnpackArgs(HTML, args, kwargs, "path", &path, "full?", &html,
		"partial?", &block, "handler?", &handler, "fragments?", &fragments, "method?", &method); err != nil {
		return nil, fmt.Errorf("error unpacking html args: %w", err)
	}

	if method == "" {
		method = "GET"
	}
	if fragments == nil {
		fragments = starlark.NewList([]starlark.Value{})
	}

	fields := starlark.StringDict{
		"path":      path,
		"full":      html,
		"partial":   block,
		"fragments": fragments,
		"method":    method,
	}
	if handler != nil {
		fields["handler"] = handler
	}
	return starlarkstruct.FromStringDict(starlark.String(HTML), fields), nil
}

func createFragmentBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, block starlark.String
	var handler starlark.Callable
	var method starlark.String
	if err := starlark.UnpackArgs(FRAGMENT, args, kwargs, "path", &path, "partial?", &block,
		"handler?", &handler, "method?", &method); err != nil {
		return nil, fmt.Errorf("error unpacking fragment args: %w", err)
	}

	if method == "" {
		method = "GET"
	}

	fields := starlark.StringDict{
		"path":    path,
		"partial": block,
		"method":  method,
	}
	if handler != nil {
		fields["handler"] = handler
	}
	return starlarkstruct.FromStringDict(starlark.String(FRAGMENT), fields), nil
}

func createStyleBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var library starlark.String
	var themes *starlark.List
	var disableWatcher starlark.Bool
	if err := starlark.UnpackArgs(FRAGMENT, args, kwargs, "library", &library, "themes?", &themes, "disable_watcher?", &disableWatcher); err != nil {
		return nil, fmt.Errorf("error unpacking style args: %w", err)
	}

	if themes == nil {
		themes = starlark.NewList([]starlark.Value{})
	}

	fields := starlark.StringDict{
		"library":         library,
		"themes":          themes,
		"disable_watcher": disableWatcher,
	}
	return starlarkstruct.FromStringDict(starlark.String(STYLE), fields), nil
}

func createRedirectBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var url starlark.String
	var code starlark.Int
	var refresh starlark.Bool
	if err := starlark.UnpackArgs(REDIRECT, args, kwargs, "url", &url, "code?", &code, "refresh?", &refresh); err != nil {
		return nil, fmt.Errorf("error unpacking redirect args: %w", err)
	}

	codeValue, _ := code.Int64()
	if codeValue == 0 {
		code = starlark.MakeInt(DEFAULT_REDIRECT_CODE)
	}

	fields := starlark.StringDict{
		"url":     url,
		"code":    code,
		"refresh": refresh,
	}
	return starlarkstruct.FromStringDict(starlark.String(REDIRECT), fields), nil
}

func createResponseBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var block, retarget, reswap, redirect, rtype starlark.String
	var data starlark.Value
	var code starlark.Int
	if err := starlark.UnpackArgs(RESPONSE, args, kwargs, "data", &data, "block?", &block, "type?", &rtype, "code?", &code, "retarget?", &retarget, "reswap?", &reswap, "redirect?", &redirect); err != nil {
		return nil, fmt.Errorf("error unpacking response args: %w", err)
	}

	codeValue, _ := code.Int64()
	if codeValue == 0 {
		code = starlark.MakeInt(http.StatusOK)
	}

	rtypeStr := strings.ToUpper(rtype.GoString())
	if rtypeStr != "" && rtypeStr != HTML_TYPE && rtypeStr != JSON && rtypeStr != TEXT {
		return nil, fmt.Errorf("invalid type specified : %s", rtypeStr)
	}

	fields := starlark.StringDict{
		"data":     data,
		"block":    block,
		"type":     rtype,
		"code":     code,
		"retarget": retarget,
		"reswap":   reswap,
		"redirect": redirect,
	}
	return starlarkstruct.FromStringDict(starlark.String(RESPONSE), fields), nil
}

func createPermissionBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var plugin, method starlark.String
	var arguments *starlark.List
	var rtype starlark.String
	if err := starlark.UnpackArgs(PERMISSION, args, kwargs, "plugin", &plugin, "method", &method,
		"arguments?", &arguments, "type?", &rtype); err != nil {
		return nil, fmt.Errorf("error unpacking permission args: %w", err)
	}

	if arguments == nil {
		arguments = starlark.NewList([]starlark.Value{})
	}

	fields := starlark.StringDict{
		"plugin":    plugin,
		"method":    method,
		"arguments": arguments,
	}

	if rtype == "READ" {
		fields["is_read"] = starlark.True
	} else if rtype == "WRITE" {
		fields["is_read"] = starlark.False
	} else if rtype != "" {
		return nil, fmt.Errorf("invalid permission type specified : %s", rtype)
	}

	return starlarkstruct.FromStringDict(starlark.String(PERMISSION), fields), nil
}

func createLibraryBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name, version starlark.String
	var esbuildArgs *starlark.List
	if err := starlark.UnpackArgs(LIBRARY, args, kwargs, "name", &name, "version", &version,
		"args?", &esbuildArgs); err != nil {
		return nil, fmt.Errorf("error unpacking library args: %w", err)
	}

	if esbuildArgs == nil {
		esbuildArgs = starlark.NewList([]starlark.Value{})
	}

	fields := starlark.StringDict{
		"name":    name,
		"version": version,
		"args":    esbuildArgs,
	}
	return starlarkstruct.FromStringDict(starlark.String(LIBRARY), fields), nil
}

func createActionBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name, desc, path starlark.String
	var validator, executor starlark.Callable
	if err := starlark.UnpackArgs(LIBRARY, args, kwargs, "name", &name, "path", &path,
		"run", &executor, "validate?", &validator, "description?", &desc); err != nil {
		return nil, fmt.Errorf("error unpacking action args: %w", err)
	}

	fields := starlark.StringDict{
		"name":        name,
		"description": desc,
		"path":        path,
		"run":         executor,
	}

	if validator != nil {
		fields["validate"] = validator
	}
	return starlarkstruct.FromStringDict(starlark.String(ACTION), fields), nil
}

func createProxyBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path starlark.String
	var config starlark.Value
	if err := starlark.UnpackArgs(PROXY, args, kwargs, "path", &path, "config", &config); err != nil {
		return nil, fmt.Errorf("error unpacking proxy args: %w", err)
	}

	fields := starlark.StringDict{
		"path":   path,
		"config": config,
	}
	return starlarkstruct.FromStringDict(starlark.String(PROXY), fields), nil
}

func createAPIBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, rtype starlark.String
	var handler starlark.Callable
	var method starlark.String
	if err := starlark.UnpackArgs(API, args, kwargs, "path", &path, "handler?", &handler, "method?", &method, "type?", &rtype); err != nil {
		return nil, fmt.Errorf("error unpacking api args: %w", err)
	}

	if method == "" {
		method = "GET"
	}

	rtypeStr := strings.ToUpper(rtype.GoString())
	if rtypeStr == "" {
		rtypeStr = JSON
	}
	if rtypeStr != JSON && rtypeStr != TEXT {
		return nil, fmt.Errorf("invalid API type specified : %s", rtypeStr)
	}

	fields := starlark.StringDict{
		"path":   path,
		"method": method,
		"type":   starlark.String(rtypeStr),
	}
	if handler != nil {
		fields["handler"] = handler
	}
	return starlarkstruct.FromStringDict(starlark.String(API), fields), nil
}

func CreateBuiltin() starlark.StringDict {
	once.Do(func() {
		builtin = starlark.StringDict{
			DEFAULT_MODULE: &starlarkstruct.Module{
				Name: DEFAULT_MODULE,
				Members: starlark.StringDict{
					APP:        starlark.NewBuiltin(APP, createAppBuiltin),
					HTML:       starlark.NewBuiltin(HTML, createHtmlBuiltin),
					PROXY:      starlark.NewBuiltin(PROXY, createProxyBuiltin),
					API:        starlark.NewBuiltin(API, createAPIBuiltin),
					FRAGMENT:   starlark.NewBuiltin(FRAGMENT, createFragmentBuiltin),
					REDIRECT:   starlark.NewBuiltin(REDIRECT, createRedirectBuiltin),
					PERMISSION: starlark.NewBuiltin(PERMISSION, createPermissionBuiltin),
					STYLE:      starlark.NewBuiltin(STYLE, createStyleBuiltin),
					RESPONSE:   starlark.NewBuiltin(RESPONSE, createResponseBuiltin),
					LIBRARY:    starlark.NewBuiltin(LIBRARY, createLibraryBuiltin),
					ACTION:     starlark.NewBuiltin(ACTION, createActionBuiltin),

					GET:    starlark.String(GET),
					POST:   starlark.String(POST),
					PUT:    starlark.String(PUT),
					DELETE: starlark.String(DELETE),
					JSON:   starlark.String(JSON),
					TEXT:   starlark.String(TEXT),
					READ:   starlark.String(READ),
					WRITE:  starlark.String(WRITE),
				},
			},
		}
	})

	return builtin
}
