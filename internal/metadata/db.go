// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/claceio/clace/internal/utils"
	_ "github.com/mattn/go-sqlite3"
)

const CURRENT_DB_VERSION = 1

// Metadata is the metadata persistence layer
type Metadata struct {
	*utils.Logger
	config *utils.ServerConfig
	db     *sql.DB
}

func checkConnectString(connStr string) (string, error) {
	parts := strings.SplitN(connStr, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid connection string: %s", connStr)
	}
	if !strings.HasPrefix(parts[0], "sqlite") {
		return "", fmt.Errorf("invalid connection string: %s, only sqlite supported", connStr)
	}
	return os.ExpandEnv(parts[1]), nil
}

// NewMetadata creates a new metadata persistence layer
func NewMetadata(logger *utils.Logger, config *utils.ServerConfig) (*Metadata, error) {
	dbPath, err := checkConnectString(config.Metadata.DBConnection)
	if err != nil {
		return nil, err
	}

	logger.Info().Str("dbPath", dbPath).Msg("Connecting to DB")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	m := &Metadata{
		Logger: logger,
		config: config,
		db:     db,
	}

	err = m.VersionUpgrade()
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Metadata) VersionUpgrade() error {
	version := 0
	row := m.db.QueryRow("SELECT version, last_upgraded FROM version")
	var dt time.Time
	row.Scan(&version, &dt)

	if version < CURRENT_DB_VERSION && !m.config.Metadata.AutoUpgrade {
		return fmt.Errorf("DB autoupgrade is disabled, exiting. Server %d, DB %d", CURRENT_DB_VERSION, version)
	}

	if version < 1 {
		m.Info().Msg("No version, initializing")
		if _, err := m.db.Exec(`create table version (version int, last_upgraded datetime)`); err != nil {
			return err
		}
		if _, err := m.db.Exec(`insert into version values (1, datetime('now'))`); err != nil {
			return err
		}
		if _, err := m.db.Exec(`create table apps(id text, path text, domain text, source_url text, fs_path text, is_dev bool, auto_sync bool, auto_reload bool, user_id text, create_time datetime, update_time datetime, rules text, metadata text, UNIQUE(id), UNIQUE(path, domain))`); err != nil {
			return err
		}
	}

	return nil
}

func (m *Metadata) AddApp(app *utils.AppEntry) error {
	stmt, err := m.db.Prepare(`INSERT into apps(id, path, domain, source_url, fs_path, is_dev, auto_sync, auto_reload, user_id, create_time, update_time, rules, metadata) values(?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'), ?, ?)`)
	if err != nil {
		return fmt.Errorf("error preparing statement: %w", err)
	}
	_, err = stmt.Exec(app.Id, app.Path, app.Domain, app.SourceUrl, app.FsPath, app.IsDev, app.AutoSync, app.AutoReload, app.UserID, app.Rules, app.Metadata)
	if err != nil {
		return fmt.Errorf("error inserting app: %w", err)
	}
	return nil
}

func (m *Metadata) GetApp(pathDomain utils.AppPathDomain) (*utils.AppEntry, error) {
	stmt, err := m.db.Prepare(`select id, path, domain, source_url, fs_path, is_dev, auto_sync, auto_reload, user_id, create_time, update_time, rules, metadata from apps where path = ? and domain = ?`)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	row := stmt.QueryRow(pathDomain.Path, pathDomain.Domain)
	var app utils.AppEntry
	err = row.Scan(&app.Id, &app.Path, &app.Domain, &app.SourceUrl, &app.FsPath, &app.IsDev, &app.AutoSync, &app.AutoReload, &app.UserID, &app.CreateTime, &app.UpdateTime, &app.Rules, &app.Metadata)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("app not found")
		}
		m.Error().Err(err).Msgf("query %s %s", pathDomain.Path, pathDomain.Domain)
		return nil, fmt.Errorf("error querying app: %w", err)
	}
	return &app, nil
}

func (m *Metadata) DeleteApp(pathDomain utils.AppPathDomain) error {
	stmt, err := m.db.Prepare(`delete from apps where path = ? and domain = ?`)
	if err != nil {
		return fmt.Errorf("error preparing statement: %w", err)
	}
	_, err = stmt.Exec(pathDomain.Path, pathDomain.Domain)
	if err != nil {
		return fmt.Errorf("error deleting app: %w", err)
	}
	return nil
}

func (m *Metadata) GetAppsForDomain(domain string) ([]string, error) {
	stmt, err := m.db.Prepare(`select path from apps where domain = ?`)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	rows, err := stmt.Query(domain)
	if err != nil {
		return nil, fmt.Errorf("error querying apps: %w", err)
	}
	paths := make([]string, 0)
	for rows.Next() {
		var path string
		err = rows.Scan(&path)
		if err != nil {
			return nil, fmt.Errorf("error querying apps: %w", err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (m *Metadata) GetAllApps() ([]utils.AppPathDomain, error) {
	stmt, err := m.db.Prepare(`select domain, path from apps`)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	rows, err := stmt.Query()
	if err != nil {
		return nil, fmt.Errorf("error querying apps: %w", err)
	}
	pathDomains := make([]utils.AppPathDomain, 0)
	for rows.Next() {
		var path, domain string
		err = rows.Scan(&domain, &path)
		if err != nil {
			return nil, fmt.Errorf("error querying apps: %w", err)
		}
		pathDomains = append(pathDomains, utils.CreateAppPathDomain(path, domain))
	}
	return pathDomains, nil
}
