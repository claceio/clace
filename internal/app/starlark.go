// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"net/http"

	"github.com/claceio/clace/internal/stardefs"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func (a *App) loadStarlark() error {
	a.Info().Str("path", a.Path).Str("domain", a.Domain).Msg("Loading app")

	buf, err := a.fs.ReadFile(APP_FILE)
	if err != nil {
		return err
	}

	// The Thread defines the behavior of the built-in 'print' function.
	thread := &starlark.Thread{
		Name:  a.Path,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
	}

	predeclared := stardefs.MakeLoadBuiltins()
	a.globals, err = starlark.ExecFile(thread, APP_FILE, buf, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			a.Error().Err(err).Str("trace", evalErr.Backtrace()).Msg("Error loading app")
		} else {
			a.Error().Err(err).Msg("Error loading app")
		}
		return err
	}
	err = a.initRouter()
	if err != nil {
		return err
	}
	return nil
}

func (a *App) initRouter() error {
	if !a.globals.Has(APP_KEY) {
		return fmt.Errorf("app not defined, check %s, add 'app = cl_app(...)'", APP_FILE)
	}
	var ok bool
	a.appDef, ok = a.globals["app"].(*starlarkstruct.Struct)
	if !ok {
		return fmt.Errorf("app not of type APP in %s", APP_FILE)
	}

	defaultHandler, _ := a.globals[DEFAULT_HANDLER].(starlark.Callable)
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

		fmt.Printf("ret = %s %s %v\n", ret.String(), ret.Type(), ret)
		value, err := stardefs.Convert(ret)
		if err != nil {
			a.Error().Err(err).Msg("error converting response")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Printf("value = %s %T %v\n", value, value, value)
		a.template.ExecuteTemplate(w, html, value)
	}

	retFunc := func(r chi.Router) {
		r.Method(method, path, http.HandlerFunc(goHandler))
	}
	return retFunc
}
