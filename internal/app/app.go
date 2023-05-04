// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/claceio/clace/internal/stardefs"
	"github.com/claceio/clace/internal/utils"
	"github.com/claceio/clace/internal/utils/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	DEFAULT_HANDLER   = "handler"
	METHODS_DELIMITER = ","
)

type App struct {
	*utils.Logger
	*utils.AppEntry
	fs          fs.FS
	mu          sync.Mutex
	initialized bool
	globals     starlark.StringDict
	appDef      *starlarkstruct.Struct
	appRouter   *chi.Mux
}

func NewApp(logger *utils.Logger, app *utils.AppEntry) *App {
	return &App{
		Logger:   logger,
		AppEntry: app,
	}
}

func (a *App) Initialize() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized {
		return nil
	}

	a.fs = os.DirFS(a.CodeUrl)
	err := a.load()
	if err != nil {
		return err
	}
	a.initialized = true
	return nil
}

func (a *App) load() error {
	a.Info().Str("path", a.Path).Str("domain", a.Domain).Msg("Loading app")

	file, err := a.fs.Open("clace.star")
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, file)
	if err != nil {
		return err
	}

	// The Thread defines the behavior of the built-in 'print' function.
	thread := &starlark.Thread{
		Name:  a.Path,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
	}

	// This dictionary defines the pre-declared environment.
	predeclared := starlark.StringDict{
		"App":      starlark.NewBuiltin("App", stardefs.CreateAppBuiltin),
		"Page":     starlark.NewBuiltin("Page", stardefs.CreatePageBuiltin),
		"Fragment": starlark.NewBuiltin("Fragment", stardefs.CreateFragmentBuiltin),
	}

	// Execute a program.
	a.globals, err = starlark.ExecFile(thread, "clace.star", buf, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			log.Fatal(evalErr.Backtrace())
		}
		log.Fatal(err)
	}
	err = a.initRouter()
	if err != nil {
		return err
	}
	return nil
}

func (a *App) initRouter() error {
	a.appDef = a.globals["app"].(*starlarkstruct.Struct)

	if a.appDef == nil {
		return fmt.Errorf("App not defined, check clace.star, add 'app = App(...)'")
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
		/*
			if err != nil {
				return err
			}
		*/
		if handler == nil {
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

		_, err := starlark.Call(thread, handler, starlark.Tuple{starlark.None}, nil)
		if err != nil {
			a.Error().Err(err).Msg("error getting App")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.Info().Msgf("gohandler called %s %s", path, html)
	}

	retFunc := func(r chi.Router) {
		r.Method(method, path, http.HandlerFunc(goHandler))
	}
	return retFunc
}

func (a *App) PrintGlobals() {
	fmt.Println("\nGlobals:")
	for _, name := range a.globals.Keys() {
		v := a.globals[name]
		fmt.Printf("%s (%s) = %s\n", name, v.Type(), v.String())
	}
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.Info().Str("method", r.Method).Str("url", r.URL.String()).Msg("App Received request")
	a.appRouter.ServeHTTP(w, r)
}
