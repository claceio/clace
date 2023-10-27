// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"bytes"
	"io/fs"
	"path"
	"time"

	"github.com/claceio/clace/internal/utils"
)

type DbFs struct {
	*utils.Logger
	fileStore *FileStore
	fileInfo  map[string]DbFileInfo
}

var _ utils.ReadableFS = (*DbFs)(nil)

func NewDbFs(logger *utils.Logger, fileStore *FileStore) (*DbFs, error) {
	fileInfo, error := fileStore.getFileInfo()
	if error != nil {
		return nil, error
	}
	return &DbFs{
		Logger:    logger,
		fileStore: fileStore,
		fileInfo:  fileInfo,
	}, nil
}

type DbFile struct {
	name   string
	data   []byte
	fi     DbFileInfo
	reader *bytes.Reader
}

var _ fs.File = (*DbFile)(nil)

func NewDBFile(name string, data []byte, fi DbFileInfo) *DbFile {
	reader := bytes.NewReader(data)
	return &DbFile{name: name, data: data, reader: reader, fi: fi}
}

func (f *DbFile) Seek(offset int64, whence int) (int64, error) {
	return f.reader.Seek(offset, whence)
}

func (f *DbFile) Name() string {
	return f.name
}

func (f *DbFile) Stat() (fs.FileInfo, error) {
	return &f.fi, nil
}

func (f *DbFile) Read(dst []byte) (int, error) {
	return f.reader.Read(dst)
}

func (f *DbFile) Close() error {
	return nil
}

type DbFileInfo struct {
	name    string
	len     int64
	sha     string
	modTime time.Time
}

var _ fs.FileInfo = (*DbFileInfo)(nil)

func (fi *DbFileInfo) Name() string {
	return fi.name
}

func (fi *DbFileInfo) Size() int64 {
	return fi.len
}
func (fi *DbFileInfo) Mode() fs.FileMode {
	return 0
}
func (fi *DbFileInfo) ModTime() time.Time {
	return fi.modTime
}
func (fi *DbFileInfo) IsDir() bool {
	return false
}
func (fi *DbFileInfo) Sys() any {
	return nil
}

func (d *DbFs) Open(name string) (fs.File, error) {
	fi, ok := d.fileInfo[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	fileBytes, err := d.fileStore.GetFileBySha(fi.sha)
	if err != nil {
		return nil, err
	}
	return NewDBFile(name, fileBytes, fi), nil
}

func (d *DbFs) ReadFile(name string) ([]byte, error) {
	fi, ok := d.fileInfo[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	fileBytes, err := d.fileStore.GetFileBySha(fi.sha)
	if err != nil {
		return nil, err
	}
	return fileBytes, err

}

func (d *DbFs) Stat(name string) (fs.FileInfo, error) {
	fi, ok := d.fileInfo[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &fi, nil
}

func (d *DbFs) Glob(pattern string) (matches []string, err error) {
	matchedFiles := []string{}
	for name, _ := range d.fileInfo {
		if matched, _ := path.Match(pattern, name); matched {
			matchedFiles = append(matchedFiles, name)
		}
	}

	return matchedFiles, nil
}
