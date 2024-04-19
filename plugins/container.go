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
	h := &containerPlugin{}
	pluginFuncs := []plugin.PluginFunc{
		app.CreatePluginApi(h.Config, app.READ), // config API
	}
	app.RegisterPlugin("container", NewContainerPlugin, pluginFuncs)
}

type containerPlugin struct {
}

func NewContainerPlugin(pluginContext *types.PluginContext) (any, error) {
	return &containerPlugin{}, nil
}

func (h *containerPlugin) Config(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var lifetime starlark.String
	var port starlark.Int
	if err := starlark.UnpackArgs("config", args, kwargs, "lifetime", &lifetime, "port?", &port); err != nil {
		return nil, err
	}
	portInt, ok := port.Int64()
	if !ok {
		return nil, fmt.Errorf("port must be an integer")
	}

	return ContainerConfig{LifeTime: string(lifetime), Port: int(portInt)}, nil
}

type ContainerConfig struct {
	LifeTime string
	Port     int
}

func (p ContainerConfig) Attr(name string) (starlark.Value, error) {
	switch name {
	case "LifeTime":
		return starlark.String(p.LifeTime), nil
	case "Port":
		return starlark.MakeInt(p.Port), nil
	default:
		return starlark.None, fmt.Errorf("container config has no attribute '%s'", name)
	}
}

func (p ContainerConfig) AttrNames() []string {
	return []string{"LifeTime", "Port"}
}

func (p ContainerConfig) String() string {
	return p.LifeTime
}

func (p ContainerConfig) Type() string {
	return "ContainerConfig"
}

func (p ContainerConfig) Freeze() {
}

func (p ContainerConfig) Truth() starlark.Bool {
	return p.LifeTime != ""
}

func (p ContainerConfig) Hash() (uint32, error) {
	return starlark.Tuple{starlark.String(p.LifeTime), starlark.MakeInt(p.Port)}.Hash()
}

var _ starlark.Value = (*ContainerConfig)(nil)
