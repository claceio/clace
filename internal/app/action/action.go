// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/benbjohnson/hashfs"
	"github.com/claceio/clace/internal/app/appfs"
	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/app/starlark_type"
	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

//go:embed *.go.html astatic/*
var embedHtml embed.FS
var embedFS = hashfs.NewFS(embedHtml)

type ActionLink struct {
	Name string
	Path string
}

// Action represents a single action that is exposed by the App. Actions
// provide a way to trigger app operations, with an auto-generated form UI
// and an API interface
type Action struct {
	*types.Logger
	isDev             bool
	name              string
	description       string
	appPath           string
	run               starlark.Callable
	suggest           starlark.Callable
	params            []apptype.AppParam
	paramValuesStr    map[string]string
	paramDict         starlark.StringDict
	actionTemplate    *template.Template
	pagePath          string
	AppTemplate       *template.Template
	StyleType         types.StyleType
	LightTheme        string
	DarkTheme         string
	containerProxyUrl string
	hidden            map[string]bool // params which are not shown in the UI
	Links             []ActionLink    // links to other actions
}

// NewAction creates a new action
func NewAction(logger *types.Logger, sourceFS *appfs.SourceFs, isDev bool, name, description, apath string, run, suggest starlark.Callable,
	params []apptype.AppParam, paramValuesStr map[string]string, paramDict starlark.StringDict,
	appPath string, styleType types.StyleType, containerProxyUrl string, hidden []string) (*Action, error) {

	funcMap := system.GetFuncMap()

	funcMap["static"] = func(name string) string {
		fullPath := path.Join(appPath, sourceFS.HashName(name))
		return fullPath
	}

	funcMap["astatic"] = func(name string) string {
		fullPath := path.Join(appPath, embedFS.HashName(name))
		return fullPath
	}

	funcMap["fileNonEmpty"] = func(name string) bool {
		fi, err := sourceFS.Stat(name)
		if err != nil {
			return false
		}
		return fi.Size() > 0
	}

	tmpl, err := template.New("form").Funcs(funcMap).ParseFS(embedFS, "*.go.html")
	if err != nil {
		return nil, err
	}

	slices.SortFunc(params, func(a, b apptype.AppParam) int {
		return a.Index - b.Index
	})

	subLogger := logger.With().Str("action", name).Logger()
	appLogger := types.Logger{Logger: &subLogger}

	pagePath := path.Join(appPath, apath)
	if pagePath == "/" {
		pagePath = ""
	}

	hiddenParams := make(map[string]bool)
	for _, h := range hidden {
		hiddenParams[h] = true
	}

	return &Action{
		Logger:            &appLogger,
		isDev:             isDev,
		name:              name,
		description:       description,
		appPath:           appPath,
		pagePath:          pagePath,
		run:               run,
		suggest:           suggest,
		params:            params,
		paramValuesStr:    paramValuesStr,
		paramDict:         paramDict,
		actionTemplate:    tmpl,
		StyleType:         styleType,
		containerProxyUrl: containerProxyUrl,
		hidden:            hiddenParams,
		// Links, AppTemplate and Theme names are initialized later
	}, nil
}

func (a *Action) GetLink() ActionLink {
	return ActionLink{
		Name: a.name,
		Path: a.pagePath,
	}
}

// GetEmbeddedTemplates returns the embedded templates files
func GetEmbeddedTemplates() (map[string][]byte, error) {
	files, err := fs.Glob(embedFS, "*.go.html")
	if err != nil {
		return nil, err
	}

	templates := make(map[string][]byte)
	for _, file := range files {
		data, err := embedHtml.ReadFile(file)
		if err != nil {
			return nil, err
		}
		templates[file] = data
	}

	return templates, nil
}

func (a *Action) BuildRouter() (*chi.Mux, error) {
	r := chi.NewRouter()
	r.Post("/", a.runAction)
	r.Get("/", a.getForm)

	r.Handle("/astatic/*", http.StripPrefix(path.Join(a.pagePath), hashfs.FileServer(embedFS)))
	return r, nil
}

