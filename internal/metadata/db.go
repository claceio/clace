// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/claceio/clace/internal/utils"
	_ "modernc.org/sqlite"
)

const CURRENT_DB_VERSION = 3

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
	db, err := sql.Open("sqlite", dbPath)
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

	if version > CURRENT_DB_VERSION {
		return fmt.Errorf("DB version is newer than server version, exiting. Server %d, DB %d", CURRENT_DB_VERSION, version)
	}

	if version == CURRENT_DB_VERSION {
		m.Info().Msg("DB version is current")
		return nil
	}

	ctx := context.Background()
	tx, err := m.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if version < 1 {
		m.Info().Msg("No version, initializing")
		if _, err := tx.ExecContext(ctx, `create table version (version int, last_upgraded datetime)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `insert into version values (1, datetime('now'))`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `create table apps(id text, path text, domain text, source_url text, fs_path text, is_dev bool, auto_sync bool, auto_reload bool, user_id text, create_time datetime, update_time datetime, rules json, metadata json, loads json, permissions json, UNIQUE(id), UNIQUE(path, domain))`); err != nil {
			return err
		}
	}

	if version < 2 {
		m.Info().Msg("Upgrading to version 2")
		if err := m.initFileTables(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `update version set version=2, last_upgraded=datetime('now')`); err != nil {
			return err
		}
	}

	if version < 3 {
		m.Info().Msg("Upgrading to version 3")
		if _, err := tx.ExecContext(ctx, `alter table apps add column main_app text default ""`); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `update version set version=3, last_upgraded=datetime('now')`); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (m *Metadata) initFileTables(ctx context.Context, tx Transaction) error {
	if _, err := tx.ExecContext(ctx, `create table files (sha text, compression_type text, content blob, create_time datetime, PRIMARY KEY(sha))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `create table app_versions (appid text, version int, git_sha text, git_branch text, commit_message text, user_id text, metadata json, create_time datetime, PRIMARY KEY(appid, version))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `create table app_files (appid text, version int, name text, sha text, uncompressed_size int, create_time datetime, PRIMARY KEY(appid, version, name))`); err != nil {
		return err
	}

	return nil
}

func (m *Metadata) CreateApp(ctx context.Context, tx Transaction, app *utils.AppEntry) error {
	rulesJson, err := json.Marshal(app.Rules)
	if err != nil {
		return fmt.Errorf("error marshalling rules: %w", err)
	}
	metadataJson, err := json.Marshal(app.Metadata)
	if err != nil {
		return fmt.Errorf("error marshalling metadata: %w", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT into apps(id, path, domain, main_app, source_url, is_dev, auto_sync, auto_reload, user_id, create_time, update_time, rules, metadata) values(?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'), ?, ?)`, app.Id, app.Path, app.Domain, app.MainApp, app.SourceUrl, app.IsDev, app.AutoSync, app.AutoReload, app.UserID, rulesJson, metadataJson)
	if err != nil {
		return fmt.Errorf("error inserting app: %w", err)
	}
	return nil
}

func (m *Metadata) GetApp(pathDomain utils.AppPathDomain) (*utils.AppEntry, error) {
	stmt, err := m.db.Prepare(`select id, path, domain, main_app, source_url, is_dev, auto_sync, auto_reload, user_id, create_time, update_time, rules, metadata, loads, permissions from apps where path = ? and domain = ?`)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	row := stmt.QueryRow(pathDomain.Path, pathDomain.Domain)
	var app utils.AppEntry
	var loads, permissions sql.NullString
	var rules, metadata sql.NullString
	err = row.Scan(&app.Id, &app.Path, &app.Domain, &app.MainApp, &app.SourceUrl, &app.IsDev, &app.AutoSync, &app.AutoReload, &app.UserID, &app.CreateTime, &app.UpdateTime, &rules, &metadata, &loads, &permissions)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("app not found")
		}
		m.Error().Err(err).Msgf("query %s %s", pathDomain.Path, pathDomain.Domain)
		return nil, fmt.Errorf("error querying app: %w", err)
	}

	if loads.Valid && loads.String != "" {
		err = json.Unmarshal([]byte(loads.String), &app.Loads)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling loads: %w", err)
		}
	} else {
		app.Loads = []string{}
	}

	if permissions.Valid && permissions.String != "" {
		err = json.Unmarshal([]byte(permissions.String), &app.Permissions)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling permissions: %w", err)
		}
	} else {
		app.Permissions = []utils.Permission{}
	}

	if rules.Valid && rules.String != "" {
		err = json.Unmarshal([]byte(rules.String), &app.Rules)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling rules: %w", err)
		}
	} else {
		app.Rules = utils.Rules{AuthnType: utils.AppAuthnDefault}
	}

	if metadata.Valid && metadata.String != "" {
		err = json.Unmarshal([]byte(metadata.String), &app.Metadata)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling metadata: %w", err)
		}
	} else {
		app.Metadata = utils.Metadata{}
	}

	return &app, nil
}

