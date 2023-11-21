// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/claceio/clace/internal/app/util"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

var (
	loaderInitMutex sync.Mutex
	builtInPlugins  map[string]PluginMap
)

func init() {
	builtInPlugins = make(map[string]PluginMap)
}

// RegisterPlugin registers a plugin with Clace
func RegisterPlugin(name string, funcs []PluginFunc) {
	loaderInitMutex.Lock()
	defer loaderInitMutex.Unlock()

	pluginName := fmt.Sprintf("%s.%s", name, util.BUILTIN_PLUGIN_SUFFIX)
	pluginMap := make(PluginMap)
	for _, f := range funcs {
		pluginMap[f.name] = f
	}

	builtInPlugins[pluginName] = pluginMap
}

// PluginMap is the plugin function mapping to PluginFuncs
type PluginMap map[string]PluginFunc

// PluginFunc is the Clace plugin function mapping to starlark function
type PluginFunc struct {
	name     string
	isRead   bool
	function *starlark.Builtin
}

// CreatePluginApi creates a Clace plugin function
func CreatePluginApi(name string, isRead bool,
	function func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error)) PluginFunc {
	return PluginFunc{
		name:     name,
		isRead:   isRead,
		function: starlark.NewBuiltin(name, function),
	}
}

// loader is the starlark loader function
func (a *App) loader(thread *starlark.Thread, module string) (starlark.StringDict, error) {
	if strings.HasSuffix(module, util.STARLARK_FILE_SUFFIX) {
		// Load the starlark file rather than the plugin
		return a.loadStarlark(thread, module, a.starlarkCache)
	}

	if a.Loads == nil || !slices.Contains(a.Loads, module) {
		return nil, fmt.Errorf("app %s is not permitted to load plugin %s. Audit the app and approve permissions", a.Path, module)
	}

	plugin, err := a.pluginLookup(thread, module)
	if err != nil {
		return nil, err
	}

	moduleName := strings.TrimSuffix(module, "."+util.BUILTIN_PLUGIN_SUFFIX)

	// Add calls to the hook function, which will do the permission checks at invocation time to
	// verify if the application has approval to call the specified function.
	// The audit loader will replace the builtins with dummy methods, so the hook is not added for the audit loader
	hookedDict := make(starlark.StringDict)
	for funcName, pluginFunc := range plugin {
		hookedDict[funcName] = a.pluginHook(module, funcName, pluginFunc)
	}

	ret := make(starlark.StringDict)
	ret[moduleName] = starlarkstruct.FromStringDict(starlarkstruct.Default, hookedDict)
	return ret, nil

}

// pluginLookup looks up the plugin. Audit checks need to be done by the caller
func (a *App) pluginLookup(_ *starlark.Thread, module string) (PluginMap, error) {
	pluginDict, ok := builtInPlugins[module]
	if !ok {
		return nil, fmt.Errorf("module %s not found", module) // TODO extend loading
	}

	return pluginDict, nil
}

func (a *App) pluginHook(module string, function string, pluginFunc PluginFunc) *starlark.Builtin {
	hook := func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		a.Trace().Msgf("Plugin called: %s.%s", module, function)

		if a.Permissions == nil {
			return nil, fmt.Errorf("app %s has no permissions configured, plugin call %s.%s is blocked. Audit the app and approve permissions", a.Path, module, function)
		}
		approved := false
		var lastError error
		for _, p := range a.Permissions {
			a.Trace().Msgf("Checking permission %s.%s call %s.%s", p.Plugin, p.Method, module, function)
			if p.Plugin == module && p.Method == function {
				if len(p.Arguments) > 0 {
					if len(p.Arguments) > len(args) {
						lastError = fmt.Errorf("app %s is not permitted to call %s.%s with %d arguments, %d or more positional arguments are required (permissions checks are not supported for kwargs). Audit the app and approve permissions", a.Path, module, function, len(args), len(p.Arguments))
						continue
					}
					argMismatch := false
					for i, arg := range p.Arguments {
						expect := fmt.Sprintf("%q", arg)
						if args[i].String() != fmt.Sprintf("%q", arg) {
							lastError = fmt.Errorf("app %s is not permitted to call %s.%s with argument %d having value %s, expected %s. Update the app or audit and approve permissions", a.Path, module, function, i, args[i].String(), expect)
							argMismatch = true
							break
						}
						// More arguments than approved are permitted. Also, using kwargs is not allowed for args which are approved
						// Regex support is not implemented, the arguments have to match exactly as approved
					}
					if argMismatch {
						// This permission is not approved, but there may be others which are
						continue
					}
				}
				approved = true
				break
			}
		}

		if !approved {
			if lastError != nil {
				return nil, lastError
			} else {
				return nil, fmt.Errorf("app %s is not permitted to call %s.%s. Audit the app and approve permissions", a.Path, module, function)
			}
		}

		val, err := pluginFunc.function.CallInternal(thread, args, kwargs)
		return val, err
	}

	return starlark.NewBuiltin(function, hook)
}
