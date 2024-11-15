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

	"github.com/claceio/clace/internal/types"
	_ "modernc.org/sqlite"
)

const CURRENT_DB_VERSION = 3

// Metadata is the metadata persistence layer
type Metadata struct {
	*types.Logger
	config *types.ServerConfig
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
func NewMetadata(logger *types.Logger, config *types.ServerConfig) (*Metadata, error) {
	dbPath, err := checkConnectString(config.Metadata.DBConnection)
	if err != nil {
		return nil, err
	}

	logger.Info().Str("dbPath", dbPath).Msg("Connecting to DB")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=10000"); err != nil {
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
		if _, err := tx.ExecContext(ctx, `create table apps(id text, path text, domain text, source_url text, is_dev bool, main_app text, user_id text, create_time datetime, update_time datetime, settings json, metadata json, UNIQUE(id), UNIQUE(path, domain))`); err != nil {
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

		if _, err := tx.ExecContext(ctx, `alter table app_versions add column previous_version int default 0`); err != nil {
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

func (m *Metadata) initFileTables(ctx context.Context, tx types.Transaction) error {
	if _, err := tx.ExecContext(ctx, `create table files (sha text, compression_type text, content blob, create_time datetime, PRIMARY KEY(sha))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `create table app_versions (appid text, version int, user_id text, metadata json, create_time datetime, PRIMARY KEY(appid, version))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `create table app_files (appid text, version int, name text, sha text, uncompressed_size int, create_time datetime, PRIMARY KEY(appid, version, name))`); err != nil {
		return err
	}

	return nil
}

func (m *Metadata) CreateApp(ctx context.Context, tx types.Transaction, app *types.AppEntry) error {
	settingsJson, err := json.Marshal(app.Settings)
	if err != nil {
		return fmt.Errorf("error marshalling settings: %w", err)
	}
	metadataJson, err := json.Marshal(app.Metadata)
	if err != nil {
		return fmt.Errorf("error marshalling metadata: %w", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT into apps(id, path, domain, main_app, source_url, is_dev, user_id, create_time, update_time, settings, metadata) values(?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'), ?, ?)`,
		app.Id, app.Path, app.Domain, app.MainApp, app.SourceUrl, app.IsDev, app.UserID, settingsJson, metadataJson)
	if err != nil {
		return fmt.Errorf("error inserting app: %w", err)
	}
	return nil
}

func (m *Metadata) GetApp(pathDomain types.AppPathDomain) (*types.AppEntry, error) {
	tx, err := m.BeginTransaction(context.Background())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return m.GetAppTx(context.Background(), tx, pathDomain)
}

func (m *Metadata) GetAppTx(ctx context.Context, tx types.Transaction, pathDomain types.AppPathDomain) (*types.AppEntry, error) {
	stmt, err := tx.PrepareContext(ctx, `select id, path, domain, main_app, source_url, is_dev, user_id, create_time, update_time, settings, metadata from apps where path = ? and domain = ?`)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	row := stmt.QueryRow(pathDomain.Path, pathDomain.Domain)
	var app types.AppEntry
	var settings, metadata sql.NullString
	err = row.Scan(&app.Id, &app.Path, &app.Domain, &app.MainApp, &app.SourceUrl, &app.IsDev, &app.UserID, &app.CreateTime, &app.UpdateTime, &settings, &metadata)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("app not found")
		}
		m.Error().Err(err).Msgf("query %s %s", pathDomain.Path, pathDomain.Domain)
		return nil, fmt.Errorf("error querying app: %w", err)
	}

	if metadata.Valid && metadata.String != "" {
		err = json.Unmarshal([]byte(metadata.String), &app.Metadata)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling metadata: %w", err)
		}
	}

	if settings.Valid && settings.String != "" {
		err = json.Unmarshal([]byte(settings.String), &app.Settings)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling settings: %w", err)
		}
	}

	if app.Metadata.SpecFiles == nil {
		tf := make(types.SpecFiles)
		app.Metadata.SpecFiles = &tf
	}

	return &app, nil
}

func (m *Metadata) DeleteApp(ctx context.Context, tx types.Transaction, id types.AppId) error {
	if _, err := tx.ExecContext(ctx, `delete from app_versions where appid in (select id from apps where id = ? or main_app = ?)`, id, id); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `delete from app_files where appid in (select id from apps where id = ? or main_app = ?)`, id, id); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `delete from apps where id = ? or main_app = ? `, id, id); err != nil {
		return fmt.Errorf("error deleting apps : %w", err)
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
	defer rows.Close()
	for rows.Next() {
		var path string
		err = rows.Scan(&path)
		if err != nil {
			return nil, fmt.Errorf("error querying apps: %w", err)
		}
		paths = append(paths, path)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return nil, fmt.Errorf("error closing rows: %w", closeErr)
	}

	return paths, nil
}

func (m *Metadata) GetAllApps(includeInternal bool) ([]types.AppInfo, error) {
	sqlStr := `select domain, path, is_dev, id, main_app, settings, metadata, source_url from apps`
	if !includeInternal {
		sqlStr += ` where main_app = ''`
	}
	sqlStr += ` order by create_time asc`

	stmt, err := m.db.Prepare(sqlStr)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	rows, err := stmt.Query()
	if err != nil {
		return nil, fmt.Errorf("error querying apps: %w", err)
	}
	apps := make([]types.AppInfo, 0)
	defer rows.Close()
	for rows.Next() {
		var path, domain, id, mainApp, sourceUrl string
		var isDev bool
		var settingsStr, metadataStr sql.NullString
		err = rows.Scan(&domain, &path, &isDev, &id, &mainApp, &settingsStr, &metadataStr, &sourceUrl)
		if err != nil {
			return nil, fmt.Errorf("error querying apps: %w", err)
		}

		var metadata types.AppMetadata
		var settings types.AppSettings

		if metadataStr.Valid && metadataStr.String != "" {
			err = json.Unmarshal([]byte(metadataStr.String), &metadata)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling metadata: %w", err)
			}
		}

		if settingsStr.Valid && settingsStr.String != "" {
			err = json.Unmarshal([]byte(settingsStr.String), &settings)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling settings: %w", err)
			}
		}

		apps = append(apps, types.CreateAppInfo(types.AppId(id), path, domain, isDev, types.AppId(mainApp), settings.AuthnType, sourceUrl, metadata.Spec))
	}
	if closeErr := rows.Close(); closeErr != nil {
		return nil, fmt.Errorf("error closing rows: %w", closeErr)
	}
	return apps, nil
}

