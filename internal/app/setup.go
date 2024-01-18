// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"

	"github.com/claceio/clace/internal/app/dev"
	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func init() {
	resolve.AllowRecursion = true
}

func (a *App) loadStarlarkConfig() error {
	a.Info().Str("path", a.Path).Str("domain", a.Domain).Msg("Loading app")

	buf, err := a.sourceFS.ReadFile(util.APP_FILE_NAME)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", util.APP_FILE_NAME, err)
	}

	thread := &starlark.Thread{
		Name:  a.Path,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) }, // TODO use logger
		Load:  a.loader,
	}

	builtin, err := a.createBuiltin()
	if err != nil {
		return err
	}

	a.globals, err = starlark.ExecFile(thread, util.APP_FILE_NAME, buf, builtin)
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

	a.Name, err = util.GetStringAttr(a.appDef, "name")
	if err != nil {
		return err
	}
	a.CustomLayout, err = util.GetBoolAttr(a.appDef, "custom_layout")
	if err != nil {
		return err
	}

	if a.IsDev {
		a.appDev.CustomLayout = a.CustomLayout
		a.appDev.JsLibs, err = a.loadLibraryInfo()
		if err != nil {
			return err
		}
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
		if converted, err = utils.UnmarshalStarlark(dict); err != nil {
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

func (a *App) createBuiltin() (starlark.StringDict, error) {
	builtin := util.CreateBuiltin()
	if builtin == nil {
		return nil, errors.New("error creating builtin")
	}

	var err error
	if builtin, err = a.addSchemaTypes(builtin); err != nil {
		return nil, err
	}

	return builtin, nil
}

func (a *App) addSchemaTypes(builtin starlark.StringDict) (starlark.StringDict, error) {
	if a.storeInfo == nil || len(a.storeInfo.Types) == 0 {
		return builtin, nil
	}

	// Create a copy of the builtins, don't modify the original
	newBuiltins := starlark.StringDict{}
	for k, v := range builtin {
		newBuiltins[k] = v
	}

	// Add type module for referencing type names
	typeDict := starlark.StringDict{}
	for _, t := range a.storeInfo.Types {
		tb := utils.TypeBuilder{Name: t.Name, Fields: t.Fields}
		typeDict[t.Name] = starlark.NewBuiltin(t.Name, tb.CreateType)
	}

	typeModule := starlarkstruct.Module{
		Name:    util.STARLARK_TYPE_MODULE,
		Members: typeDict,
	}
	newBuiltins[util.STARLARK_TYPE_MODULE] = &typeModule

	// Add table module for referencing table names
	tableDict := starlark.StringDict{}
	for _, t := range a.storeInfo.Types {
		tableDict[t.Name] = starlark.String(t.Name)
	}

	tableModule := starlarkstruct.Module{
		Name:    util.TABLE_MODULE,
		Members: tableDict,
	}
	newBuiltins[util.TABLE_MODULE] = &tableModule

	return newBuiltins, nil
}

func verifyConfig(globals starlark.StringDict) (*starlarkstruct.Struct, error) {
	if !globals.Has(util.APP_CONFIG_KEY) {
		return nil, fmt.Errorf("%s not defined, check %s, add '%s = ace.app(...)'", util.APP_CONFIG_KEY, util.APP_FILE_NAME, util.APP_CONFIG_KEY)
	}
	appDef, ok := globals[util.APP_CONFIG_KEY].(*starlarkstruct.Struct)
	if !ok {
		return nil, fmt.Errorf("%s not of type ace.app in %s", util.APP_CONFIG_KEY, util.APP_FILE_NAME)
	}
	return appDef, nil
}

func (a *App) initRouter() error {
	var defaultHandler starlark.Callable
	if a.globals.Has(util.DEFAULT_HANDLER) {
		var ok bool
		defaultHandler, ok = a.globals[util.DEFAULT_HANDLER].(starlark.Callable)
		if !ok {
			return fmt.Errorf("%s is not a function", util.DEFAULT_HANDLER)
		}
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
	count := 0
	for iter.Next(&val) {
		count++

		if err = a.addPageRoute(count, router, val, defaultHandler); err != nil {
			return err
		}
	}

	// Mount static dir
	staticPattern := path.Join("/", a.Config.Routing.StaticDir, "*")
	router.Handle(staticPattern, http.StripPrefix(a.Path, util.FileServer(a.sourceFS)))

	a.appRouter = chi.NewRouter()
	a.Trace().Msgf("Mounting app %s at %s", a.Name, a.Path)
	a.appRouter.Mount(a.Path, router)

	chi.Walk(a.appRouter, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		//a.Trace().Msgf("Routes: %s %s\n", method, route)
		return nil
	})

	return nil
}

func (a *App) addPageRoute(count int, router *chi.Mux, pageVal starlark.Value, defaultHandler starlark.Callable) error {
	var ok bool
	var err error
	var pageDef *starlarkstruct.Struct

	if pageDef, ok = pageVal.(*starlarkstruct.Struct); !ok {
		return fmt.Errorf("pages entry %d is not a struct", count)
	}
	var pathStr, htmlFile, blockStr, methodStr, rtypeStr string
	if pathStr, err = util.GetStringAttr(pageDef, "path"); err != nil {
		return err
	}
	if methodStr, err = util.GetStringAttr(pageDef, "method"); err != nil {
		return err
	}
	if htmlFile, err = util.GetStringAttr(pageDef, "full"); err != nil {
		return err
	}
	if blockStr, err = util.GetStringAttr(pageDef, "partial"); err != nil {
		return err
	}

	if rtypeStr, err = util.GetStringAttr(pageDef, "type"); err != nil {
		return err
	}

	if htmlFile == "" {
		if a.CustomLayout {
			htmlFile = util.INDEX_FILE
		} else {
			htmlFile = util.INDEX_GEN_FILE
		}
	}

	handler := defaultHandler // Use app level default handler, which could also be nil
	handlerAttr, _ := pageDef.Attr("handler")
	if handlerAttr != nil {
		if handler, ok = handlerAttr.(starlark.Callable); !ok {
			return fmt.Errorf("handler for page %s is not a function", pathStr)
		}
	}

	handlerFunc := a.createHandlerFunc(htmlFile, blockStr, handler, rtypeStr)
	if err = a.handleFragments(router, pathStr, count, htmlFile, blockStr, pageDef, handler); err != nil {
		return err
	}
	a.Trace().Msgf("Adding page route %s <%s>", methodStr, pathStr)
	router.Method(methodStr, pathStr, handlerFunc)
	return nil
}

func (a *App) handleFragments(router *chi.Mux, pagePath string, pageCount int, htmlFile string, block string, page *starlarkstruct.Struct, handlerCallable starlark.Callable) error {
	// Iterate through all the pages
	var err error
	fragmentAttr, err := page.Attr("fragments")
	if err != nil {
		// No fragments defined
		return nil
	}

	var ok bool
	var fragmentList *starlark.List
	if fragmentList, ok = fragmentAttr.(*starlark.List); !ok {
		return fmt.Errorf("fragments attribute in page %d is not a list", pageCount)
	}
	iter := fragmentList.Iterate()
	var val starlark.Value
	count := 0
	for iter.Next(&val) {
		count++

		var fragmentDef *starlarkstruct.Struct
		if fragmentDef, ok = val.(*starlarkstruct.Struct); !ok {
			return fmt.Errorf("page %d fragment %d is not a struct", pageCount, count)
		}

		var pathStr, blockStr, methodStr, rtypeStr string
		if pathStr, err = util.GetStringAttr(fragmentDef, "path"); err != nil {
			return err
		}
		if methodStr, err = util.GetStringAttr(fragmentDef, "method"); err != nil {
			return err
		}
		if blockStr, err = util.GetStringAttr(fragmentDef, "partial"); err != nil {
			return err
		}

		if rtypeStr, err = util.GetStringAttr(fragmentDef, "type"); err != nil {
			return err
		}

		if blockStr == "" {
			// Inherit page level block setting
			blockStr = block
		}

		fragmentCallback := handlerCallable
		handler, _ := fragmentDef.Attr("handler")
		if handler != nil {
			// If new handler is defined at fragment level, that is verified. Otherwise the
			// page level handler is used
			if fragmentCallback, ok = handler.(starlark.Callable); !ok {
				return fmt.Errorf("handler for page %d fragment %d is not a function", pageCount, count)
			}
		}
		handlerFunc := a.createHandlerFunc(htmlFile, blockStr, fragmentCallback, rtypeStr)

		fragmentPath := path.Join(pagePath, pathStr)
		a.Trace().Msgf("Adding fragment route %s <%s>", methodStr, fragmentPath)
		router.Method(methodStr, fragmentPath, handlerFunc)
	}

	return nil
}

func (a *App) createInternalRoutes(router *chi.Mux) error {
	if a.IsDev || a.Config.Routing.PushEvents {
		router.Get(utils.APP_INTERNAL_URL_PREFIX+"/sse", a.sseHandler)
	}

	return nil
}

func (a *App) loadLibraryInfo() ([]dev.JSLibrary, error) {
	lib, err := a.appDef.Attr("libraries")
	if err != nil {
		return nil, err
	}

	if lib == nil {
		// No libraries defined
		return nil, nil
	}

	var ok bool
	var libList *starlark.List
	if libList, ok = lib.(*starlark.List); !ok {
		return nil, fmt.Errorf("libraries is not a list")
	}

	libraries := []dev.JSLibrary{}
	iter := libList.Iterate()
	var libValue starlark.Value
	count := 0
	for iter.Next(&libValue) {
		count++
		libStruct, ok := libValue.(*starlarkstruct.Struct)
		if ok {
			var name, version string
			var esbuildArgs []string
			if name, err = util.GetStringAttr(libStruct, "name"); err != nil {
				return nil, err
			}
			if version, err = util.GetStringAttr(libStruct, "version"); err != nil {
				return nil, err
			}
			if esbuildArgs, err = util.GetListStringAttr(libStruct, "args", true); err != nil {
				return nil, err
			}
			jsLib := dev.NewLibraryESM(name, version, esbuildArgs)
			libraries = append(libraries, *jsLib)
		} else {
			libStr, ok := libValue.(starlark.String)
			if !ok {
				return nil, fmt.Errorf("libraries entry %d is not a string or library", count)
			}
			jsLib := dev.NewLibrary(string(libStr))
			libraries = append(libraries, *jsLib)
		}
	}

	return libraries, nil
}
