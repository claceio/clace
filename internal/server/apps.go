// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"strings"
	"sync"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/types"
)

// AppStore is a store of apps. Apps are initialized lazily, the first GetApp call on each app
// will load the app from the database.
type AppStore struct {
	*types.Logger
	server  *Server
	allApps []types.AppInfo

	mu     sync.RWMutex
	appMap map[types.AppPathDomain]*app.App
}

func NewAppStore(logger *types.Logger, server *Server) *AppStore {
	return &AppStore{
		Logger: logger,
		server: server,
		appMap: make(map[types.AppPathDomain]*app.App),
	}
}

func (a *AppStore) GetAllApps() ([]types.AppInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.allApps != nil {
		return a.allApps, nil
	}

	var err error
	a.allApps, err = a.server.db.GetAllApps(true)
	if err != nil {
		return nil, err
	}

	return a.allApps, nil
}

func (a *AppStore) ClearAllAppCache() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.allApps = nil
}

func (a *AppStore) GetApp(pathDomain types.AppPathDomain) (*app.App, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	app, ok := a.appMap[pathDomain]
	if !ok {
		return nil, fmt.Errorf("app not found: %s", pathDomain)
	}
	return app, nil
}

func (a *AppStore) AddApp(app *app.App) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.appMap[types.CreateAppPathDomain(app.Path, app.Domain)] = app
	a.allApps = nil
}

func (a *AppStore) DeleteLinkedApps(pathDomain types.AppPathDomain) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	linkedAppPrefix := pathDomain.Path + types.INTERNAL_APP_DELIM
	for key, app := range a.appMap {
		if app.Domain == pathDomain.Domain && strings.HasPrefix(app.Path, linkedAppPrefix) {
			a.clearApp(key)
		}
	}

	a.clearApp(pathDomain)
	a.allApps = nil
	return nil
}

func (a *AppStore) clearApp(pathDomain types.AppPathDomain) {
	app, ok := a.appMap[pathDomain]
	if ok {
		app.Close()
		delete(a.appMap, pathDomain)
	}
}

func (a *AppStore) DeleteApps(pathDomain []types.AppPathDomain) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, pd := range pathDomain {
		a.clearApp(pd)
	}
	a.allApps = nil
}

func (a *AppStore) UpdateApps(apps []*app.App) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, app := range apps {
		app.ResetFS() // clear the transaction for DbFS
		// close required??
		a.appMap[types.CreateAppPathDomain(app.Path, app.Domain)] = app
	}
	a.allApps = nil
}
