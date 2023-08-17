// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/claceio/clace/internal/testutil"
	"github.com/claceio/clace/internal/utils"
)

func createAppEntry(path string) *utils.AppEntry {
	return &utils.AppEntry{
		Id:     "testApp",
		Path:   path,
		Domain: "",
		FsPath: ".",
	}
}

type AppTestFS struct {
	fileData map[string]string
}

var _ AppFS = (*AppTestFS)(nil)

func (f *AppTestFS) Open(name string) (fs.File, error) {
	return nil, nil // no-op
}

func (f *AppTestFS) ReadFile(name string) ([]byte, error) {
	data, ok := f.fileData[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(data), nil
}

func (f *AppTestFS) Glob(pattern string) ([]string, error) {
	matchedFiles := []string{}
	for name := range f.fileData {
		if matched, _ := path.Match(pattern, name); matched {
			matchedFiles = append(matchedFiles, name)
		}
	}

	return matchedFiles, nil
}

func (f *AppTestFS) ParseFS(patterns ...string) (*template.Template, error) {
	return template.ParseFS(f, patterns...)
}

func (f *AppTestFS) Write(name string, bytes []byte) error {
	f.fileData[name] = string(bytes)
	return nil
}

func TestAppLoadError(t *testing.T) {
	logger := testutil.TestLogger()

	testFS := &AppTestFS{fileData: map[string]string{
		"app.star":      ``,
		"index.go.html": `{{.}}`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	testutil.AssertErrorContains(t, err, "app not defined, check app.star")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star":      `app = 1`,
		"index.go.html": `{{.}}`,
	}}
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "app not of type clace.app in app.star")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star":      `app = clace.app()`,
		"index.go.html": `{{.}}`,
	}}
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "missing argument for name")

	testFS =
		&AppTestFS{fileData: map[string]string{
			"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])`,
			"index.go.html": `{{.}}`,
		}}
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "has no handler, and no app level default handler function is specified")
}

func TestAppPages(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `app = clace.app("testApp")`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	if err != nil {
		t.Errorf("Error %s", err)
	}
	testFS = &AppTestFS{fileData: map[string]string{
		"app.star": `app = clace.app("testApp", pages = 2)`,
	}}
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "got int, want list")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star": `app = clace.app("testApp", pages = ["abc"])`,
	}}
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "pages entry 0 is not a struct")
}

func TestAppLoadSuccess(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}
		`,
		"index.go.html": `Template got {{ .Data.key }}.`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	if err != nil {
		t.Errorf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config AppConfig

	json.Unmarshal([]byte(testFS.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.9.2", config.Htmx.Version)
}

func TestAppLoadWithLockfile(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", html="t1.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl": `Template got {{ .Data.key }}.`,
		CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	if err != nil {
		t.Errorf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config AppConfig

	json.Unmarshal([]byte(testFS.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppLoadWrongTemplate(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", html="t12.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl": `Template got {{ .key }}.`,
		CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	testutil.AssertEqualsString(t, "body",
		`html/template: "t12.tmpl" is undefined`,
		strings.TrimSpace(response.Body.String()))
	var config AppConfig

	json.Unmarshal([]byte(testFS.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppHeaderCustom(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
		"index.go.html": `Template contents {{template "clace_gen.go.html"}}.`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	a.IsDev = true
	err := a.Initialize()
	if err != nil {
		t.Errorf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	want := `Template contents <script src="https://unpkg.com/htmx.org@"></script> .`
	testutil.AssertStringMatch(t, "body", want, response.Body.String())
}

func TestAppHeaderDefault(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	a.IsDev = true
	err := a.Initialize()
	if err != nil {
		t.Errorf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "no such template \"clace_body\"")
}

func TestAppHeaderDefaultWithBody(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
		"app.go.html": `{{block "clace_body" .}}ABC{{end}}`}}

	a := NewApp(testFS, logger, createAppEntry("/test"))
	a.IsDev = true
	err := a.Initialize()
	if err != nil {
		t.Errorf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	want := `<!DOCTYPE html>
	<html lang="en">
	
	<head>
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>testApp</title> 
	</head>
	
	<body>
		<script src="https://unpkg.com/htmx.org@1.9.2"></script>
		<script src="https://unpkg.com/htmx.org/dist/ext/sse.js"></script>
		
		<div id="cl_reload_listener" hx-ext="sse" 
			sse-connect="/test/_clace/sse" sse-swap="clace_reload"
			hx-trigger="sse:clace_reload"></div>
		<script>
			document.getElementById('cl_reload_listener').addEventListener('sse:clace_reload',
				function (event) {
					location.reload();
				});
		</script>
	
	  <h1> Clace: testApp</h1>
	  ABC
	</body>`

	testutil.AssertStringMatch(t, "body", want, response.Body.String())
}
