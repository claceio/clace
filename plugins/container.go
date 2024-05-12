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
)

func init() {
	h := &containerPlugin{}
	pluginFuncs := []plugin.PluginFunc{
		app.CreatePluginApi(h.Config, app.READ),                                 // config API
		app.CreatePluginConstant("URL", starlark.String(apptype.CONTAINER_URL)), // container constant
	}
	app.RegisterPlugin("container", NewContainerPlugin, pluginFuncs)
}

type containerPlugin struct {
}

func NewContainerPlugin(pluginContext *types.PluginContext) (any, error) {
	return &containerPlugin{}, nil
}

func (h *containerPlugin) Config(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var lifetime, scheme, health starlark.String
	var port starlark.Int
	if err := starlark.UnpackArgs("config", args, kwargs, "port?", &port, "scheme?", &scheme, "health?", &health, "lifetime?", &lifetime); err != nil {
		return nil, err
	}
	portInt, ok := port.Int64()
	if !ok || portInt < 0 {
		return nil, fmt.Errorf("port must be an integer higher than or equal to zero")
	}

	return ContainerConfig{
		Lifetime: cmp.Or(string(lifetime), "app"),
		Port:     int(portInt),
		Schema:   cmp.Or(string(scheme), "http"),
		Health:   cmp.Or(string(health), "/"),
	}, nil
}

type ContainerConfig struct {
	Lifetime string
	Port     int
	Schema   string
	Health   string
}

func (p ContainerConfig) Attr(name string) (starlark.Value, error) {
	switch name {
	case "Lifetime":
		return starlark.String(p.Lifetime), nil
	case "Port":
		return starlark.MakeInt(p.Port), nil
	case "Scheme":
		return starlark.String(p.Schema), nil
	case "Health":
		return starlark.String(p.Health), nil
	default:
		return starlark.None, fmt.Errorf("container config has no attribute '%s'", name)
	}
}

func (p ContainerConfig) AttrNames() []string {
	return []string{"Lifetime", "Port", "Scheme", "Health"}
}

func (p ContainerConfig) String() string {
	return p.Lifetime
}

func (p ContainerConfig) Type() string {
	return "ContainerConfig"
}

func (p ContainerConfig) Freeze() {
}

func (p ContainerConfig) Truth() starlark.Bool {
	return p.Lifetime != ""
}

func (p ContainerConfig) Hash() (uint32, error) {
	return starlark.Tuple{starlark.String(p.Lifetime), starlark.MakeInt(p.Port), starlark.String(p.Schema), starlark.String(p.Health)}.Hash()
}

var _ starlark.Value = (*ContainerConfig)(nil)
