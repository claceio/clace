// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"strings"
	"sync"

	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/utils"
)

type AppPlugins struct {
	sync.Mutex
	plugins map[string]any

	app          *App
	pluginConfig map[string]utils.PluginSettings // pluginName -> accountName -> PluginSettings, from clace.toml
	accountMap   map[string]string               // pluginName -> accountName, from app account links
}

func NewAppPlugins(app *App, pluginConfig map[string]utils.PluginSettings, appAccounts []utils.AccountLink) *AppPlugins {
	accountMap := make(map[string]string)
	for _, entry := range appAccounts {
		accountMap[entry.Plugin] = entry.AccountName
	}

	return &AppPlugins{
		app:          app,
		plugins:      make(map[string]any),
		pluginConfig: pluginConfig,
		accountMap:   accountMap,
	}
}

func (p *AppPlugins) GetPlugin(pluginInfo *utils.PluginInfo, accountName string) (any, error) {
	p.Lock()
	defer p.Unlock()

	plugin, ok := p.plugins[pluginInfo.PluginPath]
	if ok {
		// Already initialized, use that
		return plugin, nil
	}

	// If account name is specified, use that to lookup the account map
	accountLookupName := pluginInfo.PluginPath
	if accountName != "" {
		accountLookupName = fmt.Sprintf("%s%s%s", pluginInfo.PluginPath, apptype.ACCOUNT_SEPERATOR, accountName) // store.in#myaccount
	}

	pluginAccount := pluginInfo.PluginPath
	_, ok = p.accountMap[accountLookupName]
	if ok {
		pluginAccount = p.accountMap[accountLookupName]
		// If it is just account name, make it full plugin path
		if !strings.Contains(pluginAccount, apptype.ACCOUNT_SEPERATOR) {
			pluginAccount = fmt.Sprintf("%s%s%s", pluginInfo.PluginPath, apptype.ACCOUNT_SEPERATOR, pluginAccount)
		}
	}

	appConfig := utils.PluginSettings{}
	if _, ok := p.pluginConfig[pluginAccount]; ok {
		appConfig = p.pluginConfig[pluginAccount]
	}

	pluginContext := &utils.PluginContext{
		Logger:    p.app.Logger,
		AppId:     p.app.AppEntry.Id,
		StoreInfo: p.app.storeInfo,
		Config:    appConfig,
	}
	plugin, err := pluginInfo.Builder(pluginContext)
	if err != nil {
		return nil, fmt.Errorf("error creating plugin %s: %w", pluginInfo.FuncName, err)
	}

	p.plugins[pluginInfo.PluginPath] = plugin
	return plugin, nil
}
