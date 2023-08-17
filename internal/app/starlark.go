// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"slices"

	"github.com/claceio/clace/internal/stardefs"
	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
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

	pluginName := fmt.Sprintf("%s.%s", name, PLUGIN_SUFFIX)
	pluginDict := make(starlark.StringDict)
	pluginDict[name] = plugin
	builtInPlugins[pluginName] = pluginDict
}

// loader is the starlark loader function
func (a *App) loader(t *starlark.Thread, module string) (starlark.StringDict, error) {
	if a.Loads == nil || !slices.Contains(a.Loads, module) {
		return nil, fmt.Errorf("app %s is not permitted to load plugin %s. Audit the app and approve permissions requests", a.Path, module)
	}

	return a.loaderInternal(t, module)
}

// loaderInternal is the starlark loader function, with no audit checks
func (a *App) loaderInternal(_ *starlark.Thread, module string) (starlark.StringDict, error) {
	pluginDict, ok := builtInPlugins[module]
	if !ok {
		return nil, fmt.Errorf("module %s not found", module) // TODO extend loading
	}

	return pluginDict, nil
}

func (a *App) Audit() (*AuditResult, error) {
	buf, err := a.fs.ReadFile(APP_FILE_NAME)
	if err != nil {
		return nil, err
	}

	auditLoader := func(t *starlark.Thread, module string) (starlark.StringDict, error) {
		// The loader in audit mode is used to track the modules that are loaded.
		// A copy of the real loader's response is returned, with builtins replaced with dummy methods,
		// so that the audit can be run without any side effects
		pluginDict, err := a.loaderInternal(t, module)
		if err != nil {
			return nil, err
		}

		// Replace all the builtins with dummy methods
		dummyDict := make(starlark.StringDict)
		for k, v := range pluginDict {
			val := make(starlark.StringDict)
			if s, ok := v.(*starlarkstruct.Struct); ok {
				for _, attr := range s.AttrNames() {
					val[attr] = starlark.NewBuiltin(k, func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
						a.Info().Msgf("Plugin called during audit: %s.%s.%s", module, k, attr)
						return starlarkstruct.FromStringDict(starlarkstruct.Default, make(starlark.StringDict)), nil
					})
				}
			}
			dummyDict[k] = starlarkstruct.FromStringDict(starlarkstruct.Default, val)
		}

		return dummyDict, nil
	}

	thread := &starlark.Thread{
		Name:  a.Path,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) }, // TODO use logger
		Load:  auditLoader,
	}

	builtin := stardefs.CreateBuiltin()
	if builtin == nil {
		return nil, errors.New("error creating builtin")
	}

	_, prog, err := starlark.SourceProgram(APP_FILE_NAME, buf, builtin.Has)
	if err != nil {
		return nil, fmt.Errorf("parsing source failed %v", err)
	}

	loads := []string{}
	for i := 0; i < prog.NumLoads(); i++ {
		p, _ := prog.Load(i)
		if !slices.Contains(loads, p) {
			loads = append(loads, p)
		}
	}

	// This runs the starlark script, with dummy plugin methods
	// The intent is to load the permissions from the app definition while trying
	// to avoid any potential side effects from script
	globals, err := prog.Init(thread, builtin)
	if err != nil {
		return nil, fmt.Errorf("source init failed %v", err)
	}

	return a.createAuditResponse(loads, globals)
}

func (a *App) createAuditResponse(loads []string, globals starlark.StringDict) (*AuditResult, error) {
	// the App entry should not get updated during the audit call, since there
	// can be audit calls when the app is running.
	appDef, err := verifyConfig(globals)
	if err != nil {
		return nil, err
	}

	perms := []utils.Permission{}
	results := AuditResult{
		Loads:       loads,
		Permissions: perms,
	}
	permissions, err := appDef.Attr("permissions")
	if err != nil {
		return &results, nil
	}

	var ok bool
	var permList *starlark.List
	if permList, ok = permissions.(*starlark.List); !ok {
		return nil, fmt.Errorf("permissions is not a list")
	}
	iter := permList.Iterate()
	var val starlark.Value
	count := -1
	for iter.Next(&val) {
		count++
		var perm *starlarkstruct.Struct
		if perm, ok = val.(*starlarkstruct.Struct); !ok {
			return nil, fmt.Errorf("permissions entry %d is not a struct", count)
		}
		a.Info().Msgf("perm: %+v", perm)
		var pluginStr, methodStr string
		var args []string
		if pluginStr, err = getStringAttr(perm, "plugin"); err != nil {
			return nil, err
		}
		if methodStr, err = getStringAttr(perm, "method"); err != nil {
			return nil, err
		}
		if args, err = getListStringAttr(perm, "arguments", true); err != nil {
			return nil, err
		}
		perms = append(perms, utils.Permission{
			Plugin:    pluginStr,
			Method:    methodStr,
			Arguments: args,
		})

	}
	results.Permissions = perms
	return &results, nil
}

