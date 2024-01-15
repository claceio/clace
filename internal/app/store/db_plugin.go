// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"net/http"

	"github.com/claceio/clace/internal/app"
	"go.starlark.net/starlark"
)

func init() {
	h := &dbPlugin{}
	pluginFuncs := []app.PluginFunc{
		app.CreatePluginApi(h.Create, false),
	}
	app.RegisterPlugin("db", NewDBPlugin, pluginFuncs)
}

type dbPlugin struct {
	client *http.Client
}

func NewDBPlugin(pluginContext *app.PluginContext) (any, error) {
	return &dbPlugin{}, nil
}

func (h *dbPlugin) Create(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return nil, nil
}
