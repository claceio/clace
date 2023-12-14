// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"sync"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/utils"
)

// AppStore is a store of apps. Apps are initialized lazily, the first GetApp call on each app
// will load the app from the database.
type AppStore struct {
	*utils.Logger
	server *Server

	mu     sync.RWMutex
	appMap map[utils.AppPathDomain]*app.App
}

func NewAppStore(logger *utils.Logger, server *Server) *AppStore {
	return &AppStore{
		Logger: logger,
		server: server,
		appMap: make(map[utils.AppPathDomain]*app.App),
	}
}

func (a *AppStore) AddApp(app *app.App) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.appMap[utils.CreateAppPathDomain(app.Path, app.Domain)] = app
}

func (a *AppStore) GetApp(pathDomain utils.AppPathDomain) (*app.App, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	app, ok := a.appMap[pathDomain]
	if !ok {
		return nil, fmt.Errorf("app not found: %s", pathDomain)
	}
	return app, nil
}

func (a *AppStore) DeleteApp(pathDomain utils.AppPathDomain) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.appMap, pathDomain)
	return nil
}

func (a *AppStore) UpdateApps(apps []*app.App) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, app := range apps {
		a.appMap[utils.CreateAppPathDomain(app.Path, app.Domain)] = app
	}
}
