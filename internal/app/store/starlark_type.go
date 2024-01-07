// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"fmt"

	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
)

// StarlarkType represents a Starlark type created from the JSON type definition.
type StarlarkType struct {
	name string
	data map[string]starlark.Value
	keys []string
}

func NewStarlarkType(name string, data map[string]starlark.Value) *StarlarkType {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}

	return &StarlarkType{
		name: name,
		data: data,
		keys: keys,
	}
}

func (s *StarlarkType) Attr(attr string) (starlark.Value, error) {
	val, ok := s.data[attr]
	if !ok {
		return starlark.None, fmt.Errorf("type %s has no attribute '%s'", s.name, attr)
	}
	return val, nil
}

func (s *StarlarkType) AttrNames() []string {
	return s.keys
}

func (s *StarlarkType) SetField(name string, val starlark.Value) error {
	if _, ok := s.data[name]; !ok {
		return starlark.NoSuchAttrError(fmt.Sprintf("type %s has no attribute '%s'", s.name, name))
	}

	s.data[name] = val
	return nil
}

func (s *StarlarkType) String() string {
	return fmt.Sprintf("type %s", s.name)
}

func (s *StarlarkType) Type() string {
	return s.name
}

func (s *StarlarkType) Freeze() {
	// Not supported
}

func (s *StarlarkType) Truth() starlark.Bool {
	return true
}

func (s *StarlarkType) Hash() (uint32, error) {
	values := make([]starlark.Value, 0, len(s.data))
	for _, v := range s.data {
		values = append(values, v)
	}

	return starlark.Tuple(values).Hash()
}

func (s *StarlarkType) UnmarshalStarlarkType() (any, error) {
	ret := make(map[string]any)
	for k, v := range s.data {
		var err error
		ret[k], err = utils.UnmarshalStarlark(v)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

var _ starlark.Value = (*StarlarkType)(nil)

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
		case STRING:
			var v starlark.String
			value = v
		case BOOLEAN:
			var v starlark.Bool
			value = v
		// TODO: add support for datetime
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

	return NewStarlarkType(s.Name, valueMap), nil
}
