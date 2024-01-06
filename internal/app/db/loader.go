// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"fmt"

	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	TYPE  = "type"
	FIELD = "field"
	INDEX = "index"
)

func LoadDBInfo(fileName string, data []byte) (*DBInfo, error) {
	definedTypes := make(map[string]starlark.Value)

	typeBuiltin := func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name starlark.String
		var fields, indexes *starlark.List
		if err := starlark.UnpackArgs(TYPE, args, kwargs, "name", &name, "fields", &fields, "indexes?", &indexes); err != nil {
			return nil, err
		}

		if indexes == nil {
			indexes = starlark.NewList([]starlark.Value{})
		}

		typeDict := starlark.StringDict{
			"name":    name,
			"fields":  fields,
			"indexes": indexes,
		}
		newType := starlarkstruct.FromStringDict(starlark.String(TYPE), typeDict)

		if _, ok := definedTypes[string(name)]; ok {
			return nil, fmt.Errorf("type %s already defined", name)
		}
		definedTypes[string(name)] = newType

		return newType, nil
	}

	builtins := starlark.StringDict{
		TYPE:            starlark.NewBuiltin(TYPE, typeBuiltin),
		FIELD:           starlark.NewBuiltin(FIELD, createFieldBuiltin),
		INDEX:           starlark.NewBuiltin(INDEX, createIndexBuiltin),
		string(INT):     starlark.String(INT),
		string(STRING):  starlark.String(STRING),
		string(BOOLEAN): starlark.String(BOOLEAN),
		string(DICT):    starlark.String(DICT),
		string(LIST):    starlark.String(LIST),
	}

	thread := &starlark.Thread{
		Name:  fileName,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
	}

	_, err := starlark.ExecFile(thread, fileName, data, builtins)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			fmt.Printf("Error loading app schema: %s\n", evalErr.Backtrace()) // TODO: log
		}
		return nil, fmt.Errorf("error loading app schema: %w", err)
	}

	return createDBInfo(definedTypes)
}

func createDBInfo(definedTypes map[string]starlark.Value) (*DBInfo, error) {
	types := make([]DBType, 0, len(definedTypes))
	for _, t := range definedTypes {
		typeStruct, ok := t.(*starlarkstruct.Struct)
		if !ok {
			return nil, fmt.Errorf("invalid type definition: %s", t.String())
		}

		typeName, err := util.GetStringAttr(typeStruct, "name")
		if err != nil {
			return nil, err
		}

		fields, err := getFields(string(typeName), typeStruct, "fields")
		if err != nil {
			return nil, fmt.Errorf("error getting fields in type %s: %s", typeName, err)
		}
		indexes, err := getIndexes(string(typeName), typeStruct, "indexes")
		if err != nil {
			return nil, fmt.Errorf("error getting indexes in type %s: %s", typeName, err)
		}

		types = append(types, DBType{
			Name:    string(typeName),
			Fields:  fields,
			Indexes: indexes,
		})
	}

	return &DBInfo{
		Types: types,
	}, nil
}

func getFields(typeName string, typeStruct *starlarkstruct.Struct, key string) ([]DBField, error) {
	fieldsAttr, err := typeStruct.Attr(key)
	if err != nil {
		return nil, fmt.Errorf("error getting %s attribute in type %s: %s", key, typeName, err)
	}

	fields, ok := fieldsAttr.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("%s is not a list in type %s", key, typeName)
	}

	fieldsList := fields
	iter := fieldsList.Iterate()
	var val starlark.Value

	ret := make([]DBField, 0, fields.Len())
	for iter.Next(&val) {
		fieldStruct, ok := val.(*starlarkstruct.Struct)
		if !ok {
			return nil, fmt.Errorf("invalid field definition: %s", val.String())
		}

		fieldName, err := util.GetStringAttr(fieldStruct, "name")
		if err != nil {
			return nil, err
		}

		fieldType, err := util.GetStringAttr(fieldStruct, "type")
		if err != nil {
			return nil, err
		}

		field := DBField{
			Name: string(fieldName),
			Type: TypeName(fieldType),
		}

		defaultValue, err := fieldStruct.Attr("default")
		if err == nil { // Attr is present
			val, err := utils.UnmarshalStarlark(defaultValue)
			if err != nil {
				return nil, fmt.Errorf("error unmarshalling default value for field %s in type %s: %s", fieldName, typeName, err)
			}
			field.Default = val
		}

		ret = append(ret, field)
	}

	return ret, nil
}

func getIndexes(typeName string, typeStruct *starlarkstruct.Struct, key string) ([]Index, error) {
	indexesAttr, err := typeStruct.Attr(key)
	if err != nil {
		return []Index{}, nil // no indexes
	}

	if indexesAttr == nil || indexesAttr == starlark.None {
		return []Index{}, nil
	}

	indexes, ok := indexesAttr.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("%s is not a list in type %s", key, typeName)
	}

	indexesList := indexes
	iter := indexesList.Iterate()
	var val starlark.Value

	ret := make([]Index, 0, indexes.Len())
	for iter.Next(&val) {
		indexStruct, ok := val.(*starlarkstruct.Struct)
		if !ok {
			return nil, fmt.Errorf("invalid index definition: %s", val.String())
		}

		fields, err := util.GetListStringAttr(indexStruct, "fields", false)
		if err != nil {
			return nil, err
		}

		unique, err := util.GetBoolAttr(indexStruct, "unique")
		if err != nil {
			return nil, err
		}

		ret = append(ret, Index{
			Fields: fields,
			Unique: unique,
		})
	}

	return ret, nil
}

func createFieldBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name, fieldType starlark.String
	var defaultValue starlark.Value = starlark.None
	if err := starlark.UnpackArgs(FIELD, args, kwargs, "name", &name, "type", &fieldType, "default?", &defaultValue); err != nil {
		return nil, err
	}

	field := starlark.StringDict{
		"name": name,
		"type": fieldType,
	}

	if defaultValue != starlark.None {
		field["default"] = defaultValue
	}
	return starlarkstruct.FromStringDict(starlark.String(FIELD), field), nil
}

func createIndexBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var fields *starlark.List
	var unique starlark.Bool = false
	if err := starlark.UnpackArgs(INDEX, args, kwargs, "fields", &fields, "unique?", &unique); err != nil {
		return nil, err
	}

	index := starlark.StringDict{
		"fields": fields,
		"unique": unique,
	}

	return starlarkstruct.FromStringDict(starlark.String(INDEX), index), nil
}
