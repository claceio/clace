// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/claceio/clace/internal/utils"
)

type DiskReadFS struct {
	*utils.Logger
	root string
	fs   fs.FS
}

var _ ReadableFS = (*DiskReadFS)(nil)

func NewDiskReadFS(logger *utils.Logger, root string) *DiskReadFS {
	return &DiskReadFS{
		Logger: logger,
		root:   root,
		fs:     os.DirFS(root),
	}
}

type DiskWriteFS struct {
	*DiskReadFS
}

// Open returns a reference to the named file.
func (d *DiskReadFS) Open(name string) (fs.File, error) {
	return d.fs.Open(name)
}

func (d *DiskReadFS) ReadFile(name string) ([]byte, error) {
	if dir, ok := d.fs.(fs.ReadFileFS); ok {
		return dir.ReadFile(name)
	}

	file, err := d.fs.Open(name)
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

func (d *DiskReadFS) makeAbsolute(name string) string {
	if !strings.HasPrefix(name, d.root) {
		name = path.Join(d.root, name)
	}
	return name
}

func (d *DiskReadFS) Stat(name string) (fs.FileInfo, error) {
	name = d.makeAbsolute(name)
	return os.Stat(name)
}

func (d *DiskReadFS) Glob(pattern string) (matches []string, err error) {
	return fs.Glob(d.fs, pattern)
}

func (d *DiskWriteFS) Write(name string, bytes []byte) error {
	name = d.makeAbsolute(name)
	dirName := path.Dir(name)
	if err := os.MkdirAll(dirName, 0700); err != nil {
		return fmt.Errorf("error creating directory %s : %s", dirName, err)
	}
	return os.WriteFile(name, bytes, 0600)
}

func (d *DiskWriteFS) Remove(name string) error {
	name = d.makeAbsolute(name)
	return os.Remove(name)
}
