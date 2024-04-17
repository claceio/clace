// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"fmt"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/plugin"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
)

func init() {
	h := &proxyPlugin{}
	pluginFuncs := []plugin.PluginFunc{
		app.CreatePluginApi(h.Config, app.READ_WRITE),
	}
	app.RegisterPlugin("proxy", NewProxyPlugin, pluginFuncs)
}

type proxyPlugin struct {
}

func NewProxyPlugin(pluginContext *types.PluginContext) (any, error) {
	return &proxyPlugin{}, nil
}

func (h *proxyPlugin) Config(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var url starlark.String
	if err := starlark.UnpackArgs("config", args, kwargs, "url", &url); err != nil {
		return nil, err
	}

	return ProxyConfig{Url: string(url)}, nil
}

type ProxyConfig struct {
	Url string
}

func (p ProxyConfig) Attr(name string) (starlark.Value, error) {
	switch name {
	case "Url":
		return starlark.String(p.Url), nil
	default:
		return starlark.None, fmt.Errorf("proxy config has no attribute '%s'", name)
	}
}

func (p ProxyConfig) AttrNames() []string {
	return []string{"Url"}
}

func (p ProxyConfig) String() string {
	return p.Url
}

func (p ProxyConfig) Type() string {
	return "ProxyConfig"
}

func (p ProxyConfig) Freeze() {
}

func (p ProxyConfig) Truth() starlark.Bool {
	return p.Url != ""
}

func (p ProxyConfig) Hash() (uint32, error) {
	return starlark.Tuple{starlark.String(p.Url)}.Hash()
}

var _ starlark.Value = (*ProxyConfig)(nil)
