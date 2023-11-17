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
	"path/filepath"
	"strings"

	"github.com/claceio/clace/internal/utils"
)

type DiskReadFS struct {
	*utils.Logger
	root string
	fs   fs.FS
}

var _ utils.ReadableFS = (*DiskReadFS)(nil)

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

func (d *DiskReadFS) makeAbsolute(name string) (string, error) {
	cleanRoot := filepath.Clean(d.root)

	if !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, d.root) && !strings.HasPrefix(name, cleanRoot) {
		name = path.Join(d.root, name)
	}

	return name, nil
}

func (d *DiskReadFS) Stat(name string) (fs.FileInfo, error) {
	name, err := d.makeAbsolute(name)
	if err != nil {
		return nil, err
	}
	return os.Stat(name)
}

func (d *DiskReadFS) Glob(pattern string) (matches []string, err error) {
	return fs.Glob(d.fs, pattern)
}

func (d *DiskWriteFS) Write(name string, bytes []byte) error {
	name, err := d.makeAbsolute(name)
	if err != nil {
		return err
	}
	dirName := path.Dir(name)
	if err := os.MkdirAll(dirName, 0700); err != nil {
		return fmt.Errorf("error creating directory %s : %s", dirName, err)
	}
	return os.WriteFile(name, bytes, 0600)
}

func (d *DiskWriteFS) Remove(name string) error {
	name, err := d.makeAbsolute(name)
	if err != nil {
		return err
	}
	return os.Remove(name)
}
