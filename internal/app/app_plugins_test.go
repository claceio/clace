// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0
package app

import (
	"testing"

	"github.com/claceio/clace/internal/testutil"
	"github.com/claceio/clace/internal/utils"
)

func TestGetPlugin(t *testing.T) {
	// Plugin config info from config file
	pluginConfig := map[string]utils.PluginSettings{
		"plugin1.in":          {"key": "v1"},
		"plugin1.in#account1": {"key": "v2"},
		"plugin2.in":          {"key": "v3"},
		"plugin2.in#account1": {"key": "v4"},
		"plugin2.in#account2": {"key": "v5"},
		"plugin2.in#account3": {"key": "v6"},
	}

	// App account links
	appAccounts := []utils.AccountLink{
		{Plugin: "plugin2.in", AccountName: "account2"},
		{Plugin: "plugin2.in#account2", AccountName: "plugin2.in#account3"},
	}

	app := &App{
		Logger:   utils.NewLogger(&utils.LogConfig{}),
		AppEntry: &utils.AppEntry{Id: "testApp", Path: "/test", Domain: "", SourceUrl: ".", IsDev: false},
	}
	appPlugins := NewAppPlugins(app, pluginConfig, appAccounts)

	// Define the pluginInfo and accountName for testing
	pluginInfo := &utils.PluginInfo{
		ModuleName: "plugin1",
		PluginPath: "plugin1.in",
		FuncName:   "Plugin1Builder",
	}

	// Test with no account, no account link
	pluginInfo.Builder = func(pluginContext *utils.PluginContext) (any, error) {
		testutil.AssertEqualsString(t, "match key", "v1", pluginContext.Config["key"].(string))
		return nil, nil
	}
	plugin, err := appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.ModuleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.ModuleName], plugin)
	}

	// Test with no account, with account link
	pluginInfo.ModuleName = "plugin2"
	pluginInfo.PluginPath = "plugin2.in"
	pluginInfo.Builder = func(pluginContext *utils.PluginContext) (any, error) {
		testutil.AssertEqualsString(t, "match key", "v5", pluginContext.Config["key"].(string))
		return nil, nil
	}
	plugin, err = appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.ModuleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.ModuleName], plugin)
	}

	// Test with account, with no account link
	pluginInfo.PluginPath = "plugin2.in#account1"
	pluginInfo.Builder = func(pluginContext *utils.PluginContext) (any, error) {
		testutil.AssertEqualsString(t, "match key", "v4", pluginContext.Config["key"].(string))
		return nil, nil
	}
	plugin, err = appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.ModuleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.ModuleName], plugin)
	}

	// Test with account, with account link
	pluginInfo.PluginPath = "plugin2.in#account2"
	pluginInfo.Builder = func(pluginContext *utils.PluginContext) (any, error) {
		testutil.AssertEqualsString(t, "match key", "v6", pluginContext.Config["key"].(string))
		return nil, nil
	}
	plugin, err = appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.ModuleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.ModuleName], plugin)
	}

	// Test with invalid account
	pluginInfo.PluginPath = "plugin2.in#invalid"
	pluginInfo.Builder = func(pluginContext *utils.PluginContext) (any, error) {
		// Config should have no entries
		testutil.AssertEqualsInt(t, "match key", 0, len(pluginContext.Config))
		return nil, nil
	}
	plugin, err = appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.ModuleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.ModuleName], plugin)
	}
}
