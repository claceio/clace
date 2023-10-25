// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import "github.com/claceio/clace/internal/utils"

type DbFs struct {
	*utils.Logger
	fileStore *FileStore
}

func NewDbFs(logger *utils.Logger, fileStore *FileStore) *DbFs {
	return &DbFs{
		Logger:    logger,
		fileStore: fileStore,
	}
}
