// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"bytes"
	"encoding/json"
)

type TypeName string

const (
	INT     TypeName = "int"
	STRING  TypeName = "string"
	BOOLEAN TypeName = "boolean"
	LIST    TypeName = "list"
	DICT    TypeName = "dict"
	//DATETIME TypeName = "datetime"
)

type DBInfo struct {
	Types []DBType `json:"types"`
}

type DBType struct {
	Name    string              `json:"name"`
	Fields  map[string]TypeName `json:"fields"`
	Persist bool                `json:"persist"`
	Indexes []Index             `json:"indexes"`
}

type Index struct {
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

func ReadDBInfo(inp []byte) (*DBInfo, error) {
	var dbInfo DBInfo
	if err := json.NewDecoder(bytes.NewReader(inp)).Decode(&dbInfo); err != nil {
		return nil, err
	}

	if err := validateDBInfo(&dbInfo); err != nil {
		return nil, err
	}

	return &dbInfo, nil
}

func validateDBInfo(dbInfo *DBInfo) error {
	if err := validateTypes(dbInfo.Types); err != nil {
		return err
	}

	// TODO: validate collections
	return nil
}

func validateTypes(types []DBType) error {
	// TODO: validate types
	return nil
}
