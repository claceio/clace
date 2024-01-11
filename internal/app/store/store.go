// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import "time"

type EntryId int64
type UserId string
type Document map[string]any

type Entry struct {
	Id        EntryId
	Version   int
	CreatedBy UserId
	UpdatedBy UserId
	CreatedAt time.Time
	UpdatedAt time.Time
	Data      Document
}

type EntryIterator interface {
	HasMore() bool
	Fetch() *Entry
}

// Store is the interface for a Clace document store. These API are exposed by the db plugin
type Store interface {
	// Create a new entry in the store
	Create(collection string, Entry *Entry) (EntryId, error)

	// GetByKey returns a single item from the store
	GetByKey(collection string, key EntryId) (*Entry, error)

	// Get returns the entries matching the filter
	Get(collection string, filter map[string]any, sort map[string]int) (EntryIterator, error)

	// Update an existing entry in the store
	Update(collection string, Entry *Entry) error

	// Delete an entry from the store by key
	DeleteByKey(collection string, key EntryId) error

	// Delete entries from the store matching the filter
	Delete(collection string, filter map[string]any) error
}
