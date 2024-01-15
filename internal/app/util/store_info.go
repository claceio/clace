// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package util

import "github.com/claceio/clace/internal/utils"

func ReadStoreInfo(fileName string, inp []byte) (*utils.StoreInfo, error) {
	storeInfo, err := LoadStoreInfo(fileName, inp)
	if err != nil {
		return nil, err
	}

	if err := validateStoreInfo(storeInfo); err != nil {
		return nil, err
	}

	return storeInfo, nil
}

func validateStoreInfo(storeInfo *utils.StoreInfo) error {
	if err := validateTypes(storeInfo.Types); err != nil {
		return err
	}

	// TODO: validate collections
	return nil
}

func validateTypes(types []utils.StoreType) error {
	// TODO: validate types
	return nil
}
