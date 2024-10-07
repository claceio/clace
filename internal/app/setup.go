// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/claceio/clace/internal/app/action"
	"github.com/claceio/clace/internal/app/appfs"
	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/app/dev"
	"github.com/claceio/clace/internal/app/starlark_type"
	"github.com/claceio/clace/internal/types"
	"github.com/go-chi/chi"
	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func init() {
	resolve.AllowRecursion = true
}

func (a *App) loadStarlarkConfig(dryRun DryRun) error {
	a.Info().Str("path", a.Path).Str("domain", a.Domain).Msg("Loading app")

	buf, err := a.sourceFS.ReadFile(apptype.APP_FILE_NAME)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", apptype.APP_FILE_NAME, err)
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

	a.globals, err = starlark.ExecFile(thread, apptype.APP_FILE_NAME, buf, builtin)
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

	a.Name, err = apptype.GetStringAttr(a.appDef, "name")
	if err != nil {
		return err
	}
	a.CustomLayout, err = apptype.GetBoolAttr(a.appDef, "custom_layout")
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
		if converted, err = starlark_type.UnmarshalStarlark(dict); err != nil {
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
	if err = json.Unmarshal(jsonBuf.Bytes(), a.codeConfig); err != nil {
		return err
	}

	var stripAppPath bool
	if stripAppPath, err = a.checkAppPathStripping(); err != nil {
		return err
	}

	// Load container config. The proxy config in routes depends on this being loaded first
	if err = a.loadContainerManager(stripAppPath); err != nil {
		return err
	}

	if a.containerManager != nil {
		// Container manager is present, reload the container
		if a.IsDev {
			if err = a.containerManager.DevReload(bool(dryRun)); err != nil {
				return err
			}
		} else {
			if err := a.containerManager.ProdReload(bool(dryRun)); err != nil {
				return err
			}
		}
	}

	// Initialize the router configuration
	err = a.initRouter()
	if err != nil {
		return err
	}

	err = a.initActions()
	if err != nil {
		return err
	}

	return nil
}

func (a *App) createBuiltin() (starlark.StringDict, error) {
	builtin := apptype.CreateBuiltin()
	if builtin == nil {
		return nil, errors.New("error creating builtin")
	}

	var err error
	if builtin, err = a.addSchemaTypes(builtin); err != nil {
		return nil, err
	}

	if builtin, err = a.addParams(builtin); err != nil {
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
		tb := starlark_type.TypeBuilder{Name: t.Name, Fields: t.Fields}
		typeDict[t.Name] = starlark.NewBuiltin(t.Name, tb.CreateType)
	}

	docModule := starlarkstruct.Module{
		Name:    apptype.DOC_MODULE,
		Members: typeDict,
	}
	newBuiltins[apptype.DOC_MODULE] = &docModule

	// Add table module for referencing table names
	tableDict := starlark.StringDict{}
	for _, t := range a.storeInfo.Types {
		tableDict[t.Name] = starlark.String(t.Name)
	}

	tableModule := starlarkstruct.Module{
		Name:    apptype.TABLE_MODULE,
		Members: tableDict,
	}
	newBuiltins[apptype.TABLE_MODULE] = &tableModule

	return newBuiltins, nil
}

func (a *App) addParams(builtin starlark.StringDict) (starlark.StringDict, error) {
	if a.paramInfo == nil || len(a.paramInfo) == 0 {
		return builtin, nil
	}
	a.paramMap = make(map[string]string)

	// Create a copy of the builtins, don't modify the original
	newBuiltins := starlark.StringDict{}
	for k, v := range builtin {
		newBuiltins[k] = v
	}

	// Add param module for referencing param values
	paramDict := starlark.StringDict{}
	for _, p := range a.paramInfo {
		paramDict[p.Name] = p.DefaultValue

		if p.DefaultValue != starlark.None {
			switch p.Type {
			// Set the default value in the paramMap (in the string format)
			case starlark_type.STRING:
				a.paramMap[p.Name] = string(p.DefaultValue.(starlark.String))
			case starlark_type.INT:
				intVal, ok := p.DefaultValue.(starlark.Int).Int64()
				if !ok {
					return nil, fmt.Errorf("param %s is not an int", p.Name)
				}
				a.paramMap[p.Name] = fmt.Sprintf("%d", intVal)
			case starlark_type.BOOLEAN:
				a.paramMap[p.Name] = strconv.FormatBool(bool(p.DefaultValue.(starlark.Bool)))
			case starlark_type.DICT:
			case starlark_type.LIST:
				val, err := starlark_type.UnmarshalStarlark(p.DefaultValue)
				if err != nil {
					return nil, err
				}
				jsonVal, err := json.Marshal(val)
				if err != nil {
					return nil, err
				}
				a.paramMap[p.Name] = string(jsonVal)
			}
		}

		valueStr, ok := a.Metadata.ParamValues[p.Name]
		if !ok {
			// no custom value specified
			if p.Required && p.DefaultValue == starlark.None {
				return nil, fmt.Errorf("param %s is a required param, a value has to be provided", p.Name)
			}
			continue
		}
		a.paramMap[p.Name] = valueStr // Update the paramMap with the custom value

		switch p.Type {
		case starlark_type.STRING:
			paramDict[p.Name] = starlark.String(valueStr)
			if p.Required && valueStr == "" {
				return nil, fmt.Errorf("param %s is a required param, value cannot be empty", p.Name)
			}
		case starlark_type.INT:
			intValue, err := strconv.Atoi(valueStr)
			if err != nil {
				return nil, fmt.Errorf("param %s is not an int", p.Name)
			}

			paramDict[p.Name] = starlark.MakeInt(intValue)
		case starlark_type.BOOLEAN:
			boolValue, err := strconv.ParseBool(valueStr)
			if err != nil {
				return nil, fmt.Errorf("param %s is not a boolean", p.Name)
			}
			paramDict[p.Name] = starlark.Bool(boolValue)
		case starlark_type.DICT:
			var dictValue map[string]any
			if err := json.Unmarshal([]byte(valueStr), &dictValue); err != nil {
				return nil, fmt.Errorf("param %s is not a json dict", p.Name)
			}

			dictVal, err := starlark_type.MarshalStarlark(dictValue)
			if err != nil {
				return nil, fmt.Errorf("param %s is not a starlark dict", p.Name)
			}
			paramDict[p.Name] = dictVal
		case starlark_type.LIST:
			var listValue []any
			if err := json.Unmarshal([]byte(valueStr), &listValue); err != nil {
				return nil, fmt.Errorf("param %s is not a json list", p.Name)
			}
			listVal, err := starlark_type.MarshalStarlark(listValue)
			if err != nil {
				return nil, fmt.Errorf("param %s is not a starlark list", p.Name)
			}
			paramDict[p.Name] = listVal
		default:
			return nil, fmt.Errorf("unknown type %s for param %s", p.Type, p.Name)
		}
	}

	paramModule := starlarkstruct.Module{
		Name:    apptype.PARAM_MODULE,
		Members: paramDict,
	}

	newBuiltins[apptype.PARAM_MODULE] = &paramModule

	for k, v := range a.Metadata.ParamValues {
		if _, ok := paramDict[k]; !ok {
			a.paramMap[k] = v // add additional param values to paramMap
		}
	}
	return newBuiltins, nil
}

func verifyConfig(globals starlark.StringDict) (*starlarkstruct.Struct, error) {
	if !globals.Has(apptype.APP_CONFIG_KEY) {
		return nil, fmt.Errorf("%s not defined, check %s, add '%s = ace.app(...)'", apptype.APP_CONFIG_KEY, apptype.APP_FILE_NAME, apptype.APP_CONFIG_KEY)
	}
	appDef, ok := globals[apptype.APP_CONFIG_KEY].(*starlarkstruct.Struct)
	if !ok {
		return nil, fmt.Errorf("%s not of type ace.app in %s", apptype.APP_CONFIG_KEY, apptype.APP_FILE_NAME)
	}
	return appDef, nil
}

// checkAppPathStripping checks if the app path should be stripped from the request path for container proxying
// This is required for container health checks.
func (a *App) checkAppPathStripping() (bool, error) {
	appPathStripping := true
	// Iterate through all the routes
	routes, err := a.appDef.Attr("routes")
	if err != nil {
		return false, err
	}

	var ok bool
	var routeList *starlark.List
	if routeList, ok = routes.(*starlark.List); !ok {
		return false, fmt.Errorf("routes is not a list")
	}

	iter := routeList.Iterate()
	var val starlark.Value
	var count int

	for iter.Next(&val) {
		count += 1
		var pageDef *starlarkstruct.Struct
		if pageDef, ok = val.(*starlarkstruct.Struct); !ok {
			return false, fmt.Errorf("routes entry %d is not a struct", count)
		}

		_, err := pageDef.Attr("config")
		if err == nil {
			// "config" is defined, this must be a proxy config instead of a page definition
			var configAttr starlark.HasAttrs
			if configAttr, err = getProxyConfig(count, pageDef); err != nil {
				return false, err
			}

			var urlValue starlark.Value
			if urlValue, err = configAttr.Attr("Url"); err != nil {
				return false, err
			}

			if urlValue.(starlark.String).GoString() != apptype.CONTAINER_URL {
				// Not proxying to container url, ignore
				continue
			}

			var stripAppValue starlark.Value
			if stripAppValue, err = configAttr.Attr("StripApp"); err != nil {
				return false, err
			}

			return bool(stripAppValue.(starlark.Bool)), nil
		}
	}

	return appPathStripping, nil
}

func (a *App) initRouter() error {
	var defaultHandler starlark.Callable
	if a.globals.Has(apptype.DEFAULT_HANDLER) {
		var ok bool
		defaultHandler, ok = a.globals[apptype.DEFAULT_HANDLER].(starlark.Callable)
		if !ok {
			return fmt.Errorf("%s is not a function", apptype.DEFAULT_HANDLER)
		}
	}

	if a.globals.Has(apptype.ERROR_HANDLER) {
		var ok bool
		a.errorHandler, ok = a.globals[apptype.ERROR_HANDLER].(starlark.Callable)
		if !ok {
			return fmt.Errorf("%s is not a function", apptype.ERROR_HANDLER)
		}
	}

	router := chi.NewRouter()
	if err := a.createInternalRoutes(router); err != nil {
		return err
	}

	// Iterate through all the routes
	routes, err := a.appDef.Attr("routes")
	if err != nil {
		return err
	}

	var ok bool
	var routeList *starlark.List
	if routeList, ok = routes.(*starlark.List); !ok {
		return fmt.Errorf("routes is not a list")
	}
	iter := routeList.Iterate()
	var val starlark.Value
	count := 0
	rootWildcard := false
	for iter.Next(&val) {
		count++

		var rootWildcardSet bool
		if rootWildcardSet, err = a.addRoute(count, router, val, defaultHandler); err != nil {
			return err
		}

		if rootWildcardSet {
			rootWildcard = true // Root wildcard path, static files are not served
		}
	}

	// Mount static dir
	if !rootWildcard {
		staticPattern := path.Join("/", "static", "*")
		router.Handle(staticPattern, http.StripPrefix(a.Path, appfs.FileServer(a.sourceFS)))
	}

	err = a.addStaticRoot(router)
	if err != nil {
		return fmt.Errorf("error adding static root : %w ", err)
	}

	a.appRouter = chi.NewRouter()
	a.Trace().Msgf("Mounting app %s at %s", a.Name, a.Path)
	a.appRouter.Mount(a.Path, router)

	return nil
}

func (a *App) initActions() error {
	actions, err := a.appDef.Attr("actions")
	if err != nil {
		return err
	}

	a.actions = make([]*action.Action, 0)
	if actions == nil {
		return nil
	}

	var ok bool
	var actionList *starlark.List
	if actionList, ok = actions.(*starlark.List); !ok {
		return fmt.Errorf("actions is not a list")
	}

	iter := actionList.Iterate()
	var val starlark.Value
	count := 0
	for iter.Next(&val) {
		count++

		if err = a.addAction(count, val); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) addAction(count int, val starlark.Value) error {
	var ok bool
	var actionDef *starlarkstruct.Struct

	if actionDef, ok = val.(*starlarkstruct.Struct); !ok {
		return fmt.Errorf("actions entry %d is not a struct", count)
	}

	var name, path, description string
	var run, validate starlark.Callable
	var err error
	if name, err = apptype.GetStringAttr(actionDef, "name"); err != nil {
		return err
	}
	if path, err = apptype.GetStringAttr(actionDef, "path"); err != nil {
		return err
	}
	if description, err = apptype.GetStringAttr(actionDef, "description"); err != nil {
		return err
	}
	if run, err = apptype.GetCallableAttr(actionDef, "run"); err != nil {
		return err
	}
	v, _ := actionDef.Attr("validate")
	if v != nil {
		if validate, err = apptype.GetCallableAttr(actionDef, "validate"); err != nil {
			return err
		}
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	action, err := action.NewAction(name, description, path, run, validate, slices.Collect(maps.Values(a.paramInfo)), a.paramMap, a.Path)
	if err != nil {
		return fmt.Errorf("error creating action %s: %w", name, err)
	}
	r, err := action.BuildRouter()
	if err != nil {
		return fmt.Errorf("error building router for action %s: %w", name, err)
	}
	a.appRouter.Mount(path, r)
	a.actions = append(a.actions, action)
	return nil
}

// addStaticRoot adds the static root directory contents to the router
// Files can be referenced by /<filename>, without /static or /static_root
func (a *App) addStaticRoot(router *chi.Mux) error {
	staticFiles := a.sourceFS.StaticFiles()
	for _, rootFile := range staticFiles {
		if !strings.HasPrefix(rootFile, "static_root/") {
			continue
		}
		rootFile := rootFile
		fileName := rootFile[len("static_root/"):]
		router.Get("/"+fileName, func(w http.ResponseWriter, r *http.Request) {
			f, err := a.sourceFS.Open(rootFile)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			seeker, ok := f.(io.ReadSeeker)
			if !ok {
				http.Error(w, "500 Filesystem does not implement Seek interface", http.StatusInternalServerError)
				return
			}

			fi, err := f.Stat()
			if err != nil {
				http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
				return
			} else if fi.IsDir() {
				http.Error(w, "Directory not supported in static root", http.StatusInternalServerError)
				return
			}

			http.ServeContent(w, r, fileName, fi.ModTime(), seeker)
		})

	}
	return nil
}

func (a *App) addRoute(count int, router *chi.Mux, routeVal starlark.Value, defaultHandler starlark.Callable) (bool, error) {
	var ok bool
	var err error
	var pageDef *starlarkstruct.Struct
	rootWildcard := false

	if pageDef, ok = routeVal.(*starlarkstruct.Struct); !ok {
		return rootWildcard, fmt.Errorf("routes entry %d is not a struct", count)
	}

	var pathStr, htmlFile, blockStr, methodStr string
	_, err = pageDef.Attr("config")
	if err == nil {
		// "config" is defined, this must be a proxy config instead of a page definition
		return a.addProxyConfig(count, router, pageDef)
	}

	_, err = pageDef.Attr("full")
	if err != nil {
		// "full" is not defined, this must be a API route instead of a html route
		return rootWildcard, a.addAPIRoute("", router, pageDef, defaultHandler)
	}

	if pathStr, err = apptype.GetStringAttr(pageDef, "path"); err != nil {
		return rootWildcard, err
	}
	if methodStr, err = apptype.GetStringAttr(pageDef, "method"); err != nil {
		return rootWildcard, err
	}
	if htmlFile, err = apptype.GetStringAttr(pageDef, "full"); err != nil {
		return rootWildcard, err
	}
	if blockStr, err = apptype.GetStringAttr(pageDef, "partial"); err != nil {
		return rootWildcard, err
	}

	a.usesHtmlTemplate = true
	if htmlFile == "" {
		if a.CustomLayout {
			htmlFile = apptype.INDEX_FILE
		} else {
			htmlFile = apptype.INDEX_GEN_FILE
		}
	}

	handler := defaultHandler // Use app level default handler, which could also be nil
	handlerAttr, _ := pageDef.Attr("handler")
	if handlerAttr != nil {
		if handler, ok = handlerAttr.(starlark.Callable); !ok {
			return rootWildcard, fmt.Errorf("handler for page %s is not a function", pathStr)
		}
	}

	handlerFunc := a.createHandlerFunc(htmlFile, blockStr, handler, apptype.HTML_TYPE)
	if err = a.handleFragments(router, pathStr, count, htmlFile, blockStr, pageDef, handler); err != nil {
		return rootWildcard, err
	}
	a.Trace().Msgf("Adding page route %s <%s>", methodStr, pathStr)
	router.Method(methodStr, pathStr, handlerFunc)
	return rootWildcard, nil
}

// getProxyConfig extracts the proxy config from the proxy definition
func getProxyConfig(count int, proxyDef *starlarkstruct.Struct) (starlark.HasAttrs, error) {
	var err error
	var pathStr string

	if pathStr, err = apptype.GetStringAttr(proxyDef, "path"); err != nil {
		return nil, err
	}

	var ok bool
	var responseAttr starlark.HasAttrs
	pluginResponse, err := proxyDef.Attr("config")
	if err != nil {
		return nil, err
	}
	if responseAttr, ok = pluginResponse.(starlark.HasAttrs); !ok {
		return nil, fmt.Errorf("proxy entry %d:%s is not a proxy response", count, pathStr)
	}

	errorValue, err := responseAttr.Attr("error")
	if err != nil {
		return nil, fmt.Errorf("error in proxy config: %w", err)
	}

	if errorValue != nil && errorValue != starlark.None {
		var errorString starlark.String
		if errorString, ok = errorValue.(starlark.String); !ok {
			return nil, fmt.Errorf("error in proxy config: %w", err)
		}

		if errorString.GoString() != "" {
			return nil, fmt.Errorf("error in proxy config: %s", errorString.GoString())
		}
	}

	config, err := responseAttr.Attr("value")
	if err != nil {
		return nil, err
	}

	if config.Type() != "ProxyConfig" {
		return nil, fmt.Errorf("proxy entry %d:%s is not a proxy config", count, pathStr)
	}

	var configAttr starlark.HasAttrs
	if configAttr, ok = config.(starlark.HasAttrs); !ok {
		return nil, fmt.Errorf("proxy entry %d:%s is not a proxy config attr", count, pathStr)
	}

	return configAttr, nil
}

func (a *App) addProxyConfig(count int, router *chi.Mux, proxyDef *starlarkstruct.Struct) (bool, error) {
	var err error
	var pathStr string
	rootWildcard := false

	if pathStr, err = apptype.GetStringAttr(proxyDef, "path"); err != nil {
		return rootWildcard, err
	}

	if pathStr == "/" {
		rootWildcard = true // Root wildcard path, static files are not served
	}

	var configAttr starlark.HasAttrs
	if configAttr, err = getProxyConfig(count, proxyDef); err != nil {
		return rootWildcard, err
	}

	var urlValue, stripPathValue starlark.Value
	if urlValue, err = configAttr.Attr("Url"); err != nil {
		return rootWildcard, err
	}
	if stripPathValue, err = configAttr.Attr("StripPath"); err != nil {
		return rootWildcard, err
	}
	var preserveHostValue starlark.Value
	if preserveHostValue, err = configAttr.Attr("PreserveHost"); err != nil {
		return rootWildcard, err
	}
	var stripAppValue starlark.Value
	if stripAppValue, err = configAttr.Attr("StripApp"); err != nil {
		return rootWildcard, err
	}
	urlStr := urlValue.(starlark.String).GoString()
	preserveHost := bool(preserveHostValue.(starlark.Bool))
	stripApp := bool(stripAppValue.(starlark.Bool))

	if urlStr == apptype.CONTAINER_URL {
		// proxying to container url
		if a.containerManager == nil {
			return rootWildcard, fmt.Errorf("container manager not initialized")
		}

		urlStr = a.containerManager.GetProxyUrl()
	}

	stripPathStr := stripPathValue.(starlark.String).GoString()
	url, err := url.Parse(urlStr)
	if err != nil {
		return rootWildcard, fmt.Errorf("error parsing url %s: %w", urlStr, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(url)

	customTransport := http.DefaultTransport.(*http.Transport).Clone()
	maxIdleConnCount := a.appConfig.Proxy.MaxIdleConns
	customTransport.MaxConnsPerHost = maxIdleConnCount * 2
	customTransport.MaxIdleConns = maxIdleConnCount
	customTransport.MaxIdleConnsPerHost = maxIdleConnCount
	customTransport.IdleConnTimeout = time.Duration(a.appConfig.Proxy.IdleConnTimeoutSecs) * time.Second
	customTransport.DisableCompression = a.appConfig.Proxy.DisableCompression
	proxy.Transport = customTransport

	defaultDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		defaultDirector(req)
		// To support WebSockets, we need to ensure that the `Connection`, `Upgrade`
		// and `Host` headers are forwarded as-is and not modified.
		if req.Header.Get("Upgrade") == "websocket" {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
		} else if !preserveHost {
			// Set the Host header to target url for non-WebSocket requests, unless
			// disabled in proxy config
			req.Host = url.Host
		}
	}

	permsHandler := func(p *httputil.ReverseProxy) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If write API, check if preview/stage app is allowed access
			isWriteReques := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete
			if isWriteReques {
				if strings.HasPrefix(string(a.Id), types.ID_PREFIX_APP_PREVIEW) && !a.Settings.PreviewWriteAccess {
					http.Error(w, "Preview app does not have access to proxy write APIs", http.StatusInternalServerError)
					return
				} else if strings.HasPrefix(string(a.Id), types.ID_PREFIX_APP_STAGE) && !a.Settings.StageWriteAccess {
					http.Error(w, "Stage app does not have access to proxy write APIs", http.StatusInternalServerError)
					return
				}
			}

			r.Header.Set("X-Forwarded-Host", strings.SplitN(r.Host, ":", 1)[0])
			if r.TLS != nil {
				r.Header.Set("X-Forwarded-Proto", "https")
			} else {
				r.Header.Set("X-Forwarded-Proto", "http")
			}
			// use the reverse proxy to handle the request
			p.ServeHTTP(w, r)
		})
	}
	if stripApp {
		stripPathStr = path.Join(a.Path, stripPathStr)
	}
	router.Mount(pathStr, http.StripPrefix(stripPathStr, permsHandler(proxy)))
	return rootWildcard, nil
}

func (a *App) addAPIRoute(basePath string, router *chi.Mux, apiDef *starlarkstruct.Struct, defaultHandler starlark.Callable) error {
	var err error
	var pathStr, method, rtype string
	if pathStr, err = apptype.GetStringAttr(apiDef, "path"); err != nil {
		return err
	}

	if method, err = apptype.GetStringAttr(apiDef, "method"); err != nil {
		return err
	}

	if rtype, err = apptype.GetStringAttr(apiDef, "type"); err != nil {
		return err
	}

	var ok bool
	handler := defaultHandler // Use app level default handler, which could also be nil
	handlerAttr, _ := apiDef.Attr("handler")
	if handlerAttr != nil {
		if handler, ok = handlerAttr.(starlark.Callable); !ok {
			return fmt.Errorf("handler for API %s is not a function", pathStr)
		}
	}

	handlerFunc := a.createHandlerFunc("", "", handler, rtype)

	fullPath := pathStr
	if basePath != "" {
		fullPath = path.Join(basePath, pathStr)
	}
	router.Method(method, fullPath, handlerFunc)
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

		_, err = fragmentDef.Attr("partial")
		if err != nil {
			// "partial" is not defined, this must be a API route instead of a html route
			if err = a.addAPIRoute(pagePath, router, fragmentDef, handlerCallable); err != nil {
				return err
			}
			continue
		}

		var pathStr, blockStr, methodStr string
		if pathStr, err = apptype.GetStringAttr(fragmentDef, "path"); err != nil {
			return err
		}
		if methodStr, err = apptype.GetStringAttr(fragmentDef, "method"); err != nil {
			return err
		}
		if blockStr, err = apptype.GetStringAttr(fragmentDef, "partial"); err != nil {
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
		handlerFunc := a.createHandlerFunc(htmlFile, blockStr, fragmentCallback, apptype.HTML_TYPE)

		fragmentPath := path.Join(pagePath, pathStr)
		a.Trace().Msgf("Adding fragment route %s <%s>", methodStr, fragmentPath)
		router.Method(methodStr, fragmentPath, handlerFunc)
	}

	return nil
}

func (a *App) createInternalRoutes(router *chi.Mux) error {
	if a.IsDev || a.codeConfig.Routing.PushEvents {
		router.Get(types.APP_INTERNAL_URL_PREFIX+"/sse", a.sseHandler)
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
			if name, err = apptype.GetStringAttr(libStruct, "name"); err != nil {
				return nil, err
			}
			if version, err = apptype.GetStringAttr(libStruct, "version"); err != nil {
				return nil, err
			}
			if esbuildArgs, err = apptype.GetListStringAttr(libStruct, "args", true); err != nil {
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
