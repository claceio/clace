// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package apptests

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/claceio/clace/internal/testutil"
)

func TestStylingNone(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
settings={"styling":{"library": ""}})

def handler(req):
	return {"key": "myvalue"}`,
	}

	a, _, err := createApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/static/gen/css/style.css", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "body", "", response.Body.String())
}

func TestStylingOther(t *testing.T) {
	// Create a test server to serve the css file
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "mystyle contents")
	}))
	testUrl := testServer.URL + "/static/mystyle.css"

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": fmt.Sprintf(`
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
settings={"styling":{"library": "%s"}})

def handler(req):
	return {"key": "myvalue"}`, testUrl),
		"static/mystyle.css": `mystyle contents`,
	}

	a, _, err := createApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/static/gen/css/style.css", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "body", "mystyle contents", response.Body.String())
}

func TestStylingTailwindCSS(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
settings={"styling":{"library": "tailwindcss"}})

def handler(req):
	return {"key": "myvalue"}`,
	}

	_, workFS, err := createApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	data, err := workFS.ReadFile("style/input.css")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "input.css", "@tailwind base; @tailwind components; @tailwind utilities;", string(data))

	data, err = workFS.ReadFile("style/tailwind.config.js")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "tailwind.config.js", `MODULE.EXPORTS = { CONTENT: ['*.HTML'], THEME: { EXTEND: {}, }, PLUGINS: [ REQUIRE('@TAILWINDCSS/FORMS'), REQUIRE('@TAILWINDCSS/TYPOGRAPHY') ], }`, string(data))
}

func TestStylingDaisyUI(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
settings={"styling":{"library": "daisyui"}})

def handler(req):
	return {"key": "myvalue"}`,
	}

	_, workFS, err := createApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	data, err := workFS.ReadFile("style/input.css")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "input.css", "@tailwind base; @tailwind components; @tailwind utilities;", string(data))

	data, err = workFS.ReadFile("style/tailwind.config.js")
	testutil.AssertNoError(t, err)
	testutil.AssertStringMatch(t, "tailwind.config.js", `MODULE.EXPORTS = { CONTENT: ['*.HTML'], THEME: { EXTEND: {}, }, PLUGINS: [ REQUIRE('@TAILWINDCSS/FORMS'), REQUIRE('@TAILWINDCSS/TYPOGRAPHY') , REQUIRE("daisyui") ], }`, string(data))
}

func TestStylingError(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")],
settings={"styling":{"library": "unknown"}})

def handler(req):
	return {"key": "myvalue"}`,
	}

	_, _, err := createApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "invalid styling library config : unknown")
}
