// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/claceio/clace/internal/stardefs"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func (a *App) loadStarlark() error {
	a.Info().Str("path", a.Path).Str("domain", a.Domain).Msg("Loading app")

	buf, err := a.fs.ReadFile(APP_FILE_NAME)
	if err != nil {
		return err
	}

	thread := &starlark.Thread{
		Name:  a.Path,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) }, // TODO use logger
	}

	predeclared := stardefs.CreateBuiltins()
	a.globals, err = starlark.ExecFile(thread, APP_FILE_NAME, buf, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			a.Error().Err(err).Str("trace", evalErr.Backtrace()).Msg("Error loading app")
		} else {
			a.Error().Err(err).Msg("Error loading app")
		}
		return err
	}

	if !a.globals.Has(APP_CONFIG_KEY) {
		return fmt.Errorf("%s not defined, check %s, add '%s = app(...)'", APP_CONFIG_KEY, APP_FILE_NAME, APP_CONFIG_KEY)
	}
	var ok bool
	a.appDef, ok = a.globals[APP_CONFIG_KEY].(*starlarkstruct.Struct)
	if !ok {
		return fmt.Errorf("%s not of type app in %s", APP_CONFIG_KEY, APP_FILE_NAME)
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
		if converted, err = stardefs.Convert(dict); err != nil {
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
	if err = json.Unmarshal(jsonBuf.Bytes(), a.config); err != nil {
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

	a.appRouter = chi.NewRouter()
	a.appRouter.Mount(a.Path, router)
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

		value, err := stardefs.Convert(ret)
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
