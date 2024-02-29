// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"io/fs"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"

	_ "github.com/claceio/clace/internal/app/store" // Register db plugin
	_ "github.com/claceio/clace/plugins"            // Register builtin plugins
)

func CreateDevModeTestApp(logger *utils.Logger, fileData map[string]string) (*app.App, *util.WorkFs, error) {
	return CreateTestAppInt(logger, "/test", fileData, true, nil, nil, nil)
}

func CreateTestApp(logger *utils.Logger, fileData map[string]string) (*app.App, *util.WorkFs, error) {
	return CreateTestAppInt(logger, "/test", fileData, false, nil, nil, nil)
}

func CreateTestAppRoot(logger *utils.Logger, fileData map[string]string) (*app.App, *util.WorkFs, error) {
	return CreateTestAppInt(logger, "/", fileData, false, nil, nil, nil)
}

func CreateTestAppPlugin(logger *utils.Logger, fileData map[string]string, plugins []string, permissions []utils.Permission, pluginConfig map[string]utils.PluginSettings) (*app.App, *util.WorkFs, error) {
	return CreateTestAppInt(logger, "/test", fileData, false, plugins, permissions, pluginConfig)
}

func CreateDevAppPlugin(logger *utils.Logger, fileData map[string]string, plugins []string, permissions []utils.Permission, pluginConfig map[string]utils.PluginSettings) (*app.App, *util.WorkFs, error) {
	return CreateTestAppInt(logger, "/test", fileData, true, plugins, permissions, pluginConfig)
}

func CreateTestAppInt(logger *utils.Logger, path string, fileData map[string]string, isDev bool, plugins []string, permissions []utils.Permission, pluginConfig map[string]utils.PluginSettings) (*app.App, *util.WorkFs, error) {
	systemConfig := utils.SystemConfig{TailwindCSSCommand: ""}
	var fs utils.ReadableFS
	if isDev {
		fs = &TestWriteFS{TestReadFS: &TestReadFS{fileData: fileData}}
	} else {
		fs = &TestReadFS{fileData: fileData}
	}
	sourceFS, err := util.NewSourceFs("", fs, isDev)
	if err != nil {
		return nil, nil, err
	}

	if plugins == nil {
		plugins = []string{}
	}
	if permissions == nil {
		permissions = []utils.Permission{}
	}

	if pluginConfig == nil {
		pluginConfig = map[string]utils.PluginSettings{}
	}

	metadata := utils.AppMetadata{
		Loads:       plugins,
		Permissions: permissions,
	}
	workFS := util.NewWorkFs("", &TestWriteFS{TestReadFS: &TestReadFS{fileData: map[string]string{}}})
	a := app.NewApp(sourceFS, workFS, logger,
		createTestAppEntry(path, isDev, metadata), &systemConfig, pluginConfig)
	err = a.Initialize()
	return a, workFS, err
}

func createTestAppEntry(path string, isDev bool, metadata utils.AppMetadata) *utils.AppEntry {
	return &utils.AppEntry{
		Id:        "app_prd_testapp",
		Path:      path,
		Domain:    "",
		SourceUrl: ".",
		IsDev:     isDev,
		Metadata:  metadata,
	}
}

type TestReadFS struct {
	fileData map[string]string
}

var _ utils.ReadableFS = (*TestReadFS)(nil)

type TestWriteFS struct {
	*TestReadFS
}

var _ utils.WritableFS = (*TestWriteFS)(nil)

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
	reader *strings.Reader
}

func CreateTestFile(name string, data string) *TestFile {
	reader := strings.NewReader(data)
	return &TestFile{name: name, data: data, reader: reader}
}

func (f *TestFile) Stat() (fs.FileInfo, error) {
	return &TestFileInfo{f}, nil
}

func (f *TestFile) Seek(offset int64, whence int) (int64, error) {
	return f.reader.Seek(offset, whence)
}

func (f *TestFile) Read(dst []byte) (int, error) {
	return f.reader.Read(dst)
}

func (f *TestFile) Close() error {
	return nil
}

func (f *TestReadFS) Open(name string) (fs.File, error) {
	name = strings.TrimPrefix(name, "/")
	if _, ok := f.fileData[name]; !ok {
		return nil, fs.ErrNotExist
	}

	return CreateTestFile(name, f.fileData[name]), nil
}

func (f *TestReadFS) ReadFile(name string) ([]byte, error) {
	name = strings.TrimPrefix(name, "/")
	data, ok := f.fileData[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(data), nil
}

func (f *TestReadFS) Glob(pattern string) ([]string, error) {
	matchedFiles := []string{}
	for name := range f.fileData {
		if matched, _ := path.Match(pattern, name); matched {
			matchedFiles = append(matchedFiles, name)
		}
	}

	return matchedFiles, nil
}

func (f *TestReadFS) ParseFS(funcMap template.FuncMap, patterns ...string) (*template.Template, error) {
	return template.New("clacetestapp").Funcs(funcMap).ParseFS(f, patterns...)
}

func (f *TestReadFS) Stat(name string) (fs.FileInfo, error) {
	name = strings.TrimPrefix(name, "/")
	if _, ok := f.fileData[name]; !ok {
		return nil, fs.ErrNotExist
	}

	file := CreateTestFile(name, f.fileData[name])
	return &TestFileInfo{file}, nil
}

func (d *TestReadFS) StaticFiles() []string {
	return []string{} // Not implemented for disk fs, used only in prod mode
}

func (f *TestReadFS) Reset() {
	// do nothing
}

func (f *TestWriteFS) Write(name string, bytes []byte) error {
	name = strings.TrimPrefix(name, "/")
	f.fileData[name] = string(bytes)
	return nil
}

func (f *TestWriteFS) Remove(name string) error {
	name = strings.TrimPrefix(name, "/")
	delete(f.fileData, name)
	return nil
}
