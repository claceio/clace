// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
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
	fi     DbFileInfo
	reader *DbFileReader
}

var _ fs.File = (*DbFile)(nil)

func NewDBFile(name string, compressionType string, data []byte, fi DbFileInfo) *DbFile {
	reader := NewbFileReader(compressionType, data)
	return &DbFile{name: name, fi: fi, reader: reader}
}

func (f *DbFile) Read(dst []byte) (int, error) {
	return f.reader.Read(dst)
}

func (f *DbFile) Name() string {
	return f.name
}

func (f *DbFile) Stat() (fs.FileInfo, error) {
	return &f.fi, nil
}

func (f *DbFile) Seek(offset int64, whence int) (int64, error) {
	// Seek is called by http.ServeContent in source_fs for the unoptimized case only
	// The data is decompressed and then recompressed if required in the unoptimized case
	return f.reader.Seek(offset, whence)
}

func (f *DbFile) ReadCompressed() ([]byte, string, error) {
	return f.reader.ReadCompressed()
}

func (f *DbFile) Close() error {
	return nil
}

type DbFileReader struct {
	compressionType    string
	compressedReader   *bytes.Reader
	uncompressedReader *bytes.Reader
}

var _ io.ReadSeeker = (*DbFileReader)(nil)
var _ utils.CompressedReader = (*DbFileReader)(nil)

func NewbFileReader(compressionType string, data []byte) *DbFileReader {
	compressedReader := bytes.NewReader(data)
	return &DbFileReader{compressionType: compressionType, compressedReader: compressedReader}
}

func (f *DbFileReader) uncompress() error {
	if f.compressionType == "" {
		f.uncompressedReader = f.compressedReader
	} else if f.compressionType == "gzip" {
		gz, err := gzip.NewReader(f.compressedReader)
		if err != nil {
			return err
		}
		defer gz.Close()
		uncompressed, err := io.ReadAll(gz)
		if err != nil {
			return err
		}
		f.uncompressedReader = bytes.NewReader(uncompressed)
	} else {
		return fmt.Errorf("unsupported compression type: %s", f.compressionType)
	}
	return nil
}

func (f *DbFileReader) Seek(offset int64, whence int) (int64, error) {
	if f.uncompressedReader == nil {
		if err := f.uncompress(); err != nil {
			return 0, err
		}
	}

	return f.uncompressedReader.Seek(offset, whence)
}

func (f *DbFileReader) Read(dst []byte) (int, error) {
	if f.uncompressedReader == nil {
		if err := f.uncompress(); err != nil {
			return 0, err
		}
	}

	return f.uncompressedReader.Read(dst)
}

func (f *DbFileReader) ReadCompressed() ([]byte, string, error) {
	data, err := io.ReadAll(f.compressedReader)
	return data, f.compressionType, err
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
	fileBytes, compressionType, err := d.fileStore.GetFileBySha(fi.sha)
	if err != nil {
		return nil, err
	}
	return NewDBFile(name, compressionType, fileBytes, fi), nil
}

func (d *DbFs) ReadFile(name string) ([]byte, error) {
	fi, ok := d.fileInfo[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	fileBytes, compressionType, err := d.fileStore.GetFileBySha(fi.sha)
	if err != nil {
		return nil, err
	}
	if compressionType != "" {
		if compressionType != "gzip" {
			return nil, fmt.Errorf("unsupported compression type: %s", compressionType)
		}
		gz, err := gzip.NewReader(bytes.NewReader(fileBytes))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		fileBytes, err = io.ReadAll(gz)
		if err != nil {
			return nil, err
		}
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
