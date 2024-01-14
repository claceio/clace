// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"strings"
	"sync"

	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
)

type AppPlugins struct {
	sync.Mutex
	plugins      map[string]any
	pluginConfig map[string]utils.PluginSettings // pluginName -> accountName -> PluginSettings
	accountMap   map[string]string               // pluginName -> accountName
}

func NewAppPlugins(pluginConfig map[string]utils.PluginSettings, appAccounts []utils.AccountLink) *AppPlugins {
	accountMap := make(map[string]string)
	for _, entry := range appAccounts {
		accountMap[entry.Plugin] = entry.AccountName
	}

	return &AppPlugins{
		plugins:      make(map[string]any),
		pluginConfig: pluginConfig,
		accountMap:   accountMap,
	}
}

func (p *AppPlugins) GetPlugin(pluginInfo *PluginInfo, accountName string) (any, error) {
	p.Lock()
	defer p.Unlock()

	plugin, ok := p.plugins[pluginInfo.moduleName]
	if ok {
		// Already initialized, use that
		return plugin, nil
	}

	// If account name is specified, use that to lookup the account map
	accountLookupName := pluginInfo.pluginPath
	if accountName != "" {
		accountLookupName = fmt.Sprintf("%s%s%s", pluginInfo.pluginPath, util.ACCOUNT_SEPERATOR, accountName)
	}

	pluginAccount := pluginInfo.pluginPath
	_, ok = p.accountMap[accountLookupName]
	if ok {
		pluginAccount = p.accountMap[accountLookupName]
		// If it is just account name, make it full plugin path
		if !strings.Contains(pluginAccount, util.ACCOUNT_SEPERATOR) {
			pluginAccount = fmt.Sprintf("%s%s%s", pluginInfo.pluginPath, util.ACCOUNT_SEPERATOR, pluginAccount)
		}
	}

	appConfig := utils.PluginSettings{}
	if _, ok := p.pluginConfig[pluginAccount]; ok {
		appConfig = p.pluginConfig[pluginAccount]
	}

	pluginContext := &PluginContext{Config: appConfig}
	plugin, err := pluginInfo.builder(pluginContext)
	if err != nil {
		return nil, fmt.Errorf("error creating plugin %s: %w", pluginInfo.funcName, err)
	}

	p.plugins[pluginInfo.pluginPath] = plugin
	return plugin, nil
}
