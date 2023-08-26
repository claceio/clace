// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"time"

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

type TestFS struct {
	fileData map[string]string
}

var _ WritableFS = (*TestFS)(nil)

type TestFileInfo struct {
	f *TestFile
}

func (fi *TestFileInfo) Name() string {
	return fi.f.name
}

func (fi *TestFileInfo) Size() int64 {
	return int64(len(fi.f.data))
}
func (fi *TestFileInfo) Mode() fs.FileMode {
	return 0
}
func (fi *TestFileInfo) ModTime() time.Time {
	return time.Now()
}
func (fi *TestFileInfo) IsDir() bool {
	return false
}
func (fi *TestFileInfo) Sys() any {
	return nil
}

type TestFile struct {
	name string
	data string
}

func (f *TestFile) Stat() (fs.FileInfo, error) {
	return &TestFileInfo{f}, nil
}

func (f *TestFile) Read(dst []byte) (int, error) {
	cnt := copy(dst, f.data)

	var err error
	if cnt == len(f.data) {
		err = io.EOF
	}
	return int(cnt), err
}

func (f *TestFile) Close() error {
	return nil
}

func (f *TestFS) Open(name string) (fs.File, error) {
	name = strings.TrimPrefix(name, "/")
	if _, ok := f.fileData[name]; !ok {
		return nil, fs.ErrNotExist
	}

	return &TestFile{name: name, data: f.fileData[name]}, nil
}

func (f *TestFS) ReadFile(name string) ([]byte, error) {
	name = strings.TrimPrefix(name, "/")
	data, ok := f.fileData[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(data), nil
}

func (f *TestFS) Glob(pattern string) ([]string, error) {
	matchedFiles := []string{}
	for name := range f.fileData {
		if matched, _ := path.Match(pattern, name); matched {
			matchedFiles = append(matchedFiles, name)
		}
	}

	return matchedFiles, nil
}

func (f *TestFS) ParseFS(funcMap template.FuncMap, patterns ...string) (*template.Template, error) {
	return template.New("clacetestapp").Funcs(funcMap).ParseFS(f, patterns...)
}

func (f *TestFS) Write(name string, bytes []byte) error {
	name = strings.TrimPrefix(name, "/")
	f.fileData[name] = string(bytes)
	return nil
}

func TestAppLoadError(t *testing.T) {
	logger := testutil.TestLogger()

	testFS := NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star":      ``,
		"index.go.html": `{{.}}`,
	}})
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	testutil.AssertErrorContains(t, err, "app not defined, check app.star")

	testFS = NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star":      `app = 1`,
		"index.go.html": `{{.}}`,
	}})
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "app not of type clace.app in app.star")

	testFS = NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star":      `app = clace.app()`,
		"index.go.html": `{{.}}`,
	}})
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "missing argument for name")

	testFS =
		NewAppFS("", &TestFS{fileData: map[string]string{
			"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])`,
			"index.go.html": `{{.}}`,
		}})
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "has no handler, and no app level default handler function is specified")
}

func TestAppPages(t *testing.T) {
	logger := testutil.TestLogger()

	testFS := NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star": `app = clace.app("testApp", pages = 2)`,
	}})
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	testutil.AssertErrorContains(t, err, "got int, want list")

	testFS = NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star": `app = clace.app("testApp", pages = ["abc"])`,
	}})
	a = NewApp(testFS, logger, createAppEntry("/test"))
	err = a.Initialize()
	testutil.AssertErrorContains(t, err, "pages entry 0 is not a struct")
}

func TestAppLoadSuccess(t *testing.T) {
	logger := testutil.TestLogger()
	fs := &TestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}
		`,
		"index.go.html": `Template got {{ .Data.key }}.`,
	}}
	testFS := NewAppFS("", fs)
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config AppConfig

	json.Unmarshal([]byte(fs.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.9.2", config.Htmx.Version)
}

func TestAppLoadWithLockfile(t *testing.T) {
	logger := testutil.TestLogger()
	fs := &TestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", html="t1.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl": `Template got {{ .Data.key }}.`,
		CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
	}}
	testFS := NewAppFS("", fs)
	a := NewApp(testFS, logger, createAppEntry("/test"))
	err := a.Initialize()
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", `Template got myvalue.`, response.Body.String())
	var config AppConfig

	json.Unmarshal([]byte(fs.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppLoadWrongTemplate(t *testing.T) {
	logger := testutil.TestLogger()
	fs := &TestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/", html="t12.tmpl")]
	, settings={"routing": {"template_locations": ['./templates/*.tmpl']}})

def handler(req):
	return {"key": "myvalue"}`,
		"./templates/t1.tmpl": `Template got {{ .key }}.`,
		CONFIG_LOCK_FILE_NAME: `{ "htmx": { "version": "1.8" } }`,
	}}
	testFS := NewAppFS("", fs)
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

	json.Unmarshal([]byte(fs.fileData[CONFIG_LOCK_FILE_NAME]), &config)
	testutil.AssertEqualsString(t, "config", "1.8", config.Htmx.Version)
}

func TestAppHeaderCustom(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
		"index.go.html": `Template contents {{template "clace_gen.go.html"}}.`,
	}})
	a := NewApp(testFS, logger, createAppEntry("/test"))
	a.IsDev = true
	err := a.Initialize()
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	want := `Template contents <script src="https://unpkg.com/htmx.org@"></script> .`
	testutil.AssertStringMatch(t, "body", want, response.Body.String())
}

func TestAppHeaderDefault(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
	}})
	a := NewApp(testFS, logger, createAppEntry("/test"))
	a.IsDev = true
	err := a.Initialize()
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()

	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "no such template \"clace_body\"")
}

func TestAppHeaderDefaultWithBody(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
		"app.go.html": `{{block "clace_body" .}}ABC{{end}}`}})

	a := NewApp(testFS, logger, createAppEntry("/test"))
	a.IsDev = true
	err := a.Initialize()
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
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>testApp</title> 
	</head>
	
	<body>
		<script src="https://unpkg.com/htmx.org@1.9.2"></script>
		<script src="https://unpkg.com/htmx.org/dist/ext/sse.js"></script>
		
		<div id="cl_reload_listener" hx-ext="sse" 
			sse-connect="/test/_clace/sse" sse-swap="clace_reload"
			hx-trigger="sse:clace_reload"></div>
		<script>
			document.getElementById('cl_reload_listener').addEventListener('sse:clace_reload',
				function (event) {
					location.reload();
				});
		</script>
	
	  <h1> Clace: testApp</h1>
	  ABC
	</body>`

	testutil.AssertStringMatch(t, "body", want, response.Body.String())
}

func TestStaticLoad(t *testing.T) {
	logger := testutil.TestLogger()
	testFS := NewAppFS("", &TestFS{fileData: map[string]string{
		"app.star": `
app = clace.app("testApp", custom_layout=True, pages = [clace.page("/")])

def handler(req):
	return {"key": "myvalue"}`,
		"index.go.html":    `abc {{static "file1"}} def {{static "file2.txt"}}`,
		"static/file1":     `file1data`,
		"static/file2.txt": `file2data`}})

	a := NewApp(testFS, logger, createAppEntry("/test"))
	a.IsDev = true
	err := a.Initialize()
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