func (a *Action) runAction(w http.ResponseWriter, r *http.Request) {
	thread := &starlark.Thread{
		Name:  a.name,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
	}

	// Save the request context in the starlark thread local
	thread.SetLocal(types.TL_CONTEXT, r.Context())
	if a.containerProxyUrl != "" {
		thread.SetLocal(types.TL_CONTAINER_URL, a.containerProxyUrl)
	}
	isHtmxRequest := r.Header.Get("HX-Request") == "true"

	r.ParseForm()
	var err error
	dryRun := false
	dryRunStr := r.Form.Get("dry-run")
	if dryRunStr != "" {
		dryRun, err = strconv.ParseBool(dryRunStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid value for dry-run: %s", dryRunStr), http.StatusBadRequest)
			return
		}
	}

	deferredCleanup := func() error {
		// Check for any deferred cleanups
		err = RunDeferredCleanup(thread)
		if err != nil {
			a.Error().Err(err).Msg("error cleaning up plugins")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return err
		}
		return nil
	}

	defer deferredCleanup()

	args := starlark.StringDict{}
	// Make a copy of the app level param dict
	for k, v := range a.paramDict {
		args[k] = v
	}

	qsParams := url.Values{}

	// Update args with submitted form values
	for _, param := range a.params {
		formValue := r.Form.Get(param.Name)
		if formValue == "" {
			if param.Type == starlark_type.BOOLEAN {
				// Form does not submit unchecked checkboxes, set to false
				args[param.Name] = starlark.Bool(false)
				qsParams.Add(param.Name, "false")
			}
		} else {
			newVal, err := apptype.ParamStringToType(param.Name, param.Type, formValue)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			args[param.Name] = newVal
			qsParams.Add(param.Name, formValue)
		}
	}

	argsValue := Args{members: args}

	// Call the handler function
	var ret starlark.Value
	ret, err = starlark.Call(thread, a.run, starlark.Tuple{starlark.Bool(dryRun), &argsValue}, nil)

	if err == nil {
		pluginErrLocal := thread.Local(types.TL_PLUGIN_API_FAILED_ERROR)
		if pluginErrLocal != nil {
			pluginErr := pluginErrLocal.(error)
			a.Error().Err(pluginErr).Msg("handler had plugin API failure")
			err = pluginErr // handle as if the handler had returned an error
		}
	}

	if err != nil {
		a.Error().Err(err).Msg("error calling action run handler")

		firstFrame := ""
		if evalErr, ok := err.(*starlark.EvalError); ok {
			// Iterate through the CallFrame stack for debugging information
			for i, frame := range evalErr.CallStack {
				a.Warn().Msgf("Function: %s, Position: %s\n", frame.Name, frame.Pos)
				if i == 0 {
					firstFrame = fmt.Sprintf("Function %s, Position %s", frame.Name, frame.Pos)
				}
			}
		}

		msg := err.Error()
		if firstFrame != "" && a.isDev {
			msg = msg + " : " + firstFrame
		}

		// No err handler defined, abort
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	var valuesMap []map[string]any
	var valuesStr []string
	var status string
	var paramErrors map[string]any
	report := apptype.AUTO

	resultStruct, ok := ret.(*starlarkstruct.Struct)
	if ok {
		status, err = apptype.GetOptionalStringAttr(resultStruct, "status")
		if err != nil {
			http.Error(w, fmt.Sprintf("error getting result status: %s", err), http.StatusInternalServerError)
			return
		}

		valuesMap, err = apptype.GetListMapAttr(resultStruct, "values", true)
		if err != nil {
			valuesStr, err = apptype.GetListStringAttr(resultStruct, "values", true)
			if err != nil {
				http.Error(w, fmt.Sprintf("error getting result values, not a list of string or list of maps: %s", err), http.StatusInternalServerError)
				return
			}
		}

		paramErrors, err = apptype.GetDictAttr(resultStruct, "param_errors", true)
		if err != nil {
			http.Error(w, fmt.Sprintf("error getting result attr paramErrors: %s", err), http.StatusInternalServerError)
			return
		}

		report, err = apptype.GetOptionalStringAttr(resultStruct, "report")
		if err != nil {
			http.Error(w, fmt.Sprintf("error getting result report: %s", err), http.StatusInternalServerError)
			return
		}
	} else {
		// Not a result struct
		status = strings.Trim(ret.String(), "\"")
	}

	if deferredCleanup() != nil {
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pageInput := map[string]any{
		"name":        a.name,
		"description": a.description,
		"path":        a.pagePath,
		"lightTheme":  a.LightTheme,
		"darkTheme":   a.DarkTheme,
	}

	if !isHtmxRequest {
		err = a.actionTemplate.ExecuteTemplate(w, "header", pageInput)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Set the push URL for HTMX
		w.Header().Set("HX-Push-Url", a.pagePath+"?"+qsParams.Encode())
	}

	// Render the result message
	err = a.actionTemplate.ExecuteTemplate(w, "status", status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Render the param error messages, using HTMX OOB
	errorMsgs := map[string]string{}
	errorKeys := []string{}
	for _, param := range a.params {
		// "" error messages have to be sent to overwrite previous values in form UI
		if !strings.HasPrefix(param.Name, OPTIONS_PREFIX) {
			if paramErrors[param.Name] == nil {
				errorMsgs[param.Name] = ""
			} else {
				errorMsgs[param.Name] = fmt.Sprintf("%s", paramErrors[param.Name])
			}
			errorKeys = append(errorKeys, param.Name)
		}
	}

	slices.Sort(errorKeys)
	for _, paramName := range errorKeys {
		tv := struct {
			Name    string
			Message string
		}{
			Name:    paramName,
			Message: errorMsgs[paramName],
		}
		err = a.actionTemplate.ExecuteTemplate(w, "paramError", tv)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	err = a.renderResults(w, report, valuesMap, valuesStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !isHtmxRequest {
		err = a.actionTemplate.ExecuteTemplate(w, "footer", pageInput)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func (a *Action) renderResults(w http.ResponseWriter, report string, valuesMap []map[string]any, valuesStr []string) error {
	if report == apptype.AUTO {
		return a.renderResultsAuto(w, valuesMap, valuesStr)
	}

	switch report {
	case apptype.TABLE:
		return a.renderResultsTable(w, valuesMap)
	case apptype.TEXT:
		return a.renderResultsText(w, valuesStr)
	case apptype.JSON:
		return a.renderResultsJson(w, valuesMap)
	default:
		// Custom template being used for the results
		// Wrap the template output in a div with hx-swap-oob
		_, err := io.WriteString(w, `<div id="action_result" hx-swap-oob="true" hx-swap="outerHTML"> `)
		if err != nil {
			return err
		}
		var tmplErr error
		if len(valuesStr) > 0 {
			tmplErr = a.AppTemplate.ExecuteTemplate(w, report, valuesStr)
		} else {
			tmplErr = a.AppTemplate.ExecuteTemplate(w, report, valuesMap)
		}
		_, err = io.WriteString(w, ` </div>`)
		if err != nil {
			return err
		}

		return tmplErr
	}
}

func (a *Action) renderResultsAuto(w http.ResponseWriter, valuesMap []map[string]any, valuesStr []string) error {
	if len(valuesStr) > 0 {
		return a.renderResultsText(w, valuesStr)
	}

	if len(valuesMap) == 0 {
		return a.actionTemplate.ExecuteTemplate(w, "result-empty", nil)
	}

	if len(valuesMap) > 0 {
		firstRow := valuesMap[0]
		hasComplex := false
		for _, v := range firstRow {
			if v == nil {
				continue
			}
			switch v.(type) {
			case int:
			case string:
			case bool:
			default:
				hasComplex = true
			}
			if hasComplex {
				break
			}
		}

		if hasComplex {
			return a.renderResultsJson(w, valuesMap)
		}
		return a.renderResultsTable(w, valuesMap)
	}

	return nil
}

func (a *Action) renderResultsText(w http.ResponseWriter, valuesStr []string) error {
	// Render the result values, using HTMX OOB
	err := a.actionTemplate.ExecuteTemplate(w, "result-textarea", valuesStr)
	return err
}

func (a *Action) renderResultsTable(w http.ResponseWriter, valuesMap []map[string]any) error {
	if len(valuesMap) == 0 {
		return a.actionTemplate.ExecuteTemplate(w, "result-empty", nil)
	}
	firstRow := valuesMap[0]
	keys := make([]string, 0, len(firstRow))
	for k := range firstRow {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	values := make([][]string, 0, len(valuesMap))
	for _, row := range valuesMap {
		rowValues := make([]string, 0, len(keys))
		for _, k := range keys {
			v, ok := row[k]
			if !ok {
				// Missing value
				rowValues = append(rowValues, "")
			} else {
				pv := fmt.Sprintf("%v", v)
				DISPLAY_LIMIT := 100
				if len(pv) > DISPLAY_LIMIT {
					pv = pv[:DISPLAY_LIMIT] + "..."
				}
				rowValues = append(rowValues, pv)
			}
		}
		values = append(values, rowValues)
	}

	input := map[string]any{
		"Keys":   keys,
		"Values": values,
	}

	err := a.actionTemplate.ExecuteTemplate(w, "result-table", input)
	return err
}

func (a *Action) renderResultsJson(w http.ResponseWriter, valuesMap []map[string]any) error {
	err := a.actionTemplate.ExecuteTemplate(w, "result-json", valuesMap)
	return err
}

func RunDeferredCleanup(thread *starlark.Thread) error {
	deferMap := thread.Local(types.TL_DEFER_MAP)
	if deferMap == nil {
		return nil
	}

	strictFailures := []string{}
	for pluginName, pluginMap := range deferMap.(map[string]map[string]apptype.DeferEntry) {
		for key, entry := range pluginMap {
			err := entry.Func()
			if err != nil {
				fmt.Printf("error cleaning up %s %s: %s\n", pluginName, key, err)
			}
			if entry.Strict {
				strictFailures = append(strictFailures, fmt.Sprintf("%s:%s", pluginName, key))
			}
		}
	}

	thread.SetLocal(types.TL_DEFER_MAP, nil) // reset the defer map

	if len(strictFailures) > 0 {
		return fmt.Errorf("resource has not be closed, check handler code: %s", strings.Join(strictFailures, ", "))
	}

	return nil
}

type ParamDef struct {
	Name        string
	Description string
	Value       any
	InputType   string
	Options     []string
}

const (
	OPTIONS_PREFIX = "options-"
)

func (a *Action) getForm(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	params := make([]ParamDef, 0, len(a.params))

	options := make(map[string][]string)
	for _, p := range a.params {
		// params with options-x prefix are treated as select options for x
		if strings.HasPrefix(p.Name, OPTIONS_PREFIX) {
			name := p.Name[len(OPTIONS_PREFIX):]
			var vals []string
			err := json.Unmarshal([]byte(a.paramValuesStr[p.Name]), &vals)
			if err != nil {
				http.Error(w, fmt.Sprintf("invalid value for %s: %s", p.Name, a.paramValuesStr[p.Name]), http.StatusBadRequest)
				return
			}
			options[name] = vals
		}
	}

	for _, p := range a.params {
		if strings.HasPrefix(p.Name, OPTIONS_PREFIX) || a.hidden[p.Name] {
			continue
		}

		param := ParamDef{
			Name:        p.Name,
			Description: p.Description,
		}

		value, ok := a.paramValuesStr[p.Name]
		if !ok {
			http.Error(w, fmt.Sprintf("missing param value for %s", p.Name), http.StatusInternalServerError)
			return
		}

		qValue := queryParams.Get(p.Name)
		if qValue != "" {
			// Prefer value from query params
			value = qValue
		}

		param.Value = value // Default to string format
		param.InputType = "text"
		if p.Type == starlark_type.BOOLEAN {
			boolValue, err := strconv.ParseBool(value)
			if err != nil {
				http.Error(w, fmt.Sprintf("invalid value for %s: %s", p.Name, value), http.StatusInternalServerError)
				return
			}
			if boolValue {
				param.Value = "checked"
			}
			param.InputType = "checkbox"
		} else if options[p.Name] != nil {
			param.InputType = "select"
			param.Options = options[p.Name]
			param.Value = value
		}

		params = append(params, param)
	}

	linksWithQS := make([]ActionLink, 0, len(a.Links))
	for _, link := range a.Links {
		if link.Path != a.pagePath { // Don't add self link
			if r.URL.RawQuery != "" {
				link.Path = link.Path + "?" + r.URL.RawQuery
			}
			linksWithQS = append(linksWithQS, link)
		}
	}

	input := map[string]any{
		"dev":         a.isDev,
		"name":        a.name,
		"description": a.description,
		"appPath":     a.appPath,
		"pagePath":    a.pagePath,
		"params":      params,
		"styleType":   string(a.StyleType),
		"lightTheme":  a.LightTheme,
		"darkTheme":   a.DarkTheme,
		"links":       linksWithQS,
	}
	err := a.actionTemplate.ExecuteTemplate(w, "form.go.html", input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
