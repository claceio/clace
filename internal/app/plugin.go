// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"slices"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

var (
	loaderInitMutex sync.Mutex
	builtInPlugins  map[string]starlark.StringDict
)

func init() {
	builtInPlugins = make(map[string]starlark.StringDict)
}

func RegisterPlugin(name string, plugin *starlarkstruct.Struct) {
	loaderInitMutex.Lock()
	defer loaderInitMutex.Unlock()

	pluginName := fmt.Sprintf("%s.%s", name, BUILTIN_PLUGIN_SUFFIX)
	pluginDict := make(starlark.StringDict)
	pluginDict[name] = plugin
	builtInPlugins[pluginName] = pluginDict
}

// loader is the starlark loader function
func (a *App) loader(t *starlark.Thread, module string) (starlark.StringDict, error) {
	if a.Loads == nil || !slices.Contains(a.Loads, module) {
		return nil, fmt.Errorf("app %s is not permitted to load plugin %s. Audit the app and approve permissions", a.Path, module)
	}

	staticDict, err := a.loaderImpl(t, module)
	if err != nil {
		return nil, err
	}

	// Add calls to the hook function, which will do the permission checks at invocation time to
	// verify if the application has approval to call the specified function.
	// The audit loader will replace the builtins with dummy methods, so the hook is not added for the audit loader
	hookedDict := make(starlark.StringDict)
	for k, v := range staticDict {
		newDict := make(starlark.StringDict)
		if s, ok := v.(*starlarkstruct.Struct); ok {
			for _, attrName := range s.AttrNames() {
				attrVal, err := s.Attr(attrName)
				if err != nil {
					return nil, fmt.Errorf("error getting builtin for %s.%s.%s", module, k, attrName)
				}
				origBuiltin, ok := attrVal.(*starlark.Builtin)
				if !ok {
					return nil, fmt.Errorf("error casting as builtin for %s.%s.%s %v", module, k, attrName, v)
				}
				newDict[attrName] = a.pluginHook(module, attrName, origBuiltin)
			}
		}
		hookedDict[k] = starlarkstruct.FromStringDict(starlarkstruct.Default, newDict)
	}

	return hookedDict, nil
}

// loaderImpl is the starlark loader function, with no audit checks
func (a *App) loaderImpl(_ *starlark.Thread, module string) (starlark.StringDict, error) {
	pluginDict, ok := builtInPlugins[module]
	if !ok {
		return nil, fmt.Errorf("module %s not found", module) // TODO extend loading
	}

	return pluginDict, nil
}

func (a *App) pluginHook(plugin string, function string, builtin *starlark.Builtin) *starlark.Builtin {
	hook := func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		a.Trace().Msgf("Plugin called: %s.%s", plugin, function)

		if a.Permissions == nil {
			return nil, fmt.Errorf("app %s has no permissions configured, plugin call %s.%s is blocked. Audit the app and approve permissions", a.Path, plugin, function)
		}
		approved := false
		for _, p := range a.Permissions {
			a.Trace().Msgf("Checking permission %s.%s call %s.%s", p.Plugin, p.Method, plugin, function)
			if p.Plugin == plugin && p.Method == function {
				if len(p.Arguments) > 0 {
					if len(p.Arguments) > len(args) {
						return nil, fmt.Errorf("app %s is not permitted to call %s.%s with %d arguments, %d or more positional arguments are required (permissions checks are not supported for kwargs). Audit the app and approve permissions", a.Path, plugin, function, len(args), len(p.Arguments))
					}
					for i, arg := range p.Arguments {
						expect := fmt.Sprintf("%q", arg)
						if args[i].String() != fmt.Sprintf("%q", arg) {
							return nil, fmt.Errorf("app %s is not permitted to call %s.%s with argument %d having value %s, expected %s. Update the app or audit and approve permissions", a.Path, plugin, function, i, args[i].String(), expect)
						}
						// More arguments than approved are permitted. Also, using kwargs is not allowed for args which are approved
						// Regex support is not implemented, the arguments have to match exactly as approved
					}
				}
				approved = true
				break
			}
		}

		if !approved {
			return nil, fmt.Errorf("app %s is not permitted to call %s.%s. Audit the app and approve permissions", a.Path, plugin, function)
		}

		val, err := builtin.CallInternal(thread, args, kwargs)
		return val, err
	}

	return starlark.NewBuiltin(function, hook)
}
