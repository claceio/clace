// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/claceio/clace/internal/utils"
)

type FileStore struct {
	appId   utils.AppId
	version int
	db      *sql.DB
}

func NewFileStore(appId utils.AppId, version int, metadata *Metadata) *FileStore {
	return &FileStore{appId: appId, version: version, db: metadata.db}
}

func (f *FileStore) AddAppVersion(version int, gitSha, gitBranch, commit_message string, checkoutDir string) error {
	if _, err := f.db.Exec(`insert into app_versions (appid, version, git_sha, git_branch, commit_message, create_time) values (?, ?, ?, ?, ?, datetime('now'))`, f.appId, version, gitSha, gitBranch, commit_message); err != nil {
		return err
	}

	var err error
	var insertFileStmt *sql.Stmt
	if insertFileStmt, err = f.db.Prepare(`insert or ignore into files (sha, compression_type, content, create_time) values (?, ?, ?, datetime('now'))`); err != nil {
		return err
	}

	var insertAppFileStmt *sql.Stmt
	if insertAppFileStmt, err = f.db.Prepare(`insert into app_files (appid, version, name, sha, uncompressed_size, create_time) values (?, ?, ?, ?, ?, datetime('now'))`); err != nil {
		return err
	}

	fsys := os.DirFS(checkoutDir)

	fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, inErr error) error {
		if inErr != nil {
			return fmt.Errorf("file walk on %s failed for path %s: %w", checkoutDir, path, inErr)
		}
		if d.IsDir() && path == ".git" {
			// Skip .git directory completely
			return fs.SkipDir
		}

		if d.IsDir() {
			// Ignore directory paths
			return nil
		}

		// Walk the file system, read one file at a time
		file, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		buf, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}

		// Use forward slash as path separator
		path = strings.ReplaceAll(path, "\\", "/")

		var byteBuf bytes.Buffer
		hash := sha256.Sum256(buf)
		hashHex := hex.EncodeToString(hash[:])
		compressionType := ""
		storeBuf := buf
		if len(buf) > 1024 {
			compressionType = "gzip"
			byteBuf.Reset()
			gz := gzip.NewWriter(&byteBuf)

			if _, err := gz.Write(buf); err != nil {
				gz.Close()
				return err
			}
			if err := gz.Close(); err != nil {
				return err
			}
			storeBuf = byteBuf.Bytes()
		}

		// Compressed data is written to files table. Ignore if sha is already present
		// File contents, if same across versions and also across apps, are shared
		if _, err = insertFileStmt.Exec(hashHex, compressionType, storeBuf); err != nil {
			return fmt.Errorf("error inserting file: %w", err)
		}

		if _, err := insertAppFileStmt.Exec(f.appId, version, path, hashHex, len(buf)); err != nil {
			return fmt.Errorf("error inserting app file: %w", err)
		}
		return nil
	})
	return nil
}

func (f *FileStore) GetFileBySha(sha string) ([]byte, error) {
	stmt, err := f.db.Prepare("SELECT compression_type , content FROM files where sha = ?")
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	row := stmt.QueryRow(sha)
	var compressionType string
	var content []byte
	if err := row.Scan(&compressionType, &content); err != nil {
		return nil, fmt.Errorf("error querying file table: %w", err)
	}

	retBytes := content
	if compressionType == "gzip" {
		gz, err := gzip.NewReader(bytes.NewReader(content))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		if retBytes, err = io.ReadAll(gz); err != nil {
			return nil, err
		}
	}

	return retBytes, nil
}

func (f *FileStore) getFileInfo() (map[string]DbFileInfo, error) {
	stmt, err := f.db.Prepare(`select name, sha, uncompressed_size, create_time from app_files where appid = ? and version = ?`)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	rows, err := stmt.Query(f.appId, f.version)
	if err != nil {
		return nil, fmt.Errorf("error querying files: %w", err)
	}
	fileInfo := make(map[string]DbFileInfo)
	for rows.Next() {
		var name, sha string
		var size int64
		var modTime time.Time
		err = rows.Scan(&name, &sha, &size, &modTime)
		if err != nil {
			return nil, fmt.Errorf("error querying files: %w", err)
		}
		fileInfo[name] = DbFileInfo{name: name, sha: sha, len: size, modTime: modTime}
	}

	return fileInfo, nil
}
