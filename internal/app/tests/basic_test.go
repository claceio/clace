// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/testutil"
)

func TestAppLoadError(t *testing.T) {
	logger := testutil.TestLogger()

	_, _, err := app.CreateTestApp(logger, map[string]string{
		"app.star":      ``,
		"index.go.html": `{{.}}`,
	})
	testutil.AssertErrorContains(t, err, "app not defined, check app.star")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star":      `app = 1`,
		"index.go.html": `{{.}}`,
	})
	testutil.AssertErrorContains(t, err, "app not of type clace.app in app.star")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star":      `app = clace.app()`,
		"index.go.html": `{{.}}`,
	})
	testutil.AssertErrorContains(t, err, "missing argument for name")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])
handler = 10`,
		"index.go.html": `{{.}}`,
	})
	testutil.AssertErrorContains(t, err, "handler is not a function")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", handler=10)])`,
		"index.go.html": `{{.}}`,
	})
	testutil.AssertErrorContains(t, err, "page: for parameter \"handler\": got int, want callable")
}

func TestAppPages(t *testing.T) {
	logger := testutil.TestLogger()

	_, _, err := app.CreateTestApp(logger, map[string]string{
		"app.star": `app = clace.app("testApp", pages = 2)`,
	})
	testutil.AssertErrorContains(t, err, "got int, want list")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star": `app = clace.app("testApp", pages = ["abc"])`,
	})
	testutil.AssertErrorContains(t, err, "pages entry 1 is not a struct")
}

func TestAppLoadSuccess(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}
		`,
		"index.go.html": `Template got {{ .Data.key }}.`,
	}
	a, _, err := app.CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config app.AppConfig

	json.Unmarshal([]byte(fileData[app.CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.9.2", config.Htmx.Version)
}

func TestAppLoadWithLockfile(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", html="t1.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl":     `Template got {{ .Data.key }}.`,
		app.CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
	}
	a, _, err := app.CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config app.AppConfig

	json.Unmarshal([]byte(fileData[app.CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppLoadWrongTemplate(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", html="t12.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl":     `Template got {{ .key }}.`,
		app.CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
	}
	a, _, err := app.CreateTestApp(logger, fileData)
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
	var config app.AppConfig

	json.Unmarshal([]byte(fileData[app.CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppHeaderCustom(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
		"index.go.html": `Template contents {{template "clace_gen.go.html"}}.`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
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
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "no such template \"clace_body\"")
}

func TestNoHandler(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])`,
		"index.go.html": `Template contents {{.Data}}.`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "Template contents map[]")
}

func TestFullData(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])`,
		"index.go.html": `Template contents {{.}}.`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	configRegex := regexp.MustCompile(` Config:[^ ]+`)
	replaced := configRegex.ReplaceAllString(response.Body.String(), " CONFIG")
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, replaced, "Template contents map[AutoReload:false CONFIG Data:map[] Form:map[] IsDev:true IsHtmx:false Name:testApp Path:/test PostForm:map[] Query:map[] Url:/test UrlParams:map[]]")
}

func TestAppHeaderDefaultWithBody(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
		"app.go.html": `{{block "clace_body" .}}ABC{{end}}`,
	}

	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
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
	</head>

	<body>
	  <h1> Clace: testApp</h1>
	  ABC
	</body>`

	testutil.AssertStringMatch(t, "body", want, response.Body.String())
}

func TestRedirect(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])
def handler(req):
	return clace.redirect("/new_url", code=302)`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 302, response.Code)
	testutil.AssertStringContains(t, response.Header().Get("Location"), "/new_url")

	// Test default code is 303
	fileData = map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])
def handler(req):
	return clace.redirect("/new_url")`,
	}
	a, _, err = app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request = httptest.NewRequest("GET", "/test", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 303, response.Code)
	testutil.AssertStringContains(t, response.Header().Get("Location"), "/new_url")
}

func TestPost(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/", method="POST")])
def handler(req):
	return clace.redirect("/new_url", code=302)`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 405, response.Code) // GET instead of POST

	request = httptest.NewRequest("POST", "/test", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 302, response.Code)
	testutil.AssertStringContains(t, response.Header().Get("Location"), "/new_url")
}
