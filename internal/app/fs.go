// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
)

type AppFS interface {
	fs.ReadFileFS
	fs.GlobFS
	ParseFS(patterns ...string) (*template.Template, error)
	Write(name string, bytes []byte) error
}

type AppFSImpl struct {
	root string
	fs   fs.FS
}

var _ AppFS = (*AppFSImpl)(nil)

func NewAppFSImpl(dir string) *AppFSImpl {
	return &AppFSImpl{
		root: dir,
		fs:   os.DirFS(dir)}
}

func (f *AppFSImpl) Open(file string) (fs.File, error) {
	return f.fs.Open(file)
}

func (f *AppFSImpl) ReadFile(name string) ([]byte, error) {
	if dir, ok := f.fs.(fs.ReadFileFS); ok {
		return dir.ReadFile(name)
	}

	file, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, file)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (f *AppFSImpl) Glob(pattern string) ([]string, error) {
	return fs.Glob(f.fs, pattern)
}

func (f *AppFSImpl) ParseFS(patterns ...string) (*template.Template, error) {
	return template.ParseFS(f.fs, patterns...)
}

func (f *AppFSImpl) Write(name string, bytes []byte) error {
	target := path.Join(f.root, name)
	return os.WriteFile(target, bytes, 0600)
}
