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
	appPlugins := NewAppPlugins(pluginConfig, appAccounts)

	// Define the pluginInfo and accountName for testing
	pluginInfo := &PluginInfo{
		moduleName: "plugin1",
		pluginPath: "plugin1.in",
		funcName:   "Plugin1Builder",
	}

	// Test with no account, no account link
	pluginInfo.builder = func(pluginContext *PluginContext) (any, error) {
		testutil.AssertEqualsString(t, "match key", "v1", pluginContext.Config["key"].(string))
		return nil, nil
	}
	plugin, err := appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.moduleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.moduleName], plugin)
	}

	// Test with no account, with account link
	pluginInfo.moduleName = "plugin2"
	pluginInfo.pluginPath = "plugin2.in"
	pluginInfo.builder = func(pluginContext *PluginContext) (any, error) {
		testutil.AssertEqualsString(t, "match key", "v5", pluginContext.Config["key"].(string))
		return nil, nil
	}
	plugin, err = appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.moduleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.moduleName], plugin)
	}

	// Test with account, with no account link
	pluginInfo.pluginPath = "plugin2.in#account1"
	pluginInfo.builder = func(pluginContext *PluginContext) (any, error) {
		testutil.AssertEqualsString(t, "match key", "v4", pluginContext.Config["key"].(string))
		return nil, nil
	}
	plugin, err = appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.moduleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.moduleName], plugin)
	}

	// Test with account, with account link
	pluginInfo.pluginPath = "plugin2.in#account2"
	pluginInfo.builder = func(pluginContext *PluginContext) (any, error) {
		testutil.AssertEqualsString(t, "match key", "v6", pluginContext.Config["key"].(string))
		return nil, nil
	}
	plugin, err = appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.moduleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.moduleName], plugin)
	}

	// Test with invalid account
	pluginInfo.pluginPath = "plugin2.in#invalid"
	pluginInfo.builder = func(pluginContext *PluginContext) (any, error) {
		// Config should have no entries
		testutil.AssertEqualsInt(t, "match key", 0, len(pluginContext.Config))
		return nil, nil
	}
	plugin, err = appPlugins.GetPlugin(pluginInfo, "")
	testutil.AssertNoError(t, err)
	if plugin != appPlugins.plugins[pluginInfo.moduleName] {
		t.Errorf("Expected %v, got %v", appPlugins.plugins[pluginInfo.moduleName], plugin)
	}
}