func (a *App) loadStarlarkConfig() error {
	a.Info().Str("path", a.Path).Str("domain", a.Domain).Msg("Loading app")

	buf, err := a.fs.ReadFile(APP_FILE_NAME)
	if err != nil {
		return err
	}

	thread := &starlark.Thread{
		Name:  a.Path,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) }, // TODO use logger
		Load:  a.loader,
	}

	builtin := stardefs.CreateBuiltin()
	if builtin == nil {
		return errors.New("error creating builtin")
	}
	a.globals, err = starlark.ExecFile(thread, APP_FILE_NAME, buf, builtin)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			a.Error().Err(err).Str("trace", evalErr.Backtrace()).Msg("Error loading app")
		} else {
			a.Error().Err(err).Msg("Error loading app")
		}
		return err
	}

	a.appDef, err = verifyConfig(a.globals)
	if err != nil {
		return err
	}

	a.Name, err = getStringAttr(a.appDef, "name")
	if err != nil {
		return err
	}

	a.customLayout, err = getBoolAttr(a.appDef, "custom_layout")
	if err != nil {
		return err
	}

	var settingsMap map[string]interface{}
	settings, err := a.appDef.Attr("settings")
	if err == nil {
		var dict *starlark.Dict
		var ok bool
		if dict, ok = settings.(*starlark.Dict); !ok {
			return errors.New("settings is not a starlark dict")
		}
		var converted any
		if converted, err = stardefs.Unmarshal(dict); err != nil {
			return err
		}
		if settingsMap, ok = converted.(map[string]interface{}); !ok {
			return errors.New("settings is not a map")
		}
	} else {
		settingsMap = make(map[string]interface{})
	}

	// Update the app config with entries loaded from the settings map
	var jsonBuf bytes.Buffer
	if err = json.NewEncoder(&jsonBuf).Encode(settingsMap); err != nil {
		return err
	}
	if err = json.Unmarshal(jsonBuf.Bytes(), a.Config); err != nil {
		return err
	}

	// Initialize the router configuration
	err = a.initRouter()
	if err != nil {
		return err
	}
	return nil
}

func verifyConfig(globals starlark.StringDict) (*starlarkstruct.Struct, error) {
	if !globals.Has(APP_CONFIG_KEY) {
		return nil, fmt.Errorf("%s not defined, check %s, add '%s = clace.app(...)'", APP_CONFIG_KEY, APP_FILE_NAME, APP_CONFIG_KEY)
	}
	appDef, ok := globals[APP_CONFIG_KEY].(*starlarkstruct.Struct)
	if !ok {
		return nil, fmt.Errorf("%s not of type clace.app in %s", APP_CONFIG_KEY, APP_FILE_NAME)
	}
	return appDef, nil
}

