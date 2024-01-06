// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package db

type TypeName string

const (
	INT     TypeName = "INT"
	STRING  TypeName = "STRING"
	BOOLEAN TypeName = "BOOLEAN"
	LIST    TypeName = "LIST"
	DICT    TypeName = "DICT"
	//DATETIME TypeName = "datetime"
)

type DBInfo struct {
	Types []DBType
}

type DBType struct {
	Name    string
	Fields  []DBField
	Indexes []Index
}

type DBField struct {
	Name    string
	Type    TypeName
	Default any
}

type Index struct {
	Fields []string
	Unique bool
}

func ReadDBInfo(fileName string, inp []byte) (*DBInfo, error) {
	dbInfo, err := LoadDBInfo(fileName, inp)
	if err != nil {
		return nil, err
	}

	if err := validateDBInfo(dbInfo); err != nil {
		return nil, err
	}

	return dbInfo, nil
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
