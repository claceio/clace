// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/app/starlark_type"
	"github.com/claceio/clace/internal/types"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

//go:embed *.go.html astatic/*
var embedHtml embed.FS

// Action represents a single action that is exposed by the App. Actions
// provide a way to trigger app operations, with an auto-generated form UI
// and an API interface
type Action struct {
	*types.Logger
	isDev          bool
	name           string
	description    string
	path           string
	run            starlark.Callable
	suggest        starlark.Callable
	params         []apptype.AppParam
	paramValuesStr map[string]string
	paramDict      starlark.StringDict
	template       *template.Template
	pagePath       string
}

// NewAction creates a new action
func NewAction(logger *types.Logger, isDev bool, name, description, apath string, run, suggest starlark.Callable,
	params []apptype.AppParam, paramValuesStr map[string]string, paramDict starlark.StringDict, appPath string) (*Action, error) {
	tmpl, err := template.New("form").ParseFS(embedHtml, "*.go.html")
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

	return &Action{
		Logger:         &appLogger,
		name:           name,
		description:    description,
		path:           apath,
		run:            run,
		suggest:        suggest,
		params:         params,
		paramValuesStr: paramValuesStr,
		paramDict:      paramDict,
		template:       tmpl,
		pagePath:       pagePath,
	}, nil
}

func (a *Action) BuildRouter() (*chi.Mux, error) {
	fSys, err := fs.Sub(embedHtml, "astatic")
	if err != nil {
		return nil, err
	}
	staticServer := http.FileServer(http.FS(fSys))

	r := chi.NewRouter()
	r.Post("/", a.runAction)
	r.Get("/", a.getForm)
	r.Handle("/astatic/*", http.StripPrefix(path.Join(a.pagePath, "/astatic/"), staticServer))
	return r, nil
}

func (a *Action) runAction(w http.ResponseWriter, r *http.Request) {
	thread := &starlark.Thread{
		Name:  a.name,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
	}

	// Save the request context in the starlark thread local
	thread.SetLocal(types.TL_CONTEXT, r.Context())
	//isHtmxRequest := r.Header.Get("HX-Request") == "true" && !(r.Header.Get("HX-Boosted") == "true")

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

	// Update args with submitted form values
	for _, param := range a.params {
		formValue := r.Form.Get(param.Name)
		if formValue == "" {
			if param.Type == starlark_type.BOOLEAN {
				// Form does not submit unchecked checkboxes, set to false
				args[param.Name] = starlark.Bool(false)
			}
		} else {
			newVal, err := apptype.ParamStringToType(param.Name, param.Type, formValue)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			args[param.Name] = newVal
		}
	}

	argsValue := Args{members: args}

	// Call the handler function
	var ret starlark.Value
	ret, err = starlark.Call(thread, a.run, starlark.Tuple{starlark.Bool(dryRun), &argsValue}, nil)
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
	var message string
	var paramErrors map[string]any

	resultStruct, ok := ret.(*starlarkstruct.Struct)
	if ok {
		message, err = apptype.GetOptionalStringAttr(resultStruct, "message")
		if err != nil {
			http.Error(w, fmt.Sprintf("error getting result attr message: %s", err), http.StatusInternalServerError)
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
	} else {
		// Not a result struct
		message = ret.String()
	}

	a.Info().Msgf("action result message: %s valuesStr %s valuesMap %s paramErrors %s", message, valuesStr, valuesMap, paramErrors)

	if deferredCleanup() != nil {
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Render the result message
	err = a.template.ExecuteTemplate(w, "message", message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	// Render the param error messages if any, using HTMX OOB
	for paramName, paramError := range paramErrors {
		tv := struct {
			Name    string
			Message string
		}{
			Name:    paramName,
			Message: fmt.Sprintf("%s", paramError),
		}
		err = a.template.ExecuteTemplate(w, "paramError", tv)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	if len(valuesStr) > 0 {
		// Render the result values, using HTMX OOB
		err = a.template.ExecuteTemplate(w, "result-textarea", valuesStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

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
		if strings.HasPrefix(p.Name, OPTIONS_PREFIX) {
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
			param.Value = param.Options[0]
		}

		params = append(params, param)
	}

	input := map[string]any{
		"name":        a.name,
		"description": a.description,
		"path":        a.pagePath,
		"params":      params,
	}
	err := a.template.ExecuteTemplate(w, "form.go.html", input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
