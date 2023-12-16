// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/claceio/clace/internal/utils"
)

type FileStore struct {
	appId    utils.AppId
	version  int
	metadata *Metadata
	db       *sql.DB
	initTx   Transaction // This is the transaction for the initial setup of the app, before it is committed to the database.
	// After app is committed to database, this is not used, auto-commit transactions are used for reads
}

func NewFileStore(appId utils.AppId, version int, metadata *Metadata, tx Transaction) *FileStore {
	return &FileStore{appId: appId, version: version, metadata: metadata, db: metadata.db, initTx: tx}
}

func (f *FileStore) AddAppVersion(ctx context.Context, tx Transaction, versionMetadata utils.VersionMetadata, checkoutDir string) error {
	metadataJson, err := json.Marshal(versionMetadata)
	if err != nil {
		return fmt.Errorf("error marshalling metadata: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `insert into app_versions (appid, previous_version, version, metadata, create_time) values (?, ?, ?, ?, datetime('now'))`, f.appId, versionMetadata.PreviousVersion, versionMetadata.Version, metadataJson); err != nil {
		return fmt.Errorf("error inserting app version: %w", err)
	}

	var insertFileStmt *sql.Stmt
	if insertFileStmt, err = tx.PrepareContext(ctx, `insert or ignore into files (sha, compression_type, content, create_time) values (?, ?, ?, datetime('now'))`); err != nil {
		return err
	}

	var insertAppFileStmt *sql.Stmt
	if insertAppFileStmt, err = tx.PrepareContext(ctx, `insert into app_files (appid, version, name, sha, uncompressed_size, create_time) values (?, ?, ?, ?, ?, datetime('now'))`); err != nil {
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
		if _, err = insertFileStmt.ExecContext(ctx, hashHex, compressionType, storeBuf); err != nil {
			return fmt.Errorf("error inserting file: %w", err)
		}

		if _, err := insertAppFileStmt.ExecContext(ctx, f.appId, versionMetadata.Version, path, hashHex, len(buf)); err != nil {
			return fmt.Errorf("error inserting app file: %w", err)
		}

		return nil
	})
	return nil
}

func (f *FileStore) GetFileByShaTx(sha string) ([]byte, string, error) {
	var tx Transaction
	if f.initTx.IsInitialized() {
		tx = f.initTx
	} else {
		var err error
		tx, err = f.metadata.BeginTransaction(context.Background())
		if err != nil {
			return nil, "", fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
	}

	return f.GetFileBySha(context.Background(), tx, sha)
}

func (f *FileStore) GetFileBySha(ctx context.Context, tx Transaction, sha string) ([]byte, string, error) {
	stmt, err := tx.PrepareContext(ctx, "SELECT compression_type , content FROM files where sha = ?")
	if err != nil {
		return nil, "", fmt.Errorf("error preparing statement: %w", err)
	}
	row := stmt.QueryRow(sha)
	var compressionType string
	var content []byte
	if err := row.Scan(&compressionType, &content); err != nil {
		return nil, "", fmt.Errorf("error querying file table: %w", err)
	}

	return content, compressionType, nil
}

func (f *FileStore) getFileInfoTx() (map[string]DbFileInfo, error) {
	var tx Transaction
	if f.initTx.IsInitialized() {
		tx = f.initTx
	} else {
		var err error
		tx, err = f.metadata.BeginTransaction(context.Background())
		if err != nil {
			return nil, fmt.Errorf("error starting transaction: %w", err)
		}
		defer tx.Rollback()
	}
	return f.getFileInfo(context.Background(), tx)
}

func (f *FileStore) getFileInfo(ctx context.Context, tx Transaction) (map[string]DbFileInfo, error) {
	stmt, err := tx.PrepareContext(ctx, `select name, sha, uncompressed_size, create_time from app_files where appid = ? and version = ?`)
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

func (f *FileStore) GetHighestVersion(ctx context.Context, tx Transaction, appId utils.AppId) (int, error) {
	var maxId int
	row := tx.QueryRowContext(ctx, `select max(version) from app_versions where appid = ?`, appId)
	if err := row.Scan(&maxId); err != nil {
		return 0, nil // No versions found
	}
	return maxId, nil
}

func (f *FileStore) PromoteApp(ctx context.Context, tx Transaction, prodAppId utils.AppId, versionMetadata utils.VersionMetadata) error {
	metadataJson, err := json.Marshal(versionMetadata)
	if err != nil {
		return fmt.Errorf("error marshalling metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `insert into app_versions (appid, previous_version, version, metadata, create_time) values (?, ?, ?, ?, datetime('now'))`, prodAppId, versionMetadata.PreviousVersion, versionMetadata.Version, metadataJson); err != nil {
		return err
	}

	var insertStmt, selectStmt *sql.Stmt
	if insertStmt, err = tx.PrepareContext(ctx, `insert into app_files (appid, version, name, sha, uncompressed_size, create_time) values (?, ?, ?, ?, ?, datetime('now'))`); err != nil {
		return err
	}

	if selectStmt, err = tx.PrepareContext(ctx, `select name, sha, uncompressed_size, create_time from app_files where appid = ? and version = ?`); err != nil {
		return fmt.Errorf("error preparing statement: %w", err)
	}
	rows, err := selectStmt.Query(f.appId, f.version)
	if err != nil {
		return fmt.Errorf("error querying files: %w", err)
	}

	for rows.Next() {
		var name, sha string
		var size int64
		var modTime time.Time
		err = rows.Scan(&name, &sha, &size, &modTime)
		if err != nil {
			return fmt.Errorf("error querying files: %w", err)
		}

		// Copy entries from staging to prod app
		if _, err := insertStmt.ExecContext(ctx, prodAppId, versionMetadata.Version, name, sha, size); err != nil {
			return fmt.Errorf("error inserting app file: %w", err)
		}
	}

	return nil
}

func (f *FileStore) Reset() {
	// Unlink the file store from the transaction used during init
	f.initTx = Transaction{}
}
