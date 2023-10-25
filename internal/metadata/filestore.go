// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
)

type FileStore struct {
	db *sql.DB
}

func NewFileStore(db *sql.DB) *FileStore {
	return &FileStore{db: db}
}

func (d *FileStore) initTables() error {
	if _, err := d.db.Exec(`create table files (sha text, context blob, create_time datetime, PRIMARY KEY(sha))`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`create table app_versions (appid text, version int, git_sha text, git_branch text, user_id text, notes text, metadata json, create_time datetime, PRIMARY KEY(appid, version))`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`create table app_files (appid text, version int, name text, sha text, compressed_size int, uncompressed_size int, create_time datetime, PRIMARY KEY(appid, version, name))`); err != nil {
		return err
	}

	return nil
}

func (d *FileStore) AddAppVersion(appId string, version int, gitSha, gitBranch string, files map[string][]byte) error {

	if _, err := d.db.Exec(`insert into app_versions (appid, version, git_sha, git_branch, create_time) values (?, ?, ?, ?, ?, ?, datetime('now'))`, appId, version, gitSha, gitBranch); err != nil {
		return err
	}

	var byteBuf bytes.Buffer
	for name, buf := range files {
		hash := sha256.Sum256(buf)
		hashHex := hex.EncodeToString(hash[:])
		byteBuf.Reset()
		gz := gzip.NewWriter(&byteBuf)

		if _, err := gz.Write(buf); err != nil {
			return err
		}
		if err := gz.Close(); err != nil {
			return err
		}

		// Compressed data is written to files table. Ignore if sha is already present
		// File content, if same across versions and also across apps, are shared
		if _, err := d.db.Exec(`insert or ignore into files (sha, context, create_time) values (?, ?, datetime('now'))`, hashHex, byteBuf.Bytes()); err != nil {
			return err
		}

		if _, err := d.db.Exec(`insert into app_files (appid, version, name, sha, compressed_size, uncompressed_size, create_time) values (?, ?, ?, ?, ?, ?, datetime('now'))`, appId, version, name, hashHex, byteBuf.Len(), len(buf)); err != nil {
			return err
		}
	}
	return nil
}
