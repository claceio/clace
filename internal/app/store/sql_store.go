// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"

	_ "modernc.org/sqlite"
)

const (
	DB_CONNECTION_CONFIG = "db_connection"
	SELECT_MAX_LIMIT     = 100_000
	SELECT_DEFAULT_LIMIT = 10_000
)

type SqlStore struct {
	*utils.Logger
	sync.Mutex
	isInitialized bool
	pluginContext *utils.PluginContext
	db            *sql.DB
	prefix        string
	isSqlite      bool // false means postgres, no other options
}

var _ Store = (*SqlStore)(nil)

func NewSqlStore(pluginContext *utils.PluginContext) (*SqlStore, error) {
	return &SqlStore{
		Logger:        pluginContext.Logger,
		pluginContext: pluginContext,
	}, nil
}

func validateTableName(name string) error {
	// TODO: validate table name
	return nil
}

func (s *SqlStore) genTableName(table string) (string, error) {
	err := validateTableName(table)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("'%s_%s'", s.prefix, table), nil
}

func (s *SqlStore) initialize() error {
	s.Lock()
	defer s.Unlock()

	if s.isInitialized {
		// Already initialized
		return nil
	}

	if err := s.initStore(); err != nil {
		return err
	}
	s.isInitialized = true
	return nil
}

func checkConnectString(connStr string) (string, error) {
	parts := strings.SplitN(connStr, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid connection string: %s", connStr)
	}
	if !strings.HasPrefix(parts[0], "sqlite") { // only sqlite for now
		return "", fmt.Errorf("invalid connection string: %s, only sqlite supported", connStr)
	}
	return os.ExpandEnv(parts[1]), nil
}

func (s *SqlStore) initStore() error {
	if s.pluginContext.StoreInfo == nil {
		return fmt.Errorf("store info not found")
	}

	connectStringConfig, ok := s.pluginContext.Config[DB_CONNECTION_CONFIG]
	if !ok {
		return fmt.Errorf("db connection string not found in config")
	}
	connectString, ok := connectStringConfig.(string)
	if !ok {
		return fmt.Errorf("db connection string is not a string")
	}

	var err error
	connectString, err = checkConnectString(connectString)
	if err != nil {
		return err
	}

	s.db, err = sql.Open("sqlite", connectString)
	if err != nil {
		return fmt.Errorf("error opening db %s: %w", connectString, err)
	}
	s.isSqlite = true
	s.prefix = "db_" + string(s.pluginContext.AppId)[len(utils.ID_PREFIX_APP_PROD):]

	for _, storeType := range s.pluginContext.StoreInfo.Types {
		table, err := s.genTableName(storeType.Name)
		if err != nil {
			return err
		}

		createStmt := "CREATE TABLE IF NOT EXISTS " + table + " (id INTEGER PRIMARY KEY AUTOINCREMENT, version INTEGER, created_by TEXT, updated_by TEXT, created_at INTEGER, updated_at INTEGER, data JSON)"
		_, err = s.db.Exec(createStmt)
		if err != nil {
			return fmt.Errorf("error creating table %s: %w", table, err)
		}
		s.Info().Msgf("Created table %s", table)
	}

	return nil
}

// Insert a new entry in the store
func (s *SqlStore) Insert(table string, entry *Entry) (EntryId, error) {
	if err := s.initialize(); err != nil {
		return -1, err
	}

	entry.CreatedAt = time.Now()
	entry.CreatedBy = "admin" // TODO update userid

	var err error
	table, err = s.genTableName(table)
	if err != nil {
		return -1, err
	}

	dataJson, err := json.Marshal(entry.Data)
	if err != nil {
		return -1, fmt.Errorf("error marshalling data for table %s: %w", table, err)
	}

	createStmt := "INSERT INTO " + table + " (version, created_by, updated_by, created_at, updated_at, data) VALUES (?, ?, ?, ?, ?, ?)"
	result, err := s.db.Exec(createStmt, entry.Version, entry.CreatedBy, entry.UpdatedBy, entry.CreatedAt.UnixMilli(), entry.UpdatedAt.UnixMilli(), dataJson)
	if err != nil {
		return -1, nil
	}

	insertId, err := result.LastInsertId()
	if err != nil {
		return -1, nil
	}
	return EntryId(insertId), nil
}

