// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package starlark_type

import (
	"fmt"

	"go.starlark.net/starlark"
)

type TypeName string

const (
	INT      TypeName = "INT"
	FLOAT    TypeName = "FLOAT"
	DATETIME TypeName = "DATETIME"
	STRING   TypeName = "STRING"
	BOOLEAN  TypeName = "BOOLEAN"
	LIST     TypeName = "LIST"
	DICT     TypeName = "DICT"
)

type StoreInfo struct {
	Bytes []byte // The raw bytes for the schema.star file
	Types []StoreType
}

type StoreType struct {
	Name    string
	Fields  []StoreField
	Indexes []Index
}

type StoreField struct {
	Name    string
	Type    TypeName
	Default any
}

type Index struct {
	Fields []string
	Unique bool
}

type TypeBuilder struct {
	Name   string
	Fields []StoreField
}

func (s *TypeBuilder) CreateType(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	unpackArgs := make([]any, 0, 2*len(s.Fields))
	for _, f := range s.Fields {

		// Unpack takes field name followed by a pointer to the value
		unpackArgs = append(unpackArgs, f.Name)
		var value starlark.Value

		switch f.Type {
		case INT:
			var v starlark.Int
			value = v
		case FLOAT:
			var v starlark.Float
			value = v
		case DATETIME:
			var v starlark.Int
			value = v
		case STRING:
			var v starlark.String
			value = v
		case BOOLEAN:
			var v starlark.Bool
			value = v
		case LIST:
			var v *starlark.List
			value = v
		case DICT:
			var v *starlark.Dict
			value = v
		default:
			return nil, fmt.Errorf("unknown type %s for %s", f.Type, f.Name)
		}

		// Add value pointer
		unpackArgs = append(unpackArgs, &value)
	}

	if err := starlark.UnpackArgs(s.Name, args, kwargs, unpackArgs...); err != nil {
		return nil, err
	}

	valueMap := make(map[string]starlark.Value)
	for i := 0; i < len(unpackArgs); i += 2 {
		argName := unpackArgs[i].(string)

		var ok bool
		val, ok := (unpackArgs[i+1]).(*starlark.Value)
		if !ok {
			return nil, fmt.Errorf("invalid type for %s", argName)
		}
		valueMap[argName] = *val
	}

	valueMap["_id"] = starlark.MakeInt(0)
	valueMap["_version"] = starlark.MakeInt(0)
	valueMap["_created_by"] = starlark.String("")
	valueMap["_updated_by"] = starlark.String("")
	valueMap["_created_at"] = starlark.MakeInt(0)
	valueMap["_updated_at"] = starlark.MakeInt(0)

	return NewStarlarkType(s.Name, valueMap), nil
}
