// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/claceio/clace/internal/utils"
)

type SqliteStore struct {
	*utils.Logger
	config *utils.ServerConfig
	db     *sql.DB
	prefix string
}

func NewSqliteStore(logger *utils.Logger, config *utils.ServerConfig, db *sql.DB, prefix string) (*SqliteStore, error) {
	return &SqliteStore{
		Logger: logger,
		config: config,
		db:     db,
		prefix: prefix,
	}, nil
}

func (s *SqliteStore) genTableName(collection string) (string, error) {
	err := validateCollectionName(collection)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("'%s_%s'", s.prefix, collection), nil
}

// Create a new entry in the store
func (s *SqliteStore) Create(collection string, entry *Entry) (EntryId, error) {
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

// GetByKey returns a single item from the store
func (s *SqliteStore) GetByKey(collection string, key EntryId) (*Entry, error) {
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

// Get returns the entries matching the filter
func (s *SqliteStore) Get(collection string, filter map[string]any, sort map[string]int) (EntryIterator, error) {
	return nil, nil

}

// Update an existing entry in the store
func (s *SqliteStore) Update(collection string, Entry *Entry) error {
	return nil
}

// Delete an entry from the store by key
func (s *SqliteStore) DeleteByKey(collection string, key string) error {
	return nil
}

// Delete entries from the store matching the filter
func (s *SqliteStore) Delete(collection string, filter map[string]any) error {
	return nil
}
