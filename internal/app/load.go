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
	"strings"

	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func (a *App) loadStarlarkConfig() error {
	a.Info().Str("path", a.Path).Str("domain", a.Domain).Msg("Loading app")

	buf, err := a.sourceFS.ReadFile(APP_FILE_NAME)
	if err != nil {
		return err
	}

	thread := &starlark.Thread{
		Name:  a.Path,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) }, // TODO use logger
		Load:  a.loader,
	}

	builtin := CreateBuiltin()
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
		var ok bool
		defaultHandler, ok = a.globals[DEFAULT_HANDLER].(starlark.Callable)
		if !ok {
			return fmt.Errorf("%s is not a function", DEFAULT_HANDLER)
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
	router.Handle(staticPattern, http.StripPrefix(a.Path, FileServer(a.sourceFS)))

	a.appRouter = chi.NewRouter()
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
	var pathStr, htmlFile, blockStr, methodStr string
	if pathStr, err = getStringAttr(pageDef, "path"); err != nil {
		return err
	}
	if methodStr, err = getStringAttr(pageDef, "method"); err != nil {
		return err
	}
	if htmlFile, err = getStringAttr(pageDef, "html"); err != nil {
		return err
	}
	if blockStr, err = getStringAttr(pageDef, "block"); err != nil {
		return err
	}

	if htmlFile == "" {
		if a.customLayout {
			htmlFile = INDEX_FILE
		} else {
			htmlFile = INDEX_GEN_FILE
		}
	}

	handler := defaultHandler // Use app level default handler, which could also be nil
	handlerAttr, _ := pageDef.Attr("handler")
	if handlerAttr != nil {
		if handler, ok = handlerAttr.(starlark.Callable); !ok {
			return fmt.Errorf("handler for page %s is not a function", pathStr)
		}
	}

	handlerFunc := a.createHandlerFunc(htmlFile, blockStr, handler)
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

		var pathStr, blockStr, methodStr string
		if pathStr, err = getStringAttr(fragmentDef, "path"); err != nil {
			return err
		}
		if methodStr, err = getStringAttr(fragmentDef, "method"); err != nil {
			return err
		}
		if blockStr, err = getStringAttr(fragmentDef, "block"); err != nil {
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
		handlerFunc := a.createHandlerFunc(htmlFile, blockStr, fragmentCallback)

		fragmentPath := path.Join(pagePath, pathStr)
		a.Trace().Msgf("Adding fragment route %s <%s>", methodStr, fragmentPath)
		router.Method(methodStr, fragmentPath, handlerFunc)
	}

	return nil
}

func (a *App) createInternalRoutes(router *chi.Mux) error {
	if a.IsDev || a.AutoReload || a.Config.Routing.PushEvents {
		router.Get(utils.APP_INTERNAL_URL_PREFIX+"/sse", a.sseHandler)
	}

	return nil
}