// SelectById returns a single item from the store
func (s *SqlStore) SelectById(table string, id EntryId) (*Entry, error) {
	if err := s.initialize(); err != nil {
		return nil, err
	}

	var err error
	table, err = s.genTableName(table)
	if err != nil {
		return nil, err
	}

	query := "SELECT id, version, created_by, updated_by, created_at, updated_at, data FROM " + table + " WHERE id = ?"
	row := s.db.QueryRow(query, id)

	entry := &Entry{}
	var dataStr string
	var createdAt, updatedAt int64
	err = row.Scan(&entry.Id, &entry.Version, &entry.CreatedBy, &entry.UpdatedBy, &createdAt, &updatedAt, &dataStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entry %d not found in table %s", id, table)
		}
		return nil, err
	}

	if dataStr != "" {
		if err := json.Unmarshal([]byte(dataStr), &entry.Data); err != nil {
			return nil, err
		}
	}

	entry.CreatedAt = time.UnixMilli(createdAt)
	entry.UpdatedAt = time.UnixMilli(updatedAt)
	return entry, nil
}

// Select returns the entries matching the filter
func (s *SqlStore) Select(table string, filter map[string]any, sort []string, offset, limit int64) (starlark.Iterable, error) {
	if err := s.initialize(); err != nil {
		return nil, err
	}

	var err error
	table, err = s.genTableName(table)
	if err != nil {
		return nil, err
	}

	if limit > SELECT_MAX_LIMIT {
		return nil, fmt.Errorf("select limit %d exceeds max limit %d", limit, SELECT_MAX_LIMIT)
	}
	if limit <= 0 {
		limit = SELECT_DEFAULT_LIMIT
	}
	if offset < 0 {
		return nil, fmt.Errorf("select offset %d is invalid", offset)
	}

	limitOffsetStr := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)

	// TODO handle sort

	filterStr, params, err := parseQuery(filter)
	if err != nil {
		return nil, err
	}

	whereStr := ""
	if filterStr != "" {
		whereStr = " WHERE " + filterStr
	}

	query := "SELECT id, version, created_by, updated_by, created_at, updated_at, data FROM " + table + whereStr + limitOffsetStr
	s.Trace().Msgf("query: %s, params: %#v", query, params)
	rows, err := s.db.Query(query, params...)

	if err != nil {
		return nil, err
	}

	return NewStoreEntryIterabe(s.Logger, table, rows), nil
}

// Update an existing entry in the store
func (s *SqlStore) Update(table string, entry *Entry) (int64, error) {
	if err := s.initialize(); err != nil {
		return 0, err
	}

	var err error
	if table, err = s.genTableName(table); err != nil {
		return 0, err
	}

	origUpdateAt := entry.UpdatedAt
	entry.UpdatedAt = time.Now()
	entry.UpdatedBy = "admin" // TODO update userid

	dataJson, err := json.Marshal(entry.Data)
	if err != nil {
		return 0, fmt.Errorf("error marshalling data for table %s: %w", table, err)
	}

	updateStmt := "UPDATE " + table + " set version = ?, updated_by = ?, updated_at = ?, data = ? where id = ? and updated_at = ?"
	result, err := s.db.Exec(updateStmt, entry.Version, entry.UpdatedBy, entry.UpdatedAt.UnixMilli(), dataJson, entry.Id, origUpdateAt.UnixMilli())
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows == 0 {
		return 0, fmt.Errorf("entry %d not found or concurrently updated in table %s", entry.Id, table)
	}

	return rows, nil
}

// DeleteById an entry from the store by id
func (s *SqlStore) DeleteById(table string, id EntryId) (int64, error) {
	if err := s.initialize(); err != nil {
		return 0, err
	}

	var err error
	if table, err = s.genTableName(table); err != nil {
		return 0, err
	}

	deleteStmt := "DELETE from " + table + " where id = ?"
	result, err := s.db.Exec(deleteStmt, id)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if rows == 0 {
		return 0, fmt.Errorf("entry %d not found in table %s", id, table)
	}

	return rows, nil
}

// Delete entries from the store matching the filter
func (s *SqlStore) Delete(table string, filter map[string]any) (int64, error) {
	if err := s.initialize(); err != nil {
		return 0, err
	}

	var err error
	if table, err = s.genTableName(table); err != nil {
		return 0, err
	}

	filterStr, params, err := parseQuery(filter)
	if err != nil {
		return 0, err
	}

	whereStr := ""
	if filterStr != "" {
		whereStr = " WHERE " + filterStr
	}

	deleteStmt := "DELETE FROM " + table + whereStr
	result, err := s.db.Exec(deleteStmt, params...)
	if err != nil {
		return 0, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rows, nil
}
