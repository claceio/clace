// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"sync"
)

type AppPlugins struct {
	sync.Mutex
	plugins map[string]any
}

func NewAppPlugins() *AppPlugins {
	return &AppPlugins{
		plugins: make(map[string]any),
	}
}

func (p *AppPlugins) GetPlugin(pluginInfo *PluginInfo) (any, error) {
	p.Lock()
	defer p.Unlock()

	plugin, ok := p.plugins[pluginInfo.moduleName]
	if ok {
		return plugin, nil
	}

	pluginContext := &PluginContext{}
	plugin, err := pluginInfo.builder(pluginContext)
	if err != nil {
		return nil, fmt.Errorf("error creating plugin %s: %w", pluginInfo.funcName, err)
	}

	p.plugins[pluginInfo.moduleName] = plugin
	return plugin, nil
}
