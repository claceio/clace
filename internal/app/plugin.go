// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

type PluginFunctionType int

const (
	READ PluginFunctionType = iota
	WRITE
	READ_WRITE
)

const (
	TL_CONTEXT                  = "TL_context"
	TL_CURRENT_MODULE_FULL_PATH = "TL_current_module_full_path"
)

var (
	loaderInitMutex sync.Mutex
	builtInPlugins  map[string]utils.PluginMap
)

func init() {
	builtInPlugins = make(map[string]utils.PluginMap)
}

// RegisterPlugin registers a plugin with Clace
func RegisterPlugin(name string, builder utils.NewPluginFunc, funcs []utils.PluginFunc) {
	loaderInitMutex.Lock()
	defer loaderInitMutex.Unlock()

	pluginPath := fmt.Sprintf("%s.%s", name, util.BUILTIN_PLUGIN_SUFFIX)
	pluginMap := make(utils.PluginMap)
	for _, f := range funcs {
		info := utils.PluginInfo{
			ModuleName:  name,
			PluginPath:  pluginPath,
			FuncName:    f.Name,
			IsRead:      f.IsRead,
			HandlerName: f.FunctionName,
			Builder:     builder,
		}

		pluginMap[f.Name] = &info
	}

	builtInPlugins[pluginPath] = pluginMap
}

type StarlarkFunction func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error)

// pluginErrorWrapper wraps the plugin function call with error handling code. If the plugin function returns an error,
// it is wrapped in a PluginResponse. If the starlark function returns a PluginResponse, it is returned as is. Returning
// a error causes the starlark interpreter to panic, so this wrapper is needed to handle the error and return a value which
// the starlark code can handle
func pluginErrorWrapper(f StarlarkFunction) StarlarkFunction {
	return func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		// Wrap the plugin function call with error handling
		val, err := f(thread, fn, args, kwargs)

		// If the return value is already of type PluginResponse, return it without wrapping it
		_, ok := val.(*utils.PluginResponse)
		if ok {
			return val, err
		}

		if err != nil {
			// Error response wrapped in a PluginResponse
			return utils.NewErrorResponse(err), nil
		}

		// Success response, wrapped in a PluginResponse
		return utils.NewResponse(val), nil
	}
}

func CreatePluginApi(f StarlarkFunction, opType PluginFunctionType) utils.PluginFunc {
	funcVal := runtime.FuncForPC(reflect.ValueOf(f).Pointer())
	if funcVal == nil {
		panic(fmt.Errorf("function not found during plugin register"))
	}

	parts := strings.Split(funcVal.Name(), "/")
	nameParts := strings.Split(parts[len(parts)-1], ".")
	funcName := strings.TrimSuffix(nameParts[len(nameParts)-1], "-fm") // -fm denotes function value

	return CreatePluginApiName(f, opType, strings.ToLower(funcName))
}

// CreatePluginApiName creates a Clace plugin function
func CreatePluginApiName(
	f func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error),
	opType PluginFunctionType,
	name string) utils.PluginFunc {
	funcVal := runtime.FuncForPC(reflect.ValueOf(f).Pointer())
	if funcVal == nil {
		panic(fmt.Errorf("function %s not found during plugin register", name))
	}

	parts := strings.Split(funcVal.Name(), "/")
	nameParts := strings.Split(parts[len(parts)-1], ".")
	funcName := strings.TrimSuffix(nameParts[len(nameParts)-1], "-fm") // -fm denotes function value

	if len(funcName) == 0 {
		panic(fmt.Errorf("function %s not found during plugin register", name))
	}
	rune, _ := utf8.DecodeRuneInString(funcName)
	if !unicode.IsUpper(rune) {
		panic(fmt.Errorf("function %s is not an exported method during plugin register", funcName))
	}

	return utils.PluginFunc{
		Name:         name,
		IsRead:       opType == READ,
		FunctionName: funcName,
	}
}

