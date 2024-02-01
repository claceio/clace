// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"fmt"
	"time"

	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
)

const (
	ID_FIELD         = "_id"
	VERSION_FIELD    = "_version"
	CREATED_BY_FIELD = "_created_by"
	UPDATED_BY_FIELD = "_updated_by"
	CREATED_AT_FIELD = "_created_at"
	UPDATED_AT_FIELD = "_updated_at"
	JSON_FIELD       = "_json"
)

var RESERVED_FIELDS = map[string]bool{
	ID_FIELD:         true,
	VERSION_FIELD:    true,
	CREATED_BY_FIELD: true,
	UPDATED_BY_FIELD: true,
	CREATED_AT_FIELD: true,
	UPDATED_AT_FIELD: true,
	JSON_FIELD:       true,
}

type EntryId int64
type UserId string
type Document map[string]any

type Entry struct {
	Id        EntryId
	Version   int64
	CreatedBy UserId
	UpdatedBy UserId
	CreatedAt time.Time
	UpdatedAt time.Time
	Data      Document
}

var _ starlark.Unpacker = (*Entry)(nil)

func (e *Entry) Unpack(value starlark.Value) error {
	v, ok := value.(starlark.HasAttrs)
	if !ok {
		return fmt.Errorf("expected entry, got %s", value.Type())
	}
	var err error

	entryData := make(map[string]any)
	for _, attr := range v.AttrNames() {
		switch attr {
		case ID_FIELD:
			id, err := util.GetIntAttr(v, attr)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", attr, err)
			}
			e.Id = EntryId(id)
		case VERSION_FIELD:
			e.Version, err = util.GetIntAttr(v, attr)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", attr, err)
			}
		case CREATED_BY_FIELD:
			createdBy, err := util.GetStringAttr(v, attr)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", attr, err)
			}
			e.CreatedBy = UserId(createdBy)
		case UPDATED_BY_FIELD:
			updatedBy, err := util.GetStringAttr(v, attr)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", attr, err)
			}
			e.UpdatedBy = UserId(updatedBy)
		case CREATED_AT_FIELD:
			createdAt, err := util.GetIntAttr(v, attr)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", attr, err)
			}
			e.CreatedAt = time.UnixMilli(createdAt)
		case UPDATED_AT_FIELD:
			updatedAt, err := util.GetIntAttr(v, attr)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", attr, err)
			}
			e.UpdatedAt = time.UnixMilli(updatedAt)
		default:
			dataVal, err := v.Attr(attr)
			if err != nil {
				return fmt.Errorf("error reading %s: %w", attr, err)
			}
			data, err := utils.UnmarshalStarlark(dataVal)
			if err != nil {
				return fmt.Errorf("error unmarshalling %s : %w", attr, err)
			}
			entryData[attr] = data
		}
	}

	e.Data = entryData
	return nil
}

// Store is the interface for a Clace document store. These API are exposed by the db plugin
type Store interface {
	// Insert a new entry in the store
	Insert(table string, Entry *Entry) (EntryId, error)

	// SelectById returns a single item from the store
	SelectById(table string, id EntryId) (*Entry, error)

	// SelectOne returns a single item from the store
	SelectOne(table string, filter map[string]any) (*Entry, error)

	// Select returns the entries matching the filter
	Select(table string, filter map[string]any, sort []string, offset, limit int64) (starlark.Iterable, error)

	// Count returns the count of entries matching the filter
	Count(table string, filter map[string]any) (int64, error)

	// Update an existing entry in the store
	Update(table string, Entry *Entry) (int64, error)

	// DeleteById an entry from the store by id
	DeleteById(table string, id EntryId) (int64, error)

	// Delete entries from the store matching the filter
	Delete(table string, filter map[string]any) (int64, error)
}

func CreateType(name string, entry *Entry) (*utils.StarlarkType, error) {
	data := make(map[string]starlark.Value)

	data[ID_FIELD] = starlark.MakeInt(int(entry.Id))
	data[VERSION_FIELD] = starlark.MakeInt(int(entry.Version))
	data[CREATED_BY_FIELD] = starlark.String(string(entry.CreatedBy))
	data[UPDATED_BY_FIELD] = starlark.String(string(entry.UpdatedBy))
	data[CREATED_AT_FIELD] = starlark.MakeInt(int(entry.CreatedAt.UnixMilli()))
	data[UPDATED_AT_FIELD] = starlark.MakeInt(int(entry.UpdatedAt.UnixMilli()))

	var err error
	for k, v := range entry.Data {
		data[k], err = utils.MarshalStarlark(v)
		if err != nil {
			return nil, err
		}
		// TODO - add missing fields
	}

	return utils.NewStarlarkType(name, data), nil
}