func (a *App) createHandlerFunc(html, block string, handler starlark.Callable) http.HandlerFunc {
	goHandler := func(w http.ResponseWriter, r *http.Request) {
		thread := &starlark.Thread{
			Name:  a.Path,
			Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
		}

		isHtmxRequest := r.Header.Get("HX-Request") == "true" && !(r.Header.Get("HX-Boosted") == "true")
		appPath := a.Path
		if appPath == "/" {
			appPath = ""
		}
		pagePath := r.URL.Path
		if pagePath == "/" {
			pagePath = ""
		}
		requestData := Request{
			AppName:     a.Name,
			AppPath:     appPath,
			AppUrl:      fmt.Sprintf("%s://%s/%s", r.URL.Scheme, r.URL.Host, appPath),
			PagePath:    pagePath,
			PageUrl:     fmt.Sprintf("%s://%s/%s", r.URL.Scheme, r.URL.Host, pagePath),
			Method:      r.Method,
			IsDev:       a.IsDev,
			AutoReload:  a.AutoReload,
			IsPartial:   isHtmxRequest,
			PushEvents:  a.Config.Routing.PushEvents,
			HtmxVersion: a.Config.Htmx.Version,
			Headers:     r.Header,
			RemoteIP:    getRemoteIP(r),
		}

		chiContext := chi.RouteContext(r.Context())
		params := map[string]string{}
		if chiContext != nil && chiContext.URLParams.Keys != nil {
			for i, k := range chiContext.URLParams.Keys {
				params[k] = chiContext.URLParams.Values[i]
			}
		}
		requestData.UrlParams = params

		r.ParseForm()
		requestData.Form = r.Form
		requestData.Query = r.URL.Query()
		requestData.PostForm = r.PostForm

		var handlerResponse any = map[string]any{} // no handler means empty Data map is passed into template
		if handler != nil {
			ret, err := starlark.Call(thread, handler, starlark.Tuple{requestData}, nil)
			if err != nil {
				a.Error().Err(err).Msg("error calling handler")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			retStruct, ok := ret.(*starlarkstruct.Struct)
			if ok {
				// Handle Redirect response
				url, err := getStringAttr(retStruct, "url")
				// starlark Type() is not implemented for structs, so we can't check the type
				// Looked at the mandatory properties to decide on type for now
				if err == nil {
					// Redirect type struct returned by handler
					code, _ := getIntAttr(retStruct, "code")
					a.Trace().Msgf("Redirecting to %s with code %d", url, code)
					http.Redirect(w, r, url, int(code))
					return
				}

				// response type struct returned by handler Instead of template defined in
				// the route, use the template specified in the response
				err, done := a.handleResponse(retStruct, w, requestData)
				if done {
					return
				}

				http.Error(w, fmt.Sprintf("require url for redirect %s", err), http.StatusInternalServerError)
				return
			}

			handlerResponse, err = utils.UnmarshalStarlark(ret)
			if err != nil {
				a.Error().Err(err).Msg("error converting response")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		requestData.Data = handlerResponse
		var err error
		if isHtmxRequest && block != "" {
			a.Trace().Msgf("Rendering block %s", block)
			err = a.template.ExecuteTemplate(w, block, requestData)
		} else {
			referrer := r.Header.Get("Referer")
			isUpdateRequest := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
			if !isHtmxRequest && isUpdateRequest && block != "" && referrer != "" {
				// If block is defined, and this is a non-GET request, then redirect to the referrer page
				// This handles the Post/Redirect/Get pattern required if HTMX is disabled
				a.Trace().Msgf("Redirecting to %s with code %d", referrer, http.StatusSeeOther)
				http.Redirect(w, r, referrer, http.StatusSeeOther)
				return
			} else {
				a.Trace().Msgf("Rendering page %s", html)
				err = a.template.ExecuteTemplate(w, html, requestData)
			}
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	return goHandler
}

func (a *App) handleResponse(retStruct *starlarkstruct.Struct, w http.ResponseWriter, requestData Request) (error, bool) {
	templateBlock, err := getStringAttr(retStruct, "block")
	if err != nil || templateBlock == "" {
		return err, false
	}

	data, err := retStruct.Attr("data")
	if err != nil {
		a.Error().Err(err).Msg("error getting data from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	code, err := getIntAttr(retStruct, "code")
	if err != nil {
		a.Error().Err(err).Msg("error getting code from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	retarget, err := getStringAttr(retStruct, "retarget")
	if err != nil {
		a.Error().Err(err).Msg("error getting retarget from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	reswap, err := getStringAttr(retStruct, "reswap")
	if err != nil {
		a.Error().Err(err).Msg("error getting reswap from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	templateValue, err := utils.UnmarshalStarlark(data)
	if err != nil {
		a.Error().Err(err).Msg("error converting response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	requestData.Data = templateValue
	if retarget != "" {
		w.Header().Add("HX-Retarget", retarget)
	}
	if reswap != "" {
		w.Header().Add("HX-Reswap", reswap)
	}

	w.WriteHeader(int(code))
	err = a.template.ExecuteTemplate(w, templateBlock, requestData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}
	return nil, true
}

func getRemoteIP(r *http.Request) string {
	remoteIP := r.Header.Get("X-Real-IP")
	if remoteIP == "" {
		remoteIP = r.Header.Get("X-Forwarded-For")
	}
	if remoteIP == "" && r.RemoteAddr != "" {
		if r.RemoteAddr[0] == '[' {
			// IPv6
			remoteIP = strings.Split(r.RemoteAddr, "]")[0][1:]
		} else {
			remoteIP = strings.Split(r.RemoteAddr, ":")[0]
		}
	}
	return remoteIP
}
