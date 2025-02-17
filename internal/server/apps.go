// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
)

// AppStore is a store of apps. List of apps is stored in memory. Apps are initialized lazily,
// AddApp has to be called before GetApp to initialize the app
type AppStore struct {
	*types.Logger
	server     *Server
	allApps    []types.AppInfo
	idToInfo   map[types.AppId]types.AppInfo
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

func (a *AppStore) GetAppsFullInfo() ([]types.AppInfo, map[string]bool, error) {
	a.mu.RLock()
	if a.allApps != nil {
		a.mu.RUnlock()
		return a.allApps, a.allDomains, nil
	}
	a.mu.RUnlock()

	// Get exclusive lock
	a.mu.Lock()
	defer a.mu.Unlock()

	err := a.updateAppInfo()
	if err != nil {
		return nil, nil, err
	}
	return a.allApps, a.allDomains, nil
}

func (a *AppStore) GetAllAppsInfo() ([]types.AppInfo, error) {
	a.mu.RLock()
	if a.allApps != nil {
		a.mu.RUnlock()
		return a.allApps, nil
	}
	a.mu.RUnlock()

	// Get exclusive lock
	a.mu.Lock()
	defer a.mu.Unlock()

	err := a.updateAppInfo()
	if err != nil {
		return nil, err
	}
	return a.allApps, nil
}

func (a *AppStore) GetAppInfo(appId types.AppId) (types.AppInfo, bool) {
	a.mu.RLock()
	if a.idToInfo != nil {
		a.mu.RUnlock()
		info, ok := a.idToInfo[appId]
		return info, ok
	}
	a.mu.RUnlock()

	// Get exclusive lock
	a.mu.Lock()
	defer a.mu.Unlock()

	err := a.updateAppInfo()
	if err != nil {
		return types.AppInfo{}, false
	}
	info, ok := a.idToInfo[appId]
	return info, ok
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

	err := a.updateAppInfo()
	if err != nil {
		return nil, err
	}
	return a.allDomains, nil
}

func (a *AppStore) updateAppInfo() error {
	var err error
	a.allApps, err = a.server.db.GetAllApps(true)
	if err != nil {
		return err
	}

	a.idToInfo = make(map[types.AppId]types.AppInfo)
	for _, appInfo := range a.allApps {
		a.idToInfo[appInfo.Id] = appInfo
	}

	a.allDomains = make(map[string]bool)
	a.allDomains[a.server.config.System.DefaultDomain] = true
	for _, appInfo := range a.allApps {
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
	a.idToInfo = nil
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
	a.idToInfo = nil
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
	a.idToInfo = nil
}

func (a *AppStore) DeleteAppsAudit(ctx context.Context, pathDomain []types.AppPathDomain, op string) error {
	appInfo, error := a.GetAllAppsInfo()
	if error != nil {
		return error
	}
	appMap := getAppInfoMap(appInfo)

	event := types.AuditEvent{
		RequestId: system.GetContextRequestId(ctx),
		UserId:    system.GetContextUserId(ctx),
		EventType: types.EventTypeSystem,
		Operation: op,
		Status:    string(types.EventStatusSuccess),
	}

	for _, pd := range pathDomain {
		appInfo, ok := appMap[pd.String()]
		if !ok {
			continue
		}

		event.Target = pd.String()
		event.AppId = appInfo.Id
		event.CreateTime = time.Now()

		if err := a.server.InsertAuditEvent(&event); err != nil {
			return err
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, pd := range pathDomain {
		a.clearApp(pd)
	}
	a.allApps = nil
	a.allDomains = nil
	a.idToInfo = nil
	return nil
}

func getAppInfoMap(appInfo []types.AppInfo) map[string]types.AppInfo {
	ret := make(map[string]types.AppInfo)
	for _, info := range appInfo {
		ret[info.AppPathDomain.String()] = info
	}
	return ret
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
	a.idToInfo = nil
}