// GetLinkedApps gets all the apps linked to the given main app (staging and preview apps)
func (m *Metadata) GetLinkedApps(ctx context.Context, tx types.Transaction, mainAppId types.AppId) ([]*types.AppEntry, error) {
	stmt, err := tx.PrepareContext(ctx, `select id, path, domain, main_app, source_url, is_dev, user_id, create_time, update_time, settings, metadata from apps where main_app = ?`)
	if err != nil {
		return nil, fmt.Errorf("error preparing statement: %w", err)
	}
	rows, err := stmt.Query(mainAppId)
	if err != nil {
		return nil, fmt.Errorf("error querying apps: %w", err)
	}
	apps := make([]*types.AppEntry, 0)
	defer rows.Close()
	for rows.Next() {
		var app types.AppEntry
		var settings, metadata sql.NullString
		err = rows.Scan(&app.Id, &app.Path, &app.Domain, &app.MainApp, &app.SourceUrl, &app.IsDev, &app.UserID, &app.CreateTime, &app.UpdateTime, &settings, &metadata)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, errors.New("app not found")
			}
			m.Error().Err(err).Msgf("query %s", mainAppId)
			return nil, fmt.Errorf("error querying appy: %w", err)
		}

		if metadata.Valid && metadata.String != "" {
			err = json.Unmarshal([]byte(metadata.String), &app.Metadata)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling metadata: %w", err)
			}
		}

		if settings.Valid && settings.String != "" {
			err = json.Unmarshal([]byte(settings.String), &app.Settings)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling settings: %w", err)
			}
		}

		apps = append(apps, &app)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return nil, fmt.Errorf("error closing rows: %w", closeErr)
	}

	return apps, nil
}

func (m *Metadata) UpdateAppMetadata(ctx context.Context, tx types.Transaction, app *types.AppEntry) error {
	metadataJson, err := json.Marshal(app.Metadata)
	if err != nil {
		return fmt.Errorf("error marshalling metadata: %w", err)
	}

	_, err = tx.ExecContext(ctx, `UPDATE apps set metadata = ? where path = ? and domain = ?`, string(metadataJson), app.Path, app.Domain)
	if err != nil {
		return fmt.Errorf("error updating app metadata: %w", err)
	}

	if strings.HasPrefix(string(app.Id), types.ID_PREFIX_APP_PROD) || strings.HasPrefix(string(app.Id), types.ID_PREFIX_APP_STAGE) {
		_, err = tx.ExecContext(ctx, `UPDATE app_versions set metadata = ? where appid = ? and version = ?`, string(metadataJson), app.Id, app.Metadata.VersionMetadata.Version)
		if err != nil {
			return fmt.Errorf("error updating app metadata: %w", err)
		}
	}

	return nil
}

func (m *Metadata) UpdateAppSettings(ctx context.Context, tx types.Transaction, app *types.AppEntry) error {
	settingsJson, err := json.Marshal(app.Settings)
	if err != nil {
		return fmt.Errorf("error marshalling settings: %w", err)
	}

	_, err = tx.ExecContext(ctx, `UPDATE apps set settings = ? where path = ? and domain = ?`, string(settingsJson), app.Path, app.Domain)
	if err != nil {
		return fmt.Errorf("error updating app settings: %w", err)
	}

	return nil
}

// BeginTransaction starts a new Transaction
func (m *Metadata) BeginTransaction(ctx context.Context) (types.Transaction, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	return types.Transaction{tx}, err
}

// CommitTransaction commits a transaction
func (m *Metadata) CommitTransaction(tx types.Transaction) error {
	return tx.Commit()
}

// Rollbacktypes.Transaction rolls back a transaction
func (m *Metadata) RollbackTransaction(tx types.Transaction) error {
	return tx.Rollback()
}
