// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"cmp"
	"fmt"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/plugin"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func init() {
	h := &containerPlugin{}
	pluginFuncs := []plugin.PluginFunc{
		app.CreatePluginApi(h.Config, app.READ), // config API
		app.CreatePluginConstant("URL", starlark.String(apptype.CONTAINER_URL)),
		app.CreatePluginConstant("AUTO", starlark.String(types.CONTAINER_SOURCE_AUTO)),
		app.CreatePluginConstant("NIXPACKS", starlark.String(types.CONTAINER_SOURCE_NIXPACKS)),
		app.CreatePluginConstant("IMAGE_PREFIX", starlark.String(types.CONTAINER_SOURCE_IMAGE_PREFIX)),
	}
	app.RegisterPlugin("container", NewContainerPlugin, pluginFuncs)
}

type containerPlugin struct {
}

func NewContainerPlugin(pluginContext *types.PluginContext) (any, error) {
	return &containerPlugin{}, nil
}

func (h *containerPlugin) Config(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var src, lifetime, scheme, health, buildDir starlark.String
	var port starlark.Int
	var volumes *starlark.List
	if err := starlark.UnpackArgs("config", args, kwargs, "src?", &src, "port?", &port, "scheme?", &scheme,
		"health?", &health, "lifetime?", &lifetime, "build_dir?", &buildDir, "volumes?", &volumes); err != nil {
		return nil, err
	}
	portInt, ok := port.Int64()
	if !ok || portInt < 0 {
		return nil, fmt.Errorf("port must be an integer higher than or equal to zero")
	}

	volumes = cmp.Or(volumes, starlark.NewList([]starlark.Value{}))

	fields := starlark.StringDict{
		"source":    starlark.String(cmp.Or(string(src), "auto")),
		"lifetime":  starlark.String(cmp.Or(string(lifetime), "app")),
		"port":      port,
		"scheme":    starlark.String(cmp.Or(string(scheme), "http")),
		"health":    starlark.String(cmp.Or(string(health), "/")),
		"build_dir": buildDir,
		"volumes":   volumes,
	}

	return starlarkstruct.FromStringDict(starlark.String("container_config"), fields), nil
}
