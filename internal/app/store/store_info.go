// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

type TypeName string

const (
	INT     TypeName = "INT"
	STRING  TypeName = "STRING"
	BOOLEAN TypeName = "BOOLEAN"
	LIST    TypeName = "LIST"
	DICT    TypeName = "DICT"
	//DATETIME TypeName = "datetime"
)

type StoreInfo struct {
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

func ReadStoreInfo(fileName string, inp []byte) (*StoreInfo, error) {
	storeInfo, err := LoadStoreInfo(fileName, inp)
	if err != nil {
		return nil, err
	}

	if err := validateStoreInfo(storeInfo); err != nil {
		return nil, err
	}

	return storeInfo, nil
}

func validateStoreInfo(storeInfo *StoreInfo) error {
	if err := validateTypes(storeInfo.Types); err != nil {
		return err
	}

	// TODO: validate collections
	return nil
}

func validateTypes(types []StoreType) error {
	// TODO: validate types
	return nil
}
