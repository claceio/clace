// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strconv"

	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/app/starlark_type"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
)

//go:embed *.go.html static/*
var embedHtml embed.FS

// Action represents a single action that is exposed by the App. Actions
// provide a way to trigger app operations, with an auto-generated form UI
// and an API interface
type Action struct {
	name        string
	description string
	path        string
	run         starlark.Callable
	validate    starlark.Callable
	params      []apptype.AppParam
	paramValues map[string]string
	template    *template.Template
	pagePath    string
}

// NewAction creates a new action
func NewAction(name, description, apath string, run, validate starlark.Callable,
	params []apptype.AppParam, paramValues map[string]string, appPath string) (*Action, error) {
	tmpl, err := template.New("form").ParseFS(embedHtml, "*.go.html")
	if err != nil {
		return nil, err
	}

	slices.SortFunc(params, func(a, b apptype.AppParam) int {
		return a.Index - b.Index
	})

	return &Action{
		name:        name,
		description: description,
		path:        apath,
		run:         run,
		validate:    validate,
		params:      params,
		paramValues: paramValues,
		template:    tmpl,
		pagePath:    path.Join(appPath, apath),
	}, nil
}

func (a *Action) BuildRouter() (*chi.Mux, error) {
	fSys, err := fs.Sub(embedHtml, "static")
	if err != nil {
		return nil, err
	}
	staticServer := http.FileServer(http.FS(fSys))

	r := chi.NewRouter()
	r.Post("/", a.runHandler)
	r.Get("/", a.getForm)
	r.Handle("/static/*", http.StripPrefix(path.Join(a.pagePath, "/static/"), staticServer))
	return r, nil
}

func (a *Action) runHandler(w http.ResponseWriter, r *http.Request) {
	// Parse the form
	// Validate the form
	// Run the action
}

type ParamDef struct {
	Name          string
	Description   string
	Value         any
	InputType     string
	AllowedValues []string
}

func (a *Action) getForm(w http.ResponseWriter, r *http.Request) {
	params := make([]ParamDef, 0, len(a.params))
	for _, p := range a.params {
		param := ParamDef{
			Name:        p.Name,
			Description: p.Description,
		}

		value, ok := a.paramValues[p.Name]
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
			param.Value = boolValue
			param.InputType = "checkbox"
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
