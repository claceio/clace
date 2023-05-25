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

	"github.com/claceio/clace/internal/stardefs"
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
func (a *App) loader(_ *starlark.Thread, module string) (starlark.StringDict, error) {
	pluginDict, ok := builtInPlugins[module]
	if !ok {
		return nil, fmt.Errorf("module %s not found", module) // TODO extend loading
	}

	return pluginDict, nil
}

func (a *App) loadStarlark() error {
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

	if !a.globals.Has(APP_CONFIG_KEY) {
		return fmt.Errorf("%s not defined, check %s, add '%s = clace.app(...)'", APP_CONFIG_KEY, APP_FILE_NAME, APP_CONFIG_KEY)
	}
	var ok bool
	a.appDef, ok = a.globals[APP_CONFIG_KEY].(*starlarkstruct.Struct)
	if !ok {
		return fmt.Errorf("%s not of type clace.app in %s", APP_CONFIG_KEY, APP_FILE_NAME)
	}

	a.name, err = getStringAttr(a.appDef, "name")
	if err != nil {
		return err
	}

	a.layout, err = getStringAttr(a.appDef, "layout")
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

func getStringAttr(s *starlarkstruct.Struct, key string) (string, error) {
	v, err := s.Attr(key)
	if err != nil {
		return "", fmt.Errorf("error getting %s: %s", key, err)
	}
	var vs starlark.String
	var ok bool
	if vs, ok = v.(starlark.String); !ok {
		return "", fmt.Errorf("%s is not a string", key)
	}
	return vs.GoString(), nil
}

func (a *App) initRouter() error {

	var defaultHandler starlark.Callable
	if a.globals.Has(DEFAULT_HANDLER) {
		defaultHandler, _ = a.globals[DEFAULT_HANDLER].(starlark.Callable)
	}
	router := chi.NewRouter()

	// Iterate through all the pages
	pages, err := a.appDef.Attr("pages")
	if err != nil {
		return err
	}

	iter := pages.(*starlark.List).Iterate()
	var val starlark.Value
	for iter.Next(&val) {
		pageDef := val.(*starlarkstruct.Struct)
		path, err := pageDef.Attr("path")
		if err != nil {
			return err
		}
		pathStr := path.(starlark.String).GoString()

		html, err := pageDef.Attr("html")
		if err != nil {
			return err
		}
		htmlStr := html.(starlark.String).GoString()

		method, err := pageDef.Attr("method")
		if err != nil {
			return err
		}
		methodStr := method.(starlark.String).GoString()

		handler, _ := pageDef.Attr("handler")
		if handler == nil {
			// Use app level default handler if configured
			handler = defaultHandler
		}
		if handler == nil {
			return fmt.Errorf("page %s has no handler, and no app level default handler function is specified", path)
		}
		handlerCallable := handler.(starlark.Callable)

		routeHandler := a.createRouteHandler(pathStr, htmlStr, methodStr, handlerCallable)
		router.Route(pathStr, routeHandler)
	}

	if err = a.createInternalRoutes(router); err != nil {
		return err
	}

	a.appRouter = chi.NewRouter()
	a.appRouter.Mount(a.Path, router)
	return nil
}

func (a *App) createInternalRoutes(router *chi.Mux) error {
	if a.IsDev || a.AutoReload || a.Config.Routing.PushEvents {
		router.Get("/_clace/sse", a.sseHandler)
	}

	return nil
}

func (a *App) createRouteHandler(path, html, method string, handler starlark.Callable) func(r chi.Router) {
	goHandler := func(w http.ResponseWriter, r *http.Request) {
		thread := &starlark.Thread{
			Name:  a.Path,
			Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
		}

		ret, err := starlark.Call(thread, handler, starlark.Tuple{starlark.None}, nil)
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

		err = a.template.ExecuteTemplate(w, html, value)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	retFunc := func(r chi.Router) {
		r.Method(method, path, http.HandlerFunc(goHandler))
	}
	return retFunc
}
