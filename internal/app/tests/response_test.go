// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/claceio/clace/internal/testutil"
)

func TestRTypeBasic(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", custom_layout=True, pages = [ace.page("/", type="json")])

def handler(req):
	return {"a": "aval", "b": 1}`,
		"index.go.html": `Template. {{block "testtmpl" .}}ABC {{.Data.key}} {{end}}`,
	}
	a, _, err := CreateTestAppRoot(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "type", "application/json", response.Header().Get("Content-Type"))
	ret := make(map[string]any)
	json.NewDecoder(response.Body).Decode(&ret)
	testutil.AssertEqualsString(t, "a", ret["a"].(string), "aval")
	testutil.AssertEqualsInt(t, "b", int(ret["b"].(float64)), 1)
}

func TestRTypeNoTemplate(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", type="json")])

def handler(req):
	return {"a": "aval", "b": 1}`,
	}
	a, _, err := CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "type", "application/json", response.Header().Get("Content-Type"))
	ret := make(map[string]any)
	json.NewDecoder(response.Body).Decode(&ret)
	testutil.AssertEqualsString(t, "a", ret["a"].(string), "aval")
	testutil.AssertEqualsInt(t, "b", int(ret["b"].(float64)), 1)
}

func TestRTypeFragment(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", fragments=[ace.fragment("frag", type="json")])])

def handler(req):
	return {"a": "aval", "b": 1}`,
	}
	a, _, err := CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/frag", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "type", "application/json", response.Header().Get("Content-Type"))
	ret := make(map[string]any)
	json.NewDecoder(response.Body).Decode(&ret)
	testutil.AssertEqualsString(t, "a", ret["a"].(string), "aval")
	testutil.AssertEqualsInt(t, "b", int(ret["b"].(float64)), 1)
}

func TestRTypeResponse(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/")])

def handler(req):
	return ace.response({"a": "aval", "b": 1}, type="json")`,
	}
	a, _, err := CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "type", "application/json", response.Header().Get("Content-Type"))
	ret := make(map[string]any)
	json.NewDecoder(response.Body).Decode(&ret)
	testutil.AssertEqualsString(t, "a", ret["a"].(string), "aval")
	testutil.AssertEqualsInt(t, "b", int(ret["b"].(float64)), 1)
}

func TestRTypeResponseInherit(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", type="json")])

def handler(req):
	return ace.response({"a": "aval", "b": 1})`,
	}
	a, _, err := CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "type", "application/json", response.Header().Get("Content-Type"))
	ret := make(map[string]any)
	json.NewDecoder(response.Body).Decode(&ret)
	testutil.AssertEqualsString(t, "a", ret["a"].(string), "aval")
	testutil.AssertEqualsInt(t, "b", int(ret["b"].(float64)), 1)
}

func TestRTypeFragmentInherit(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", fragments=[ace.fragment("frag", type="json")])])

def handler(req):
	return ace.response({"a": "aval", "b": 1})`,
	}
	a, _, err := CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/frag", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "type", "application/json", response.Header().Get("Content-Type"))
	ret := make(map[string]any)
	json.NewDecoder(response.Body).Decode(&ret)
	testutil.AssertEqualsString(t, "a", ret["a"].(string), "aval")
	testutil.AssertEqualsInt(t, "b", int(ret["b"].(float64)), 1)
}

func TestRTypeResponseInvalid(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", fragments=[ace.fragment("frag")])])

def handler(req):
	return ace.response({"a": "aval", "b": 1})`,
	}
	a, _, err := CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/frag", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	testutil.AssertEqualsString(t, "error", "Error handling response: block not defined in response and type is not html\n", response.Body.String())
}

func TestRTypeInvalidType(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = ace.app("testApp", pages = [ace.page("/", fragments=[ace.fragment("frag", type="abc")])])

def handler(req):
	return ace.response({"a": "aval", "b": 1})`,
	}
	_, _, err := CreateDevModeTestApp(logger, fileData)
	testutil.AssertErrorContains(t, err, "invalid type specified : abc")
}
