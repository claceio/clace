// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package util

// WorkFs is the implementation of work file system
type WorkFs struct {
	WritableFS
	Root string
}

var _ WritableFS = (*WorkFs)(nil)

// NewWorkFs creates a new work file system
func NewWorkFs(dir string, fs WritableFS) *WorkFs {
	return &WorkFs{
		Root:       dir,
		WritableFS: fs,
	}
}
