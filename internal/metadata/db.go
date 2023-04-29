// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"database/sql"
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
		if _, err := m.db.Exec(`create table apps(id text, path text, domain text, code_url text, user_id text, create_time datetime, update_time datetime, rules text, metadata text, UNIQUE(id), UNIQUE(path, domain))`); err != nil {
			return err
		}
	}

	return nil
}

func (m *Metadata) AddApp(app *utils.App) error {
	stmt, err := m.db.Prepare(`INSERT into apps(id, path, domain, code_url, user_id, create_time, update_time, rules, metadata) values(?, ?, ?, ?, ?, datetime('now'), datetime('now'), ?, ?)`)
	if err != nil {
		return fmt.Errorf("error preparing statement: %w", err)
	}
	_, err = stmt.Exec(app.Id, app.Path, app.Domain, app.CodeUrl, app.UserID, app.Rules, app.Metadata)
	if err != nil {
		return fmt.Errorf("error inserting app: %w", err)
	}
	return nil
}

func (m *Metadata) GetApp(path, domain string) (*utils.App, error) {
	stmt, err := m.db.Prepare(`select id, path, domain, code_url, user_id, create_time, update_time, rules, metadata from apps where path = ? and domain = ?`)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	row := stmt.QueryRow(path, domain)
	var app utils.App
	err = row.Scan(&app.Id, &app.Path, &app.Domain, &app.CodeUrl, &app.UserID, &app.CreateTime, &app.UpdateTime, &app.Rules, &app.Metadata)
	if err != nil {
		return nil, fmt.Errorf("error getting app: %w", err)
	}
	return &app, nil
}

func (m *Metadata) DeleteApp(path, domain string) error {
	stmt, err := m.db.Prepare(`delete from apps where path = ? and domain = ?`)
	if err != nil {
		return fmt.Errorf("error preparing statement: %w", err)
	}
	_, err = stmt.Exec(path, domain)
	if err != nil {
		return fmt.Errorf("error deleting app: %w", err)
	}
	return nil
}
