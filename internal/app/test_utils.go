// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"io"
	"io/fs"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/claceio/clace/internal/utils"
)

func CreateDevModeTestApp(logger *utils.Logger, fileData map[string]string) (*App, *AppFS, error) {
	return CreateTestAppInt(logger, "/test", fileData, true, false)
}

func CreateDevModeHashDisable(logger *utils.Logger, fileData map[string]string) (*App, *AppFS, error) {
	return CreateTestAppInt(logger, "/test", fileData, true, true)
}

func CreateTestApp(logger *utils.Logger, fileData map[string]string) (*App, *AppFS, error) {
	return CreateTestAppInt(logger, "/test", fileData, false, false)
}

func CreateTestAppRoot(logger *utils.Logger, fileData map[string]string) (*App, *AppFS, error) {
	return CreateTestAppInt(logger, "/", fileData, false, false)
}

func CreateTestAppInt(logger *utils.Logger, path string, fileData map[string]string, isDev bool, disableHash bool) (*App, *AppFS, error) {
	systemConfig := utils.SystemConfig{TailwindCSSCommand: "", DisableFileHashDevMode: disableHash}
	sourceFS := NewAppFS("", &TestFS{fileData: fileData}, isDev, &systemConfig)
	workFS := NewAppFS("", &TestFS{fileData: map[string]string{}}, isDev, &systemConfig)
	a := NewApp(sourceFS, workFS, logger, createTestAppEntry(path), &systemConfig)
	a.IsDev = isDev
	err := a.Initialize()
	return a, workFS, err
}

func createTestAppEntry(path string) *utils.AppEntry {
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
	name   string
	data   string
	reader io.Reader
}

func CreateTestFile(name string, data string) *TestFile {
	reader := strings.NewReader(data)
	return &TestFile{name: name, data: data, reader: reader}
}

func (f *TestFile) Stat() (fs.FileInfo, error) {
	return &TestFileInfo{f}, nil
}

func (f *TestFile) Read(dst []byte) (int, error) {
	return f.reader.Read(dst)
}

func (f *TestFile) Close() error {
	if r, ok := f.reader.(io.Closer); ok {
		return r.Close()
	}
	return nil
}

func (f *TestFS) Open(name string) (fs.File, error) {
	name = strings.TrimPrefix(name, "/")
	if _, ok := f.fileData[name]; !ok {
		return nil, fs.ErrNotExist
	}

	return CreateTestFile(name, f.fileData[name]), nil
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
