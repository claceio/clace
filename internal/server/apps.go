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

// AppStore is a store of apps. List of apps is stored in memory. Apps are initialized lazily,
// AddApp has to be called before GetApp to initialize the app
type AppStore struct {
	*types.Logger
	server     *Server
	allApps    []types.AppInfo
	allDomains map[string]bool

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

func (a *AppStore) GetAppInfo() ([]types.AppInfo, map[string]bool, error) {
	a.mu.RLock()
	if a.allApps != nil {
		a.mu.RUnlock()
		return a.allApps, a.allDomains, nil
	}
	a.mu.RUnlock()

	// Get exclusive lock
	a.mu.Lock()
	defer a.mu.Unlock()

	err := a.updateAppInfo(a.allApps)
	if err != nil {
		return nil, nil, err
	}
	return a.allApps, a.allDomains, nil
}

func (a *AppStore) GetAllApps() ([]types.AppInfo, error) {
	a.mu.RLock()
	if a.allApps != nil {
		a.mu.RUnlock()
		return a.allApps, nil
	}
	a.mu.RUnlock()

	// Get exclusive lock
	a.mu.Lock()
	defer a.mu.Unlock()

	err := a.updateAppInfo(a.allApps)
	if err != nil {
		return nil, err
	}
	return a.allApps, nil
}

func (a *AppStore) GetAllDomains() (map[string]bool, error) {
	a.mu.RLock()
	if a.allDomains != nil {
		a.mu.RUnlock()
		return a.allDomains, nil
	}
	a.mu.RUnlock()

	// Get exclusive lock
	a.mu.Lock()
	defer a.mu.Unlock()

	err := a.updateAppInfo(a.allApps)
	if err != nil {
		return nil, err
	}
	return a.allDomains, nil
}

func (a *AppStore) updateAppInfo(allApps []types.AppInfo) error {
	var err error
	a.allApps, err = a.server.db.GetAllApps(true)
	if err != nil {
		return err
	}

	a.allDomains = make(map[string]bool)
	a.allDomains[a.server.config.System.DefaultDomain] = true
	for _, appInfo := range allApps {
		if appInfo.Domain != "" {
			a.allDomains[appInfo.Domain] = true
		}
	}
	return nil
}

func (a *AppStore) ClearAllAppCache() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.allApps = nil
	a.allDomains = nil
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
	a.allDomains = nil
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
	a.allDomains = nil
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
	a.allDomains = nil
}
