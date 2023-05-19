// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http/httptest"
	"path"
	"strings"
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

func (f *AppTestFS) Open(name string) (fs.File, error) {
	return nil, nil // no-op
}

func (f *AppTestFS) ReadFile(name string) ([]byte, error) {
	data, ok := f.fileData[name]
	if !ok {
		return nil, fs.ErrNotExist
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

func (f *AppTestFS) Write(name string, bytes []byte) error {
	f.fileData[name] = string(bytes)
	return nil
}

func TestAppLoadError(t *testing.T) {
	logger := testutil.TestLogger()

	testFS := &AppTestFS{fileData: map[string]string{
		"app.star":      ``,
		"index.go.html": `{{.}}`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	testutil.AssertErrorContains(t, err, "app not defined, check app.star")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star":      `app = 1`,
		"index.go.html": `{{.}}`,
	}}
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "app not of type clace.app in app.star")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star":      `app = clace.app()`,
		"index.go.html": `{{.}}`,
	}}
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "missing argument for name")

	testFS = &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])`,
		"index.go.html": `{{.}}`,
	}}
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "has no handler, and no app level default handler function is specified")
}

func TestAppLoadSuccess(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}
		`,
		"index.go.html": `Template got {{ .key }}.`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	if err != nil {
		t.Errorf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config AppConfig

	json.Unmarshal([]byte(testFS.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.9.2", config.Htmx.Version)
}

func TestAppLoadWithLockfile(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", html="t1.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl": `Template got {{ .key }}.`,
		CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	if err != nil {
		t.Errorf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config AppConfig

	json.Unmarshal([]byte(testFS.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppLoadWrongTemplate(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := &AppTestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", html="t12.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl": `Template got {{ .key }}.`,
		CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
	}}
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
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
	var config AppConfig

	json.Unmarshal([]byte(testFS.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}
