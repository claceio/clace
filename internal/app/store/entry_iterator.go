// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
)

type StoreEntryIterable struct {
	*utils.Logger
	table string
	rows  *sql.Rows
}

func NewStoreEntryIterabe(logger *utils.Logger, table string, rows *sql.Rows) *StoreEntryIterable {
	return &StoreEntryIterable{
		Logger: logger,
		table:  table,
		rows:   rows,
	}
}

var _ starlark.Iterable = (*StoreEntryIterable)(nil)

func (s *StoreEntryIterable) Iterate() starlark.Iterator {
	return NewStoreEntryIterator(s.Logger, s.table, s.rows)
}

func (s *StoreEntryIterable) String() string {
	return s.Type()
}

func (s *StoreEntryIterable) Type() string {
	return s.table + " iterator"
}

func (s *StoreEntryIterable) Freeze() {
	// Not supported
}

func (s *StoreEntryIterable) Truth() starlark.Bool {
	return true
}

func (s *StoreEntryIterable) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", s.Type())
}

type StoreEntryIterator struct {
	*utils.Logger
	table string
	rows  *sql.Rows
}

var _ starlark.Iterator = (*StoreEntryIterator)(nil)

func NewStoreEntryIterator(logger *utils.Logger, table string, rows *sql.Rows) *StoreEntryIterator {
	return &StoreEntryIterator{
		Logger: logger,
		table:  table,
		rows:   rows,
	}
}

func (i *StoreEntryIterator) Next(value *starlark.Value) bool {
	entry := Entry{}
	hasNext := i.rows.Next()
	if !hasNext {
		err := i.rows.Close()
		if err != nil {
			i.Error().Err(err).Msg("error closing rows")
		}
		return false
	}

	var dataStr string
	var createdAt, updatedAt int64

	err := i.rows.Scan(&entry.Id, &entry.Version, &entry.CreatedBy, &entry.UpdatedBy, &createdAt, &updatedAt, &dataStr)
	if err != nil {
		closeError := i.rows.Close()
		if closeError != nil {
			i.Error().Err(fmt.Errorf("error closing rows: %w after scan error %s", closeError, err))
		}
		panic(err)
	}

	if dataStr != "" {
		if err := json.Unmarshal([]byte(dataStr), &entry.Data); err != nil {
			panic(err)
		}
	}

	returnType, err := CreateType(i.table, &entry)
	if err != nil {
		panic(err)
	}

	*value = returnType
	return true
}

func (i *StoreEntryIterator) Done() {
	closeErr := i.rows.Close()
	if closeErr != nil {
		i.Error().Err(fmt.Errorf("error closing rows: %w", closeErr))
	}
}
