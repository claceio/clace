// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http/httptest"
	"path"
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
var _ fs.ReadFileFS = (*AppTestFS)(nil)

func (f *AppTestFS) Open(name string) (fs.File, error) {
	return nil, nil // no-op
}

func (f *AppTestFS) ReadFile(name string) ([]byte, error) {
	data, ok := f.fileData[name]
	if !ok {
		return nil, fmt.Errorf("test data not found: %s", name)
	}
	return []byte(data), nil
}

func (f *AppTestFS) Glob(pattern string) ([]string, error) {
	matchedFiles := []string{}
	for name := range f.fileData {
		if matched, _ := path.Match(pattern, name); matched {
			matchedFiles = append(matchedFiles, name)
			break
		}
	}

	return matchedFiles, nil
}

func (f *AppTestFS) ParseFS(patterns ...string) (*template.Template, error) {
	return template.ParseFS(f, patterns...)
}

func TestAppLoadError(t *testing.T) {
	logger := testutil.TestLogger()
	a := NewApp(logger, createAppEntry("/test"))

	testFS := &AppTestFS{fileData: map[string]string{
		"app.star":      ``,
		"index.go.html": `{{.}}`,
	}}
	err := a.Initialize(testFS)
	testutil.AssertErrorContains(t, err, "app not defined, check app.star")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star":      `app = 1`,
		"index.go.html": `{{.}}`,
	}}
	err = a.Initialize(testFS)
	testutil.AssertErrorContains(t, err, "app not of type APP in app.star")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star":      `app = cl_app()`,
		"index.go.html": `{{.}}`,
	}}
	err = a.Initialize(testFS)
	testutil.AssertErrorContains(t, err, "missing argument for name")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star": `
app = cl_app("testApp", pages = [cl_page("/")])`,
		"index.go.html": `{{.}}`,
	}}
	err = a.Initialize(testFS)
	testutil.AssertErrorContains(t, err, "has no handler, and no app level default handler function is specified")
}

func TestAppLoadSuccess(t *testing.T) {
	logger := testutil.TestLogger()
	a := NewApp(logger, createAppEntry("/test"))

	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = cl_app("testApp", pages = [cl_page("/")])

def handler(req):
	return {"key": "myvalue"}
		`,
		"index.go.html": `Template got {{ .key }}.`,
	}}
	err := a.Initialize(testFS)
	if err != nil {
		t.Errorf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
}
