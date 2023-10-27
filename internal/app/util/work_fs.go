// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package util

import "github.com/claceio/clace/internal/utils"

// WorkFs is the implementation of work file system
type WorkFs struct {
	utils.WritableFS
	Root string
}

var _ utils.WritableFS = (*WorkFs)(nil)

// NewWorkFs creates a new work file system
func NewWorkFs(dir string, fs utils.WritableFS) *WorkFs {
	return &WorkFs{
		Root:       dir,
		WritableFS: fs,
	}
}