func GetContext(thread *starlark.Thread) context.Context {
	c := thread.Local(TL_CONTEXT)
	if c == nil {
		return nil
	}
	return c.(context.Context)
}

// SavePluginState saves a value in the thread local for the plugin
func SavePluginState(thread *starlark.Thread, key string, value any) {
	pluginName := thread.Local(TL_CURRENT_MODULE_FULL_PATH)
	if pluginName == nil {
		panic(fmt.Errorf("plugin name not found in thread local"))
	}

	keyName := fmt.Sprintf("%s_%s", pluginName, key)
	thread.SetLocal(keyName, value)
}

// FetchPluginState fetches a value from the thread local for the plugin
func FetchPluginState(thread *starlark.Thread, key string) any {
	pluginName := thread.Local(TL_CURRENT_MODULE_FULL_PATH)
	if pluginName == nil {
		panic(fmt.Errorf("plugin name not found in thread local"))
	}

	keyName := fmt.Sprintf("%s_%s", pluginName, key)
	return thread.Local(keyName)
}

type DeferFunc func() error
type DeferEntry struct {
	Func   DeferFunc
	Strict bool
}

// DeferPluginClose defers a close function to call when the API handler is done
func DeferPluginClose(thread *starlark.Thread, deferFunc DeferFunc, strict bool) {
	pluginName := thread.Local(TL_CURRENT_MODULE_FULL_PATH)
	if pluginName == nil {
		panic(fmt.Errorf("plugin name not found in thread local"))
	}

	deferKey := fmt.Sprintf("defer_%s", pluginName)
	deferList := thread.Local(deferKey)
	if deferList == nil {
		deferList = make([]DeferEntry, 0, 1)
	}

	deferList = append(deferList.([]DeferEntry), DeferEntry{Func: deferFunc, Strict: strict})
	thread.SetLocal(deferKey, deferList)
}

// loader is the starlark loader function
func (a *App) loader(thread *starlark.Thread, moduleFullPath string) (starlark.StringDict, error) {
	if strings.HasSuffix(moduleFullPath, util.STARLARK_FILE_SUFFIX) {
		// Load the starlark file rather than the plugin
		return a.loadStarlark(thread, moduleFullPath, a.starlarkCache)
	}

	if a.Metadata.Loads == nil || !slices.Contains(a.Metadata.Loads, moduleFullPath) {
		return nil, fmt.Errorf("app %s is not permitted to load plugin %s. Audit the app and approve permissions", a.Path, moduleFullPath)
	}

	modulePath, moduleName, accountName := parseModulePath(moduleFullPath)
	plugin, err := a.pluginLookup(thread, modulePath)
	if err != nil {
		return nil, err
	}

	// Add calls to the hook function, which will do the permission checks at invocation time to
	// verify if the application has approval to call the specified function.
	// The audit loader will replace the builtins with dummy methods, so the hook is not added for the audit loader
	hookedDict := make(starlark.StringDict)
	for funcName, pluginInfo := range plugin {
		hookedDict[funcName] = a.pluginHook(moduleFullPath, accountName, funcName, pluginInfo)
	}

	ret := make(starlark.StringDict)
	ret[moduleName] = starlarkstruct.FromStringDict(starlarkstruct.Default, hookedDict)
	return ret, nil

}

func parseModulePath(moduleFullPath string) (string, string, string) {
	parts := strings.Split(moduleFullPath, util.ACCOUNT_SEPERATOR)
	modulePath := parts[0]
	moduleName := strings.TrimSuffix(modulePath, "."+util.BUILTIN_PLUGIN_SUFFIX)
	accountName := ""
	if len(parts) > 1 {
		accountName = parts[1]
	}
	return modulePath, moduleName, accountName
}

// pluginLookup looks up the plugin. Audit checks need to be done by the caller
func (a *App) pluginLookup(_ *starlark.Thread, module string) (utils.PluginMap, error) {
	pluginDict, ok := builtInPlugins[module]
	if !ok {
		return nil, fmt.Errorf("module %s not found", module) // TODO extend loading
	}

	return pluginDict, nil
}

