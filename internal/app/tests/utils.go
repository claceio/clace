// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package apptests

import (
	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/utils"
)

func createDevModeApp(logger *utils.Logger, fileData map[string]string) (*app.App, error) {
	return createTestAppInt(logger, fileData, true)
}

func createApp(logger *utils.Logger, fileData map[string]string) (*app.App, error) {
	return createTestAppInt(logger, fileData, false)
}

func createTestAppInt(logger *utils.Logger, fileData map[string]string, isDev bool) (*app.App, error) {
	testFS := app.NewAppFS("", &TestFS{fileData: fileData})
	a := app.NewApp(testFS, logger, createTestAppEntry("/test"))
	a.IsDev = isDev
	err := a.Initialize()
	return a, err
}

func createTestAppEntry(path string) *utils.AppEntry {
	return &utils.AppEntry{
		Id:     "testApp",
		Path:   path,
		Domain: "",
		FsPath: ".",
	}
}
