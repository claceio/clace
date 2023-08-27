// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package apptests

import (
	"net/http/httptest"
	"testing"

	"github.com/claceio/clace/internal/testutil"
)

func TestStaticLoad(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
		"index.go.html":    `abc {{static "file1"}} def {{static "file2.txt"}}`,
		"static/file1":     `file1data`,
		"static/file2.txt": `file2data`,
	}

	a, err := createDevModeApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	want := `abc /test/static/file1-ca9e40772ef9119c13100a8258bc38a665a0a1976bf81c96e69a353b6605f5a7 def /test/static/file2-d044e5b148745e322fe3e916e5f3bb9c9182892fdf99850baf4ed82c2864dd30.txt`
	testutil.AssertStringMatch(t, "body", want, response.Body.String())

	request = httptest.NewRequest("GET", "/test/static/file1-ca9e40772ef9119c13100a8258bc38a665a0a1976bf81c96e69a353b6605f5a7", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	want = `file1data`
	testutil.AssertStringMatch(t, "body", want, response.Body.String())

	request = httptest.NewRequest("GET", "/test/static/file2-d044e5b148745e322fe3e916e5f3bb9c9182892fdf99850baf4ed82c2864dd30.txt", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsString(t, "header cache", "public, max-age=31536000", response.Header().Get("Cache-Control"))
	testutil.AssertEqualsBool(t, "header etag", true, response.Header().Get("ETag") != "")

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	want = `file2data`
	testutil.AssertStringMatch(t, "body", want, response.Body.String())
	testutil.AssertEqualsString(t, "header", "public, max-age=31536000", response.Header().Get("Cache-Control"))
	testutil.AssertEqualsBool(t, "header etag", true, response.Header().Get("ETag") != "")
}