func (a *App) pluginHook(modulePath, accountName, functionName string, pluginInfo *utils.PluginInfo) *starlark.Builtin {
	hook := func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		a.Trace().Msgf("Plugin called: %s.%s", modulePath, functionName)

		if a.Metadata.Permissions == nil {
			return nil, fmt.Errorf("app %s has no permissions configured, plugin call %s.%s is blocked. Audit the app and approve permissions", a.Path, modulePath, functionName)
		}

		approved := false
		var lastError error
		for _, p := range a.Metadata.Permissions {
			a.Trace().Msgf("Checking permission %s.%s call %s.%s", p.Plugin, p.Method, modulePath, functionName)
			if p.Plugin == modulePath && p.Method == functionName {
				if len(p.Arguments) > 0 {
					if len(p.Arguments) > len(args) {
						lastError = fmt.Errorf("app %s is not permitted to call %s.%s with %d arguments, %d or more positional arguments are required (permissions checks are not supported for kwargs). Audit the app and approve permissions", a.Path, modulePath, functionName, len(args), len(p.Arguments))
						continue
					}
					argMismatch := false
					for i, arg := range p.Arguments {
						expect := fmt.Sprintf("%q", arg)
						if args[i].String() != fmt.Sprintf("%q", arg) {
							lastError = fmt.Errorf("app %s is not permitted to call %s.%s with argument %d having value %s, expected %s. Update the app or audit and approve permissions", a.Path, modulePath, functionName, i, args[i].String(), expect)
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

				if a.MainApp != "" {
					var isRead bool
					if p.IsRead != nil {
						// Permission defines isRead, use that
						isRead = *p.IsRead
					} else {
						// Use the plugin defined isRead value
						isRead = pluginInfo.IsRead
					}

					if !isRead {
						// Write API, check if stage/preview has write access
						if strings.HasPrefix(string(a.Id), utils.ID_PREFIX_APP_STAGE) && !a.Settings.StageWriteAccess {
							return nil, fmt.Errorf("stage app %s is not permitted to call %s.%s args %v. Stage app does not have access to write operations", a.Path, modulePath, functionName, p.Arguments)
						}

						if strings.HasPrefix(string(a.Id), utils.ID_PREFIX_APP_PREVIEW) && !a.Settings.PreviewWriteAccess {
							return nil, fmt.Errorf("preview app %s is not permitted to call %s.%s args %v. Preview app does not have access to write operations", a.Path, modulePath, functionName, p.Arguments)
						}
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
				return nil, fmt.Errorf("app %s is not permitted to call %s.%s. Audit the app and approve permissions", a.Path, modulePath, functionName)
			}
		}

		// Get the plugin from the app config
		plugin, err := a.plugins.GetPlugin(pluginInfo, accountName)
		if err != nil {
			return nil, err
		}

		// Get the plugin function using reflection
		pluginValue := reflect.ValueOf(plugin).MethodByName(pluginInfo.HandlerName)
		if pluginValue.IsNil() {
			return nil, fmt.Errorf("plugin func %s.%s cannot be resolved", modulePath, functionName)
		}

		builtinFunc, ok := pluginValue.Interface().(func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error))
		if !ok {
			return nil, fmt.Errorf("plugin %s.%s is not a starlark function", modulePath, functionName)
		}

		// Wrap the plugin function call with error handling
		errorHandlingWrapper := pluginErrorWrapper(builtinFunc)

		// Pass the module full path as a thread local
		thread.SetLocal(TL_CURRENT_MODULE_FULL_PATH, modulePath)

		// Call the builtin function
		newBuiltin := starlark.NewBuiltin(functionName, errorHandlingWrapper)
		val, err := newBuiltin.CallInternal(thread, args, kwargs)
		return val, err
	}

	return starlark.NewBuiltin(functionName, hook)
}
