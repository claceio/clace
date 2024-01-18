// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sync"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/claceio/clace/internal/app/dev"
	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
	"github.com/fsnotify/fsnotify"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// App is the main object that represents a Clace app. It is created when the app is loaded
type App struct {
	*utils.Logger
	*utils.AppEntry
	Name         string
	CustomLayout bool

	Config          *util.AppConfig
	sourceFS        *util.SourceFs
	initMutex       sync.Mutex
	initialized     bool
	reloadError     error
	reloadStartTime time.Time
	appDev          *dev.AppDev
	systemConfig    *utils.SystemConfig
	storeInfo       *utils.StoreInfo
	plugins         *AppPlugins

	globals       starlark.StringDict
	appDef        *starlarkstruct.Struct
	appRouter     *chi.Mux
	template      *template.Template
	watcher       *fsnotify.Watcher
	sseListeners  []chan SSEMessage
	funcMap       template.FuncMap
	starlarkCache map[string]*starlarkCacheEntry
}

type starlarkCacheEntry struct {
	globals starlark.StringDict
	err     error
}

type SSEMessage struct {
	event string
	data  string
}

func NewApp(sourceFS *util.SourceFs, workFS *util.WorkFs, logger *utils.Logger,
	appEntry *utils.AppEntry, systemConfig *utils.SystemConfig,
	plugins map[string]utils.PluginSettings) *App {
	newApp := &App{
		sourceFS:      sourceFS,
		Logger:        logger,
		AppEntry:      appEntry,
		systemConfig:  systemConfig,
		starlarkCache: map[string]*starlarkCacheEntry{},
	}
	newApp.plugins = NewAppPlugins(newApp, plugins, appEntry.Metadata.Accounts)

	if appEntry.IsDev {
		newApp.appDev = dev.NewAppDev(logger, &util.WritableSourceFs{SourceFs: sourceFS}, workFS, systemConfig)
	}

	funcMap := sprig.FuncMap()
	funcMap["static"] = func(name string) string {
		staticPath := path.Join(newApp.Config.Routing.StaticDir, name)
		fullPath := path.Join(newApp.Path, sourceFS.HashName(staticPath))
		return fullPath
	}
	funcMap["fileNonEmpty"] = func(name string) bool {
		staticPath := path.Join(newApp.Config.Routing.StaticDir, name)
		fi, err := sourceFS.Stat(staticPath)
		if err != nil {
			return false
		}
		return fi.Size() > 0
	}

	// Remove the env functions from sprig, since they can leak system information
	delete(funcMap, "env")
	delete(funcMap, "expandenv")

	newApp.funcMap = funcMap
	return newApp
}

func (a *App) Initialize() error {
	var reloaded bool
	var err error
	if reloaded, err = a.Reload(false, true); err != nil {
		return err
	}

	if reloaded && a.IsDev {
		if err := a.startWatcher(); err != nil {
			a.Info().Msgf("error starting watcher: %s", err)
			return err
		}
	}
	return nil
}

func (a *App) Close() error {
	a.initMutex.Lock()
	defer a.initMutex.Unlock()
	if a.watcher != nil {
		if err := a.watcher.Close(); err != nil {
			return err
		}
	}

	if a.appDev != nil {
		_ = a.appDev.Close()
	}

	return nil
}

func (a *App) ResetFS() {
	a.sourceFS.Reset()
}

func (a *App) Reload(force, immediate bool) (bool, error) {
	requestTime := time.Now()

	a.initMutex.Lock()
	defer a.initMutex.Unlock()
	if a.initialized && !force {
		return false, nil
	}

	if requestTime.Compare(a.reloadStartTime) == -1 {
		// Current request is older than the last reloaded request, ignore
		a.Info().Msg("Ignoring reload request since it is older than the last reload request")
		return false, nil
	}

	if !immediate {
		// Sleep to allow for multiple file changes to be processed together
		// For slower machines, this can be increased, default is 300ms. The tailwind watcher
		// especially might need a higher value
		time.Sleep(time.Duration(a.systemConfig.FileWatcherDebounceMillis) * time.Millisecond)
	}
	a.reloadStartTime = time.Now()

	var err error
	a.Info().Msg("Reloading app definition")

	// Clear any cached data
	a.sourceFS.ClearCache()
	clear(a.starlarkCache)

	err = a.loadSchemaInfo(a.sourceFS)
	if err != nil {
		return false, err
	}

	configData, err := a.sourceFS.ReadFile(util.CONFIG_LOCK_FILE_NAME)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, fs.ErrNotExist) && !os.IsNotExist(err) {
			return false, err
		}

		// Config lock is not present, use default config
		a.Debug().Msg("No config lock file found, using default config")
		a.Config = util.NewAppConfig()
		if a.IsDev {
			a.appDev.Config = a.Config
			a.appDev.SaveConfigLockFile()
		}
	} else {
		// Config lock file is present, read defaults from that
		a.Debug().Msg("Config lock file found, using config from lock file")
		a.Config = util.NewCompatibleAppConfig()
		if err := json.Unmarshal(configData, a.Config); err != nil {
			return false, err
		}
	}

	// Load Starlark config, AppConfig is updated with the settings contents
	if err = a.loadStarlarkConfig(); err != nil {
		return false, err
	}

	if a.IsDev {
		// Copy settings into appdev
		a.appDev.Config = a.Config
		a.appDev.CustomLayout = a.CustomLayout

		// Initialize style configuration
		if err := a.appDev.AppStyle.Init(a.Id, a.appDef); err != nil {
			return false, err
		}

		// Setup the CSS files
		if err = a.appDev.AppStyle.Setup(a.appDev); err != nil {
			return false, err
		}

		// Start the watcher for CSS files unless disabled
		if !a.appDev.AppStyle.DisableWatcher {
			if err = a.appDev.AppStyle.StartWatcher(a.appDev); err != nil {
				a.Warn().Err(err).Msg("Error starting tailwind watcher")
				fmt.Printf("Error: %s\n", err)
				// Allow the app to start even if the watcher fails
			}
		} else if err := a.appDev.AppStyle.StopWatcher(); err != nil {
			return false, err
		}

		// Setup the JS libraries
		if err := a.appDev.SetupJsLibs(); err != nil {
			return false, err
		}

		// Create the generated HTML
		if err = a.appDev.GenerateHTML(); err != nil {
			return false, err
		}
	}

	// Parse HTML templates
	if a.template, err = a.sourceFS.ParseFS(a.funcMap, a.Config.Routing.TemplateLocations...); err != nil {
		return false, err
	}
	a.initialized = true

	if a.IsDev {
		a.notifyClients()
	}
	return true, nil
}

