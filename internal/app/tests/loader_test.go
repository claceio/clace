// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"net/http/httptest"
	"testing"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/testutil"
)

func TestLoadStarlark(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `load("test.star", "testpage")
app = ace.app("testApp", custom_layout=True, pages = testpage)`,
		"index.go.html": `Template contents {{.AppName}}.`,
		"test.star":     `testpage = [ace.page("/")]`,
	}
	a, _, err := app.CreateTestAppRoot(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "Template contents testApp.")
}

func TestLoadStarlarkMulti(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `load("test1.star", "testpages")
app = ace.app("testApp", custom_layout=True, pages = testpages)`,
		"index.go.html": `Template contents {{.AppName}}.`,
		"test1.star": `load ("test2.star", "mypage")
testpages = [mypage]`,
		"test2.star": `mypage = ace.page("/")`,
	}
	a, _, err := app.CreateTestAppRoot(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "Template contents testApp.")
}

func TestLoadStarlarkLoop(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `load("test1.star", "testpages")
app = ace.app("testApp", custom_layout=True, pages = testpages)`,
		"index.go.html": `Template contents {{.AppName}}.`,
		"test1.star": `load ("app.star", "mypage")
testpages = [mypage]`,
	}
	_, _, err := app.CreateTestAppRoot(logger, fileData)
	testutil.AssertErrorContains(t, err, "cycle in starlark load graph during load of test1.star")
}

func TestLoadStarlarkLoopRuntime(t *testing.T) {
	// The audit load and runtime loader work differently, this test is to ensure that the runtime loader
	// is also detecting a loop
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `load("test1.star", "testpages")
app = ace.app("testApp", custom_layout=True, pages = testpages)`,
		"index.go.html": `Template contents {{.AppName}}.`,
		"test1.star":    `testpages = [ace.page("/")]`,
	}
	a, _, err := app.CreateDevModeTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "Template contents testApp.")

	// Now add a loop in the graph
	fileData["test1.star"] = `load ("app.star", "mypage")`
	r, err := a.Reload(true, true)
	testutil.AssertErrorContains(t, err, "cycle in starlark load graph during load of test1.star")
	testutil.AssertEqualsBool(t, "reload", false, r) // reload should have failed
}

func TestLoadStarlarkError(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `load("test2.star", "testpage")
app = ace.app("testApp", custom_layout=True, pages = testpage)`,
		"index.go.html": `Template contents {{.AppName}}.`,
		"test.star":     `testpage = [ace.page("/")]`,
	}
	_, _, err := app.CreateTestAppRoot(logger, fileData)
	testutil.AssertErrorContains(t, err, "cannot load test2.star: file does not exist")
}
