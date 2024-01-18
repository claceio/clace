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

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/utils"
)

const (
	DB_CONNECTION_CONFIG = "db_connection"
)

type SqlStore struct {
	*utils.Logger
	sync.Mutex
	isInitialized bool
	pluginContext *app.PluginContext
	db            *sql.DB
	prefix        string
	isSqlite      bool // false means postgres, no other options
}

var _ Store = (*SqlStore)(nil)

func NewSqlStore(pluginContext *app.PluginContext) (*SqlStore, error) {
	return &SqlStore{
		Logger:        pluginContext.Logger,
		pluginContext: pluginContext,
	}, nil
}

func validateTableName(name string) error {
	// TODO: validate table name
	return nil
}

func (s *SqlStore) genTableName(collection string) (string, error) {
	err := validateTableName(collection)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("'%s_%s'", s.prefix, collection), nil
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
		collection, err := s.genTableName(storeType.Name)
		if err != nil {
			return err
		}

		createStmt := "CREATE TABLE IF NOT EXISTS " + collection + " (id INTEGER PRIMARY KEY AUTOINCREMENT, version INTEGER, created_by TEXT, updated_by TEXT, created_at INTEGER, updated_at INTEGER, data JSON)"
		_, err = s.db.Exec(createStmt)
		if err != nil {
			return fmt.Errorf("error creating table %s: %w", collection, err)
		}
		s.Info().Msgf("Created table %s", collection)
	}

	return nil
}

// Insert a new entry in the store
func (s *SqlStore) Insert(collection string, entry *Entry) (EntryId, error) {
	if err := s.initialize(); err != nil {
		return -1, err
	}

	var err error
	collection, err = s.genTableName(collection)
	if err != nil {
		return -1, err
	}

	dataJson, err := json.Marshal(entry.Data)
	if err != nil {
		return -1, fmt.Errorf("error marshalling data for collection %s: %w", collection, err)
	}

	createStmt := "INSERT INTO " + collection + " (version, created_by, updated_by, created_at, updated_at, data) VALUES (?, ?, ?, ?, ?, ?)"
	result, err := s.db.Exec(createStmt, entry.Version, entry.CreatedBy, entry.UpdatedBy, entry.CreatedAt, entry.UpdatedAt, dataJson)
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
func (s *SqlStore) SelectById(collection string, key EntryId) (*Entry, error) {
	if err := s.initialize(); err != nil {
		return nil, err
	}

	var err error
	collection, err = s.genTableName(collection)
	if err != nil {
		return nil, err
	}

	query := "SELECT id, version, created_by, updated_by, created_at, updated_at, data FROM " + collection + " WHERE id = ?"
	row := s.db.QueryRow(query, key)

	var dataStr string
	entry := &Entry{}

	err = row.Scan(&entry.Id, &entry.Version, &entry.CreatedBy, &entry.UpdatedBy, &entry.CreatedAt, &entry.UpdatedAt, dataStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("entry %d not found in collection %s", key, collection)
		}
		return nil, err
	}

	if dataStr != "" {
		if err := json.Unmarshal([]byte(dataStr), &entry.Data); err != nil {
			return nil, err
		}
	}

	return entry, nil
}

// Select returns the entries matching the filter
func (s *SqlStore) Select(collection string, filter map[string]any, sort []string, offset, limit int64) (EntryIterator, error) {
	return nil, nil

}

// Update an existing entry in the store
func (s *SqlStore) Update(collection string, Entry *Entry) error {
	return nil
}

// DeleteById an entry from the store by id
func (s *SqlStore) DeleteById(collection string, key EntryId) error {
	return nil
}

// Delete entries from the store matching the filter
func (s *SqlStore) Delete(collection string, filter map[string]any) error {
	return nil
}
