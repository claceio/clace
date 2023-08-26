// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// WritableFS is the interface for the writable underlying file system used by AppFS
type WritableFS interface {
	Write(name string, bytes []byte) error
}

// AppFS is the implementation of app file system
type AppFS struct {
	root string
	fs   fs.FS

	mu         sync.RWMutex
	nameToHash map[string]string    // lookup (path to hash path)
	hashToName map[string][2]string // reverse lookup (hash path to path)
}

func NewAppFS(dir string, fs fs.FS) *AppFS {
	return &AppFS{
		root: dir,
		fs:   fs,

		// File hashing code based on https://github.com/benbjohnson/hashfs/blob/main/hashfs.go
		nameToHash: make(map[string]string),
		hashToName: make(map[string][2]string)}
}

func (f *AppFS) ReadFile(name string) ([]byte, error) {
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

func (f *AppFS) Glob(pattern string) ([]string, error) {
	return fs.Glob(f.fs, pattern)
}

func (f *AppFS) ParseFS(funcMap template.FuncMap, patterns ...string) (*template.Template, error) {
	return template.New("claceapp").Funcs(funcMap).ParseFS(f.fs, patterns...)
}

func (f *AppFS) Write(name string, bytes []byte) error {
	target := path.Join(f.root, name)
	// If underlying FS implements Write, use that. Otherwise use os.Write
	if fs, ok := f.fs.(WritableFS); ok {
		return fs.Write(target, bytes)
	}
	return os.WriteFile(target, bytes, 0600)
}

// Open returns a reference to the named file.
// If name is a hash name then the underlying file is used.
func (f *AppFS) Open(name string) (fs.File, error) {
	fi, _, err := f.open(name)
	return fi, err
}

func (f *AppFS) open(name string) (_ fs.File, hash string, err error) {
	// Parse filename to see if it contains a hash.
	// If so, check if hash name matches.
	base, hash := f.ParseName(name)
	if hash != "" && f.HashName(base) == name {
		name = base
	}

	fi, err := f.fs.Open(name)
	return fi, hash, err
}

// HashName returns the hash name for a path, if exists.
// Otherwise returns the original path.
func (f *AppFS) HashName(name string) string {
	// Lookup cached formatted name, if exists.
	f.mu.RLock()
	if s := f.nameToHash[name]; s != "" {
		f.mu.RUnlock()
		return s
	}
	f.mu.RUnlock()

	// Read file contents. Return original filename if we receive an error.
	buf, err := fs.ReadFile(f.fs, name)
	if err != nil {
		return name
	}

	// Compute hash and build filename.
	hash := sha256.Sum256(buf)
	hashhex := hex.EncodeToString(hash[:])
	hashname := FormatName(name, hashhex)

	// Store in lookups.
	f.mu.Lock()
	f.nameToHash[name] = hashname
	f.hashToName[hashname] = [2]string{name, hashhex}
	f.mu.Unlock()

	return hashname
}

// FormatName returns a hash name that inserts hash before the filename's
// extension. If no extension exists on filename then the hash is appended.
// Returns blank string the original filename if hash is blank. Returns a blank
// string if the filename is blank.
func FormatName(filename, hash string) string {
	if filename == "" {
		return ""
	} else if hash == "" {
		return filename
	}

	dir, base := path.Split(filename)
	if i := strings.Index(base, "."); i != -1 {
		return path.Join(dir, fmt.Sprintf("%s-%s%s", base[:i], hash, base[i:]))
	}
	return path.Join(dir, fmt.Sprintf("%s-%s", base, hash))
}

// ParseName splits formatted hash filename into its base & hash components.
func (f *AppFS) ParseName(filename string) (base, hash string) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if hashed, ok := f.hashToName[filename]; ok {
		return hashed[0], hashed[1]
	}

	return ParseName(filename)
}

// ParseName splits formatted hash filename into its base & hash components.
func ParseName(filename string) (base, hash string) {
	if filename == "" {
		return "", ""
	}

	dir, base := path.Split(filename)

	// Extract pre-hash & extension.
	pre, ext := base, ""
	if i := strings.Index(base, "."); i != -1 {
		pre = base[:i]
		ext = base[i:]
	}

	// If prehash doesn't contain the hash, then exit.
	if !hashSuffixRegex.MatchString(pre) {
		return filename, ""
	}

	return path.Join(dir, pre[:len(pre)-65]+ext), pre[len(pre)-64:]
}

var hashSuffixRegex = regexp.MustCompile(`-[0-9a-f]{64}`)

// FileServer returns an http.Handler for serving FS files. It provides a
// simplified implementation of http.FileServer which is used to aggressively
// cache files on the client since the file hash is in the filename.
//
// Because FileServer is focused on small known path files, several features
// of http.FileServer have been removed including canonicalizing directories,
// defaulting index.html pages, precondition checks, & content range headers.
func FileServer(fsys *AppFS) http.Handler {
	return &fsHandler{fsys: fsys}
}

type fsHandler struct {
	fsys *AppFS
}

func (h *fsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean up filename based on URL path.
	filename := r.URL.Path
	if filename == "/" {
		filename = "."
	} else {
		filename = strings.TrimPrefix(filename, "/")
	}
	filename = path.Clean(filename)

	// Read file from attached file system.
	f, hash, err := h.fsys.open(filename)
	if os.IsNotExist(err) {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Fetch file info. Disallow directories from being displayed.
	fi, err := f.Stat()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	} else if fi.IsDir() {
		http.Error(w, "403 Forbidden", http.StatusForbidden)
		return
	}

	// Cache the file aggressively if the file contains a hash.
	if hash != "" {
		w.Header().Set("Cache-Control", `public, max-age=31536000`)
		w.Header().Set("ETag", "\""+hash+"\"")
	}

	// Flush header and write content.
	switch f := f.(type) {
	case io.ReadSeeker:
		http.ServeContent(w, r, filename, fi.ModTime(), f.(io.ReadSeeker))
	default:
		// Set content length.
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))

		// Flush header and write content.
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			io.Copy(w, f)
		}
	}
}