func (a *App) initRouter() error {
	var defaultHandler starlark.Callable
	if a.globals.Has(DEFAULT_HANDLER) {
		defaultHandler, _ = a.globals[DEFAULT_HANDLER].(starlark.Callable)
	}
	router := chi.NewRouter()
	if err := a.createInternalRoutes(router); err != nil {
		return err
	}

	// Iterate through all the pages
	pages, err := a.appDef.Attr("pages")
	if err != nil {
		return err
	}

	var ok bool
	var pageList *starlark.List
	if pageList, ok = pages.(*starlark.List); !ok {
		return fmt.Errorf("pages is not a list")
	}
	iter := pageList.Iterate()
	var val starlark.Value
	count := -1
	for iter.Next(&val) {
		count++
		var pageDef *starlarkstruct.Struct
		if pageDef, ok = val.(*starlarkstruct.Struct); !ok {
			return fmt.Errorf("pages entry %d is not a struct", count)
		}
		var pathStr, htmlStr, blockStr, methodStr string
		if pathStr, err = getStringAttr(pageDef, "path"); err != nil {
			return err
		}
		if methodStr, err = getStringAttr(pageDef, "method"); err != nil {
			return err
		}
		if htmlStr, err = getStringAttr(pageDef, "html"); err != nil {
			return err
		}
		if blockStr, err = getStringAttr(pageDef, "block"); err != nil {
			return err
		}

		if htmlStr == "" {
			if a.customLayout {
				htmlStr = INDEX_FILE
			} else {
				htmlStr = INDEX_GEN_FILE
			}
		}

		handler, _ := pageDef.Attr("handler")
		if handler == nil {
			// Use app level default handler if configured
			handler = defaultHandler
		}
		if handler == nil {
			return fmt.Errorf("page %s has no handler, and no app level default handler function is specified", pathStr)
		}
		var handlerCallable starlark.Callable
		if handlerCallable, ok = handler.(starlark.Callable); !ok {
			return fmt.Errorf("handler for page %s is not a function", pathStr)
		}

		routeHandler := a.createRouteHandler(htmlStr, blockStr, handlerCallable)
		if err = a.handleFragments(pageDef); err != nil {
			return err
		}
		a.Trace().Msgf("Adding route <%s>", pathStr)
		router.Method(methodStr, pathStr, routeHandler)
	}

	a.Trace().Msgf("Mounting route %s", a.Path)
	a.appRouter = chi.NewRouter()
	a.appRouter.Mount(a.Path, router)
	return nil
}

func (a *App) handleFragments(page *starlarkstruct.Struct) error {
	// TODO add fragment support
	return nil
}

func (a *App) createInternalRoutes(router *chi.Mux) error {
	if a.IsDev || a.AutoReload || a.Config.Routing.PushEvents {
		router.Get("/_clace/sse", a.sseHandler)
	}

	return nil
}

func (a *App) createRouteHandler(html, block string, handler starlark.Callable) http.HandlerFunc {
	goHandler := func(w http.ResponseWriter, r *http.Request) {
		thread := &starlark.Thread{
			Name:  a.Path,
			Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
		}

		isHtmxRequest := r.Header.Get("HX-Request") == "true" && !(r.Header.Get("HX-Boosted") == "true")

		requestData := map[string]interface{}{
			"Name":       a.Name,
			"Path":       a.Path,
			"IsDev":      a.IsDev,
			"AutoReload": a.AutoReload,
			"IsHtmx":     isHtmxRequest,
		}

		chiContext := chi.RouteContext(r.Context())
		params := map[string]string{}
		if chiContext != nil && chiContext.URLParams.Keys != nil {
			for i, k := range chiContext.URLParams.Keys {
				params[k] = chiContext.URLParams.Values[i]
			}
		}
		requestData["UrlParams"] = params

		r.ParseForm()
		requestData["Form"] = r.Form
		requestData["Query"] = r.URL.Query()
		requestData["PostForm"] = r.PostForm

		dataStarlark, err := stardefs.Marshal(requestData)
		if err != nil {
			a.Error().Err(err).Msg("error converting request data")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.Trace().Msgf("Calling handler %s %s", handler.Name(), dataStarlark.String())

		ret, err := starlark.Call(thread, handler, starlark.Tuple{dataStarlark}, nil)
		if err != nil {
			a.Error().Err(err).Msg("error calling handler")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// TODO : handle redirects and renders

		value, err := stardefs.Unmarshal(ret)
		if err != nil {
			a.Error().Err(err).Msg("error converting response")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		requestData["Data"] = value
		requestData["Config"] = a.Config
		if isHtmxRequest && block != "" {
			a.Trace().Msgf("Rendering block %s", block)
			err = a.template.ExecuteTemplate(w, block, requestData)
		} else {
			a.Trace().Msgf("Rendering page %s", html)
			err = a.template.ExecuteTemplate(w, html, requestData)
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	return goHandler
}
