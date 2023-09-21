// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/testutil"
)

func TestStyleNone(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
settings={"style":{"library": ""}})`,
	}

	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/static/gen/css/style.css", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "body", "", response.Body.String())
}

func TestStyleOther(t *testing.T) {
	// Create a test server to serve the css file
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "mystyle contents")
	}))
	testUrl := testServer.URL + "/static/mystyle.css"

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": fmt.Sprintf(`
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
			     style=clace.style("%s"))`, testUrl),
		"static/mystyle.css": `mystyle contents`,
	}

	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/static/gen/css/style.css", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "body", "mystyle contents", response.Body.String())
}

func TestStyleTailwindCSS(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
		        style=clace.style(library="tailwindcss"))`,
	}

	_, workFS, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	data, err := workFS.ReadFile("style/input.css")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "input.css", "@tailwind base; @tailwind components; @tailwind utilities;", string(data))

	data, err = workFS.ReadFile("style/tailwind.config.js")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "tailwind.config.js", `module.exports = { content: ['*.go.html'], theme: { extend: {}, }, plugins: [ ], }`, string(data))
}

func TestStyleDaisyUI(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
				style=clace.style(library="daisyui"))`,
	}

	_, workFS, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	data, err := workFS.ReadFile("style/input.css")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "input.css", "@tailwind base; @tailwind components; @tailwind utilities;", string(data))

	data, err = workFS.ReadFile("style/tailwind.config.js")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "tailwind.config.js", `module.exports = { content: ['*.go.html'], theme: { extend: {}, }, plugins: [ require("daisyui") ], }`, string(data))
}

func TestStyleDaisyUIThemes(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
				style=clace.style(library="daisyui", themes=["dark", "cupcake"]))`,
	}

	_, workFS, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	data, err := workFS.ReadFile("style/input.css")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "input.css", "@tailwind base; @tailwind components; @tailwind utilities;", string(data))

	data, err = workFS.ReadFile("style/tailwind.config.js")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "tailwind.config.js", `module.exports = { content: ['*.go.html'], theme: { extend: {}, }, plugins: [ require("daisyui") ], daisyui: { themes: ["dark", "cupcake"], }, }`, string(data))
}

func TestStyleCustom(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=False, pages = [clace.page("/")])`,
		"static/css/style.css": "body { background-color: red; }",
		"app.go.html":          `{{block "clace_body" .}}ABC{{end}}`,
	}

	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	// Since custom style static/css/style.css is present, that should be included in the header
	testutil.AssertStringContains(t, response.Body.String(),
		`<link rel="stylesheet" href=/test/static/css/style-ac05e05bbc5e5410e5c9e7531bbd20c45803d479bb10e5a6e9d3c61d40e3e811.css>`)
}

func TestStyleError(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
                style=clace.style(library="unknown"))`,
	}

	_, _, err := app.CreateDevModeTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "invalid style library config : unknown")
}
