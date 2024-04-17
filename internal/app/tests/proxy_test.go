// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/claceio/clace/internal/testutil"
	"github.com/claceio/clace/internal/types"
)

func TestProxyBasics(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abc" {
			t.Fatalf("Invalid path %s", r.URL.Path)
		}
		io.WriteString(w, "test contents")
	}))

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": fmt.Sprintf(`
load("proxy.in", "proxy")

app = ace.app("testApp", pages = [ace.proxy("/", proxy.config("%s"))],
permissions=[
	ace.permission("proxy.in", "config"),
]
)`, testServer.URL),
	}

	a, _, err := CreateTestAppPlugin(logger, fileData, []string{"proxy.in"},
		[]types.Permission{
			{Plugin: "proxy.in", Method: "config"},
		}, map[string]types.PluginSettings{})
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/abc", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", "test contents", response.Body.String())
}

func TestProxyMultiPath(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pp/abc" {
			t.Fatalf("Invalid path %s", r.URL.Path)
		}
		io.WriteString(w, "test contents")
	}))

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": fmt.Sprintf(`
load("proxy.in", "proxy")

def handler(req):
    return "handler text"

app = ace.app("testApp", pages = [
	ace.page("/", type=ace.TEXT),
	ace.proxy("/pp", proxy.config("%s")),
	ace.page("/np", type=ace.TEXT)],
permissions=[
	ace.permission("proxy.in", "config"),
]
)`, testServer.URL),
	}

	a, _, err := CreateTestAppPlugin(logger, fileData, []string{"proxy.in"},
		[]types.Permission{
			{Plugin: "proxy.in", Method: "config"},
		}, map[string]types.PluginSettings{})
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", "handler text", response.Body.String())

	request = httptest.NewRequest("GET", "/test/pp/abc", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", "test contents", response.Body.String())

	request = httptest.NewRequest("GET", "/test/np", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", "handler text", response.Body.String())
}

func TestProxyPermsSuccess(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abc" {
			t.Fatalf("Invalid path %s", r.URL.Path)
		}
		io.WriteString(w, "test contents")
	}))

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": fmt.Sprintf(`
load("proxy.in", "proxy")

app = ace.app("testApp", pages = [ace.proxy("/", proxy.config("%s"))],
permissions=[
	ace.permission("proxy.in", "config", ["%s"]),
]
)`, testServer.URL, testServer.URL),
	}

	a, _, err := CreateTestAppPlugin(logger, fileData, []string{"proxy.in"},
		[]types.Permission{
			{Plugin: "proxy.in", Method: "config", Arguments: []string{testServer.URL}},
		}, map[string]types.PluginSettings{})
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/abc", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", "test contents", response.Body.String())
}

func TestProxyPermsFailure(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abc" {
			t.Fatalf("Invalid path %s", r.URL.Path)
		}
		io.WriteString(w, "test contents")
	}))

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": fmt.Sprintf(`
load("proxy.in", "proxy")

app = ace.app("testApp", pages = [ace.proxy("/", proxy.config("%s"))],
permissions=[
	ace.permission("proxy.in", "config", ["example.com"]),
]
)`, testServer.URL),
	}

	_, _, err := CreateTestAppPlugin(logger, fileData, []string{"proxy.in"},
		[]types.Permission{
			{Plugin: "proxy.in", Method: "config", Arguments: []string{"example.com"}},
		}, map[string]types.PluginSettings{})

	testutil.AssertErrorContains(t, err, "is not permitted to call proxy.in.config with argument 0 having value")
}