func (a *App) loadSchemaInfo(sourceFS *util.SourceFs) error {
	// Load the schema info
	schemaInfoData, err := sourceFS.ReadFile(util.SCHEMA_FILE_NAME)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, fs.ErrNotExist) && !os.IsNotExist(err) {
			return err
		}
		return nil // Ignore absence of schema file
	}

	a.storeInfo, err = util.ReadStoreInfo(util.SCHEMA_FILE_NAME, schemaInfoData)
	if err != nil {
		return fmt.Errorf("error reading schema info: %w", err)
	}

	return nil

}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.Info().Str("method", r.Method).Str("url", r.URL.String()).Msg("App Received request")
	if a.reloadError != nil {
		a.Warn().Err(a.reloadError).Msg("Last reload had failed")
		http.Error(w, a.reloadError.Error(), http.StatusInternalServerError)
		return
	}
	a.appRouter.ServeHTTP(w, r)
}

func (a *App) startWatcher() error {
	a.initMutex.Lock()
	defer a.initMutex.Unlock()
	if a.watcher != nil {
		_ = a.watcher.Close()
	}

	var err error
	a.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Start listening for events.
	a.Trace().Msg("Start waiting for file changes")
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.Error().Msgf("Recovered from panic in watcher: %s", r)
			}
		}()
		for {
			select {
			case event, ok := <-a.watcher.Events:
				if !ok {
					return
				}
				a.Trace().Str("event", fmt.Sprint(event)).Msg("Received event")
				if event.Op == fsnotify.Chmod {
					continue
				}
				_, err := a.Reload(true, false)
				if err != nil {
					a.Error().Err(err).Msg("Error reloading app")
				}
				a.reloadError = err
				a.notifyClients()
			case err, ok := <-a.watcher.Errors:
				a.Error().Err(err).Msgf("Error in watcher error receiver")
				if !ok {
					return
				}
			}
		}
	}()

	// Add watcher path.
	err = a.watcher.Add(a.SourceUrl)
	if err != nil {
		return err
	}

	return nil
}

func (a *App) addSSEClient(newChan chan SSEMessage) {
	a.initMutex.Lock()
	defer a.initMutex.Unlock()
	a.sseListeners = append(a.sseListeners, newChan)
}

func (a *App) removeSSEClient(chanRemove chan SSEMessage) {
	a.initMutex.Lock()
	defer a.initMutex.Unlock()
	for i, ch := range a.sseListeners {
		if ch == chanRemove {
			a.sseListeners = append(a.sseListeners[:i], a.sseListeners[i+1:]...)
			break
		}
	}
}

func (a *App) notifyClients() {
	a.Trace().Msg("Notifying clients for reload")
	reloadMessage := SSEMessage{
		event: "clace_reload",
		data:  "App reloaded after file updates",
	}
	for _, ch := range a.sseListeners {
		ch <- reloadMessage
	}
}

func (a *App) sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	messageChan := make(chan SSEMessage)
	a.addSSEClient(messageChan)

	//keeping the connection alive with keep-alive protocol
	keepAliveTickler := time.NewTicker(15 * time.Second)
	notify := r.Context().Done()

	//listen to signal to close and unregister
	go func() {
		<-notify
		a.Trace().Msg("Closing SSE connection")
		a.removeSSEClient(messageChan)
		keepAliveTickler.Stop()
	}()

	for {
		select {
		case appMessage := <-messageChan:
			fmt.Fprintf(w, "event: %s\n", appMessage.event)
			fmt.Fprintf(w, "data: %s\n\n", appMessage.data)
			flusher.Flush()
		case <-keepAliveTickler.C:
			a.Trace().Msg("Sending keepalive")
			fmt.Fprintf(w, "event:keepalive\n\n")
			flusher.Flush()
		}
	}
}

// loadStarlark loads a starlark file. The main app.star, if it calls load on a file with .star suffix, then
// this function is used to load the starlark file.
func (a *App) loadStarlark(thread *starlark.Thread, module string, cache map[string]*starlarkCacheEntry) (starlark.StringDict, error) {
	cacheEntry, ok := cache[module]
	if cacheEntry == nil {
		if ok {
			// request for package whose loading is in progress
			return nil, fmt.Errorf("cycle in starlark load graph during load of %s", module)
		}
		// Add a placeholder to indicate "load in progress".
		cache[module] = nil

		buf, err := a.sourceFS.ReadFile(module)
		if err != nil {
			return nil, err
		}

		builtin, err := a.createBuiltin()
		if err != nil {
			return nil, err
		}
		globals, err := starlark.ExecFile(thread, module, buf, builtin)
		cacheEntry = &starlarkCacheEntry{globals, err}
		// Update the cache.
		cache[module] = cacheEntry
	}
	return cacheEntry.globals, cacheEntry.err
}
