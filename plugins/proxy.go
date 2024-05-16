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
		app.CreatePluginApi(h.Config, app.READ), // config API, preview/stage permission checks happen in the reverse proxy wrapper
	}
	app.RegisterPlugin("proxy", NewProxyPlugin, pluginFuncs)
}

type proxyPlugin struct {
}

func NewProxyPlugin(pluginContext *types.PluginContext) (any, error) {
	return &proxyPlugin{}, nil
}

func (h *proxyPlugin) Config(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var url, stripPath starlark.String
	var preserveHost starlark.Bool
	if err := starlark.UnpackArgs("config", args, kwargs, "url", &url, "strip_path?", &stripPath, "preserve_host?", &preserveHost); err != nil {
		return nil, err
	}

	return ProxyConfig{
		Url:          string(url),
		StripPath:    string(stripPath),
		PreserveHost: bool(preserveHost),
	}, nil
}

type ProxyConfig struct {
	Url          string
	StripPath    string
	PreserveHost bool
}

func (p ProxyConfig) Attr(name string) (starlark.Value, error) {
	switch name {
	case "Url":
		return starlark.String(p.Url), nil
	case "StripPath":
		return starlark.String(p.StripPath), nil
	case "PreserveHost":
		return starlark.Bool(p.PreserveHost), nil
	default:
		return starlark.None, fmt.Errorf("proxy config has no attribute '%s'", name)
	}
}

func (p ProxyConfig) AttrNames() []string {
	return []string{"Url", "StripPath", "PreserveHost"}
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
	return starlark.Tuple{starlark.String(p.Url), starlark.String(p.StripPath), starlark.Bool(p.PreserveHost)}.Hash()
}

var _ starlark.Value = (*ProxyConfig)(nil)
