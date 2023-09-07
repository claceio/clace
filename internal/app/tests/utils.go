// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package apptests

import (
	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/utils"
)

func createDevModeApp(logger *utils.Logger, fileData map[string]string) (*app.App, *app.AppFS, error) {
	return createTestAppInt(logger, fileData, true)
}

func createApp(logger *utils.Logger, fileData map[string]string) (*app.App, *app.AppFS, error) {
	return createTestAppInt(logger, fileData, false)
}

func createTestAppInt(logger *utils.Logger, fileData map[string]string, isDev bool) (*app.App, *app.AppFS, error) {
	sourceFS := app.NewAppFS("", &TestFS{fileData: fileData})
	workFS := app.NewAppFS("", &TestFS{fileData: map[string]string{}})
	systemConfig := utils.SystemConfig{TailwindCSSCommand: ""}
	a := app.NewApp(sourceFS, workFS, logger, createTestAppEntry("/test"), &systemConfig)
	a.IsDev = isDev
	err := a.Initialize()
	return a, workFS, err
}

func createTestAppEntry(path string) *utils.AppEntry {
	return &utils.AppEntry{
		Id:     "testApp",
		Path:   path,
		Domain: "",
		FsPath: ".",
	}
}