func (m *Metadata) DeleteApp(ctx context.Context, tx Transaction, id utils.AppId) error {
	stageAppId := utils.AppId(string(id) + utils.STAGE_SUFFIX)
	if _, err := tx.ExecContext(ctx, `delete from apps where id = ? or id = ?`, id, stageAppId); err != nil {
		return fmt.Errorf("error deleting app : %w", err)
	}

	if _, err := tx.ExecContext(ctx, `delete from apps where main_app = ? `, id); err != nil {
		return fmt.Errorf("error deleting linked apps : %w", err)
	}

	if _, err := tx.ExecContext(ctx, `delete from app_versions where appid=? or appid = ?`, id, stageAppId); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `delete from app_files where appid=? or appid = ?`, id, stageAppId); err != nil {
		return err
	}

	// Clean up unused files. This can be done more aggressively, when older versions are deleted.
	// Currently done only when an app is deleted. This cleanup is across apps, not just the deleted app.
	if _, err := tx.ExecContext(ctx, `delete from files where sha not in (select distinct sha from app_files)`); err != nil {
		return err
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

func (m *Metadata) UpdateAppPermissions(ctx context.Context, tx Transaction, app *utils.AppEntry) error {
	loadsJson, err := json.Marshal(app.Loads)
	if err != nil {
		return fmt.Errorf("error marshalling loads: %w", err)
	}
	permissionsJson, err := json.Marshal(app.Permissions)
	if err != nil {
		return fmt.Errorf("error marshalling permissions: %w", err)
	}

	_, err = tx.ExecContext(ctx, `UPDATE apps set loads = ?, permissions = ? where path = ? and domain = ?`, string(loadsJson), string(permissionsJson), app.Path, app.Domain)
	if err != nil {
		return fmt.Errorf("error updating app: %w", err)
	}
	return nil
}

func (m *Metadata) UpdateAppRules(app *utils.AppEntry) error {
	stmt, err := m.db.Prepare(`UPDATE apps set rules = ? where path = ? and domain = ?`)
	if err != nil {
		return fmt.Errorf("error preparing statement: %w", err)
	}

	rulesJson, err := json.Marshal(app.Rules)
	if err != nil {
		return fmt.Errorf("error marshalling rules: %w", err)
	}

	_, err = stmt.Exec(string(rulesJson), app.Path, app.Domain)
	if err != nil {
		return fmt.Errorf("error updating app: %w", err)
	}
	return nil
}

func (m *Metadata) UpdateAppMetadata(ctx context.Context, tx Transaction, app *utils.AppEntry) error {
	metadataJson, err := json.Marshal(app.Metadata)
	if err != nil {
		return fmt.Errorf("error marshalling metadata: %w", err)
	}

	_, err = tx.ExecContext(ctx, `UPDATE apps set metadata = ? where path = ? and domain = ?`, string(metadataJson), app.Path, app.Domain)
	if err != nil {
		return fmt.Errorf("error updating app: %w", err)
	}
	return nil
}

// Transaction is a wrapper around sql.Tx
type Transaction struct {
	*sql.Tx
}

func (t Transaction) IsInitialized() bool {
	return t.Tx != nil
}

// BeginTransaction starts a new transaction
func (m *Metadata) BeginTransaction(ctx context.Context) (Transaction, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	return Transaction{tx}, err
}

// CommitTransaction commits a transaction
func (m *Metadata) CommitTransaction(tx Transaction) error {
	return tx.Commit()
}

// RollbackTransaction rolls back a transaction
func (m *Metadata) RollbackTransaction(tx Transaction) error {
	return tx.Rollback()
}
