// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/app/util"
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
	testutil.AssertErrorContains(t, err, "app not of type ace.app in app.star")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star":      `app = ace.app()`,
		"index.go.html": `{{.}}`,
	})
	testutil.AssertErrorContains(t, err, "missing argument for name")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/")])
handler = 10`,
		"index.go.html": `{{.}}`,
	})
	testutil.AssertErrorContains(t, err, "handler is not a function")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", handler=10)])`,
		"index.go.html": `{{.}}`,
	})
	testutil.AssertErrorContains(t, err, "page: for parameter \"handler\": got int, want callable")
}

func TestAppPages(t *testing.T) {
	logger := testutil.TestLogger()

	_, _, err := app.CreateTestApp(logger, map[string]string{
		"app.star": `app = ace.app("testApp", pages = 2)`,
	})
	testutil.AssertErrorContains(t, err, "got int, want list")

	_, _, err = app.CreateTestApp(logger, map[string]string{
		"app.star": `app = ace.app("testApp", pages = ["abc"])`,
	})
	testutil.AssertErrorContains(t, err, "pages entry 1 is not a struct")
}

func TestAppLoadSuccess(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])

def handler(req):
	return {"key": "myvalue"}
		`,
		"index.go.html": `Template got {{ .Data.key }}.`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config util.AppConfig

	json.Unmarshal([]byte(fileData[util.CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.9.2", config.Htmx.Version)
}

func TestAppLoadWithLockfile(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", full="t1.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl":      `Template got {{ .Data.key }}.`,
		util.CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
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
	var config util.AppConfig

	json.Unmarshal([]byte(fileData[util.CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppLoadWrongTemplate(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", full="t12.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl":      `Template got {{ .key }}.`,
		util.CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
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
	var config util.AppConfig

	json.Unmarshal([]byte(fileData[util.CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppHeaderCustom(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])

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
	want := `Template contents <script src="/test/static/gen/lib/htmx-fd346e9c8639d4624893fc455f2407a09b418301736dd18ebbb07764637fb478.min.js"></script> .`
	fmt.Println(response.Body.String())
	testutil.AssertStringMatch(t, "body", want, response.Body.String())
}

func TestAppHeaderDefault(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/")])

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
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])`,
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
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])`,
		"index.go.html": `Template contents {{.}}.`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "Template contents testapp:/test:get.")
}

func TestFullDataRoot(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])`,
		"index.go.html": `Template contents {{.}}.`,
	}
	a, _, err := app.CreateTestAppRoot(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "Template contents testapp::get.")
}

func TestAppHeaderDefaultWithBody(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/")])

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
		<meta charset="utf-8" />
		<meta name="viewport" content="width=device-width, initial-scale=1" />
		<title>testApp</title>
		<script src="/test/static/gen/lib/htmx-fd346e9c8639d4624893fc455f2407a09b418301736dd18ebbb07764637fb478.min.js"></script>
		<script src="/test/static/gen/lib/sse-66dadc2c017a266e589ea23d6825f7806f75317056ef29a56e5da01ea312f6e5.js"></script>
		<div id="cl_reload_listener" hx-ext="sse"
		sse-connect="/test/_clace_app/sse" sse-swap="clace_reload"
		hx-trigger="sse:clace_reload"></div>
	<script>
		document .getElementById("cl_reload_listener") .addEventListener("sse:clace_reload",
			function (event) {
				location.reload();
			});
	</script>
	</head>

	<body>
	  <h1>Clace: testApp</h1>
	  ABC
	</body>
	</html>`

	testutil.AssertStringMatch(t, "body", want, response.Body.String())
}

func TestRedirect(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])
def handler(req):
	return ace.redirect("/new_url", code=302)`,
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
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])
def handler(req):
	return ace.redirect("/new_url")`,
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
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/", method="POST")])
def handler(req):
	return ace.redirect("/new_url", code=302)`,
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

func TestResponse(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])

def handler(req):
	return ace.response({"key": "myvalue"}, "testtmpl")`,
		"index.go.html": `Template. {{block "testtmpl" .}}ABC {{.Data.key}} {{end}}`,
	}
	a, _, err := app.CreateTestAppRoot(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "ABC myvalue")
}

func TestResponseRetarget(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/")])

def handler(req):
	return ace.response({"key": "myvalue"}, "testtmpl", code=500, retarget="#abc", reswap="outerHTML")`,
		"index.go.html": `Template. {{block "testtmpl" .}}ABC {{.Data.key}} {{end}}`,
	}
	a, _, err := app.CreateTestAppRoot(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "ABC myvalue")
	testutil.AssertEqualsString(t, "retarget", response.Header().Get("HX-Retarget"), "#abc")
	testutil.AssertEqualsString(t, "reswap", response.Header().Get("HX-Reswap"), "outerHTML")
}
