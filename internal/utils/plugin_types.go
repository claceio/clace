// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package utils

type NewPluginFunc func(pluginContext *PluginContext) (any, error)

// PluginMap is the plugin function mapping to PluginFuncs
type PluginMap map[string]*PluginInfo

// PluginFunc is the Clace plugin function mapping to starlark function
type PluginFunc struct {
	Name         string
	IsRead       bool
	FunctionName string
}

// PluginFuncInfo is the Clace plugin function info for the starlark function
type PluginInfo struct {
	ModuleName  string // exec
	PluginPath  string // exec.in
	FuncName    string // run
	IsRead      bool
	HandlerName string
	Builder     NewPluginFunc
}

type PluginContext struct {
	Logger    *Logger
	AppId     AppId
	StoreInfo *StoreInfo
	Config    PluginSettings
}
