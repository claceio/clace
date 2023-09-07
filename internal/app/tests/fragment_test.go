// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"net/http/httptest"
	"testing"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/testutil"
)

func TestFragmentSuccess(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/abc",
	fragments=[clace.fragment("frag", "ff")]
)])

def handler(req):
	return {"key": "myvalue", "key2": "myvalue2"}
		`,
		"index.go.html": `Template main {{ .Data.key }}. {{ block "ff" . }} fragdata {{ .Data.key2 }} {{ end }}`,
	}
	a, _, err := app.CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/abc", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	fullHtml := `Template main myvalue.  fragdata myvalue2 `
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", fullHtml, response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc/frag", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	// With default http request to fragment url (no htmx headers, full html is returned)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", fullHtml, response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc", nil)
	response = httptest.NewRecorder()
	request.Header.Set("HX-Request", "true")
	a.ServeHTTP(response, request)
	// With htmx request to main url, full html is returned
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", fullHtml, response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc/frag", nil)
	response = httptest.NewRecorder()
	request.Header.Set("HX-Request", "true")
	a.ServeHTTP(response, request)
	// With htmx request to fragment url, partial html is returned
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", " fragdata myvalue2 ", response.Body.String())
}

func TestFragmentDifferentHandler(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(req):
	return {"key": "myvalue", "key2": "myvalue2"}
def handler2(req):
	return {"key": "myvalue3", "key2": "myvalue4"}

app = clace.app("testApp", custom_layout=True, pages = [clace.page("/abc",
	fragments=[clace.fragment("frag", "ff", handler=handler2)]
)])
		`,
		"index.go.html": `Template main {{ .Data.key }}. {{ block "ff" . }} fragdata {{ .Data.key2 }} {{ end }}`,
	}
	a, _, err := app.CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/abc", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	fullHtml := `Template main myvalue.  fragdata myvalue2 `
	fullHtml2 := `Template main myvalue3.  fragdata myvalue4 `
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", fullHtml, response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc/frag", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	// With default http request to fragment url (no htmx headers), full html2 is returned
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", fullHtml2, response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc", nil)
	response = httptest.NewRecorder()
	request.Header.Set("HX-Request", "true")
	a.ServeHTTP(response, request)
	// With htmx request to main url, full html is returned
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", fullHtml, response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc/frag", nil)
	response = httptest.NewRecorder()
	request.Header.Set("HX-Request", "true")
	a.ServeHTTP(response, request)
	// With htmx request to fragment url, partial html is returned
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", " fragdata myvalue4 ", response.Body.String())
}

func TestFragmentMulti(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(req):
	return {"key": "myvalue", "key2": "myvalue2"}
def handler2(req):
	return {"key": "myvalue3", "key2": "myvalue4"}

app = clace.app("testApp", custom_layout=True, pages = [clace.page("/abc",
	fragments=[clace.fragment("frag", "ff", handler=handler2), clace.fragment("frag2", "ff2", method="POST")]
)])
		`,
		"index.go.html": `Template main {{ .Data.key }}. {{ block "ff" . }} fragdata {{ .Data.key2 }} {{ end }}
		{{ block "ff2" . }} {{if contains "frag2" .Url}} {{.Url}} frag2data {{ end }} {{end}}`,
	}
	a, _, err := app.CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/abc", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	fullHtml := `Template main myvalue.  fragdata myvalue2 `
	fullHtml2 := `Template main myvalue3.  fragdata myvalue4 `
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "body", fullHtml, response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc/frag", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "body", fullHtml2, response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc/frag2", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 405, response.Code) // GET instead of POST

	request = httptest.NewRequest("POST", "/test/abc/frag2", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code) // GET instead of POST
	testutil.AssertStringMatch(t, "body", fullHtml+"/test/abc/frag2 frag2data", response.Body.String())

	request = httptest.NewRequest("GET", "/test/abc/frag", nil)
	request.Header.Set("HX-Request", "true")
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "body", " fragdata myvalue4 ", response.Body.String())

	request = httptest.NewRequest("POST", "/test/abc/frag2", nil)
	request.Header.Set("HX-Request", "true")
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "body", "/test/abc/frag2 frag2data", response.Body.String())
}

func TestFragmentErrors(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/abc",
	fragments=10
)])
		`,
		"index.go.html": `Template main {{ .Data.key }}. {{ block "ff" . }} fragdata {{ .Data.key2 }} {{ end }}`,
	}
	_, _, err := app.CreateTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "got int, want list")

	fileData = map[string]string{
		"app.star": `
def handler(req):
		return {"key": "myvalue", "key2": "myvalue2"}
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/abc",
	fragments=[10]
)])
		`,
		"index.go.html": `Template main {{ .Data.key }}. {{ block "ff" . }} fragdata {{ .Data.key2 }} {{ end }}`,
	}
	_, _, err = app.CreateTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "page 1 fragment 1 is not a struct")

	fileData = map[string]string{
		"app.star": `
def handler(req):
		return {"key": "myvalue", "key2": "myvalue2"}
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/abc",
	fragments=[clace.fragment("frag", abc="ff", handler=handler)]
)])
		`,
		"index.go.html": `Template main {{ .Data.key }}. {{ block "ff" . }} fragdata {{ .Data.key2 }} {{ end }}`,
	}
	_, _, err = app.CreateTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "unexpected keyword argument \"abc\"")

	fileData = map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/abc",
	fragments=[clace.fragment("frag", "ff", handler=10)]
)])
		`,
		"index.go.html": `Template main {{ .Data.key }}. {{ block "ff" . }} fragdata {{ .Data.key2 }} {{ end }}`,
	}
	_, _, err = app.CreateTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "for parameter \"handler\": got int, want callable")
}
