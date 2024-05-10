// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/claceio/clace/internal/app/appfs"
	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/app/container"
	"github.com/claceio/clace/internal/app/dev"
	"github.com/claceio/clace/internal/app/starlark_type"
	"github.com/claceio/clace/internal/types"
	"github.com/fsnotify/fsnotify"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// App is the main object that represents a Clace app. It is created when the app is loaded
type App struct {
	*types.Logger
	*types.AppEntry
	Name         string
	CustomLayout bool

	Config           *apptype.AppConfig
	sourceFS         *appfs.SourceFs
	initMutex        sync.Mutex
	initialized      bool
	reloadError      error
	reloadStartTime  time.Time
	appDev           *dev.AppDev
	systemConfig     *types.SystemConfig
	storeInfo        *starlark_type.StoreInfo
	paramInfo        map[string]apptype.AppParam
	plugins          *AppPlugins
	containerManager *container.Manager

	globals      starlark.StringDict
	appDef       *starlarkstruct.Struct
	errorHandler starlark.Callable
	appRouter    *chi.Mux

	usesHtmlTemplate bool                          // Whether the app uses HTML templates, false if only JSON APIs
	template         *template.Template            // unstructured templates, no base_templates defined
	templateMap      map[string]*template.Template // structured templates, base_templates defined

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

func NewApp(sourceFS *appfs.SourceFs, workFS *appfs.WorkFs, logger *types.Logger,
	appEntry *types.AppEntry, systemConfig *types.SystemConfig,
	plugins map[string]types.PluginSettings) *App {
	newApp := &App{
		sourceFS:      sourceFS,
		Logger:        logger,
		AppEntry:      appEntry,
		systemConfig:  systemConfig,
		starlarkCache: map[string]*starlarkCacheEntry{},
	}
	newApp.plugins = NewAppPlugins(newApp, plugins, appEntry.Metadata.Accounts)

	if appEntry.IsDev {
		newApp.appDev = dev.NewAppDev(logger, &appfs.WritableSourceFs{SourceFs: sourceFS}, workFS, systemConfig)
	}

	funcMap := sprig.FuncMap()
	funcMap["static"] = func(name string) string {
		staticPath := path.Join("static", name)
		fullPath := path.Join(newApp.Path, sourceFS.HashName(staticPath))
		return fullPath
	}
	funcMap["fileNonEmpty"] = func(name string) bool {
		staticPath := path.Join("static", name)
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

	err = a.loadParamsInfo(a.sourceFS)
	if err != nil {
		return false, err
	}

	configData, err := a.sourceFS.ReadFile(apptype.CONFIG_LOCK_FILE_NAME)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}

		// Config lock is not present, use default config
		a.Debug().Msg("No config lock file found, using default config")
		a.Config = apptype.NewAppConfig()
		if a.IsDev {
			a.appDev.Config = a.Config
			a.appDev.SaveConfigLockFile()
		}
	} else {
		// Config lock file is present, read defaults from that
		a.Debug().Msg("Config lock file found, using config from lock file")
		a.Config = apptype.NewCompatibleAppConfig()
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

	// Parse HTML templates if there are HTML routes
	if a.usesHtmlTemplate {
		baseFiles, err := a.sourceFS.Glob(path.Join(a.Config.Routing.BaseTemplates, "*.go.html"))
		if err != nil {
			return false, err
		}

		if len(baseFiles) == 0 {
			// No base templates found, use the default unstructured templates
			if a.template, err = a.sourceFS.ParseFS(a.funcMap, a.Config.Routing.TemplateLocations...); err != nil {
				return false, err
			}
		} else {
			// Base templates found, using structured templates
			base, err := a.sourceFS.ParseFS(a.funcMap, baseFiles...)
			if err != nil {
				return false, err
			}

			a.templateMap = make(map[string]*template.Template)
			for _, paths := range a.Config.Routing.TemplateLocations {
				files, err := a.sourceFS.Glob(paths)
				if err != nil {
					return false, err
				}

				for _, file := range files {
					tmpl, err := base.Clone()
					if err != nil {
						return false, err
					}

					a.templateMap[file], err = tmpl.ParseFS(a.sourceFS.ReadableFS, file)
					if err != nil {
						return false, err
					}
				}
			}
		}
	}
	a.initialized = true

	if a.IsDev {
		a.notifyClients()
	}
	return true, nil
}

const (
	CONTAINERFILE = "Containerfile"
	DOCKERFILE    = "Dockerfile"
)

func (a *App) loadContainerManager() error {
	var fileName string
	var cfErr, dfErr error

	_, cfErr = a.sourceFS.Stat(CONTAINERFILE)
	if cfErr == nil {
		fileName = CONTAINERFILE
	} else {
		_, dfErr = a.sourceFS.Stat(DOCKERFILE)
		fileName = DOCKERFILE
	}

	if cfErr != nil && dfErr != nil {
		// No container files found, skip
		return nil
	}

	containerConfig, err := a.appDef.Attr("container")
	if err != nil || containerConfig == nil {
		// Plugin not authorized, skip any container files
		a.Warn().Msg("Container config not defined, skipping container initialization")
		return nil
	}

	if a.systemConfig.ContainerCommand == "" {
		return fmt.Errorf("app requires container support. Container management is not enabled in Clace server config. " +
			"Install Docker/Podman and set the container_command in system config or set to auto (default) and ensure that " +
			"the container manager command is in the PATH")
	}

	var ok bool
	var responseAttr starlark.HasAttrs
	if responseAttr, ok = containerConfig.(starlark.HasAttrs); !ok {
		return fmt.Errorf("container config is not valid type")
	}

	errorValue, err := responseAttr.Attr("error")
	if err != nil {
		return fmt.Errorf("error in container config: %w", err)
	}

	if errorValue != nil && errorValue != starlark.None {
		var errorString starlark.String
		if errorString, ok = errorValue.(starlark.String); !ok {
			return fmt.Errorf("error in container config: %w", err)
		}

		if errorString.GoString() != "" {
			return fmt.Errorf("error in container config: %s", errorString.GoString())
		}
	}

	config, err := responseAttr.Attr("value")
	if err != nil {
		return err
	}

	if config.Type() != "ContainerConfig" {
		return fmt.Errorf("container config is not valid type: expected ContainerConfig, got %s", config.Type())
	}

	var configAttr starlark.HasAttrs
	if configAttr, ok = config.(starlark.HasAttrs); !ok {
		return fmt.Errorf("container config is not valid type")
	}

	port, err := apptype.GetIntAttr(configAttr, "Port")
	if err != nil {
		return fmt.Errorf("error reading port: %w", err)
	}
	lifetime, err := apptype.GetStringAttr(configAttr, "Lifetime")
	if err != nil {
		return fmt.Errorf("error reading lifetime: %w", err)
	}

	scheme, err := apptype.GetStringAttr(configAttr, "Scheme")
	if err != nil {
		return fmt.Errorf("error reading scheme: %w", err)
	}

	health, err := apptype.GetStringAttr(configAttr, "Health")
	if err != nil {
		return fmt.Errorf("error reading health: %w", err)
	}

	a.containerManager, err = container.NewContainerManager(a.Logger, a.AppEntry, fileName, a.systemConfig, port, lifetime, scheme, health, a.sourceFS)
	if err != nil {
		return fmt.Errorf("error creating container manager: %w", err)
	}
	return nil
}

func (a *App) executeTemplate(w io.Writer, template, partial string, data any) error {
	var err error
	if a.template != nil {
		exec := partial
		if partial == "" {
			exec = template
		}
		if err = a.template.ExecuteTemplate(w, exec, data); err != nil {
			return err
		}
	} else {
		if template == "" {
			if _, ok := a.templateMap[partial]; ok {
				template = partial
			} else {
				template = "index.go.html"
			}
		}

		t, ok := a.templateMap[template]
		if !ok {
			return fmt.Errorf("template %s not found", template)
		}
		exec := partial
		if partial == "" {
			exec = template
		}
		if err = t.ExecuteTemplate(w, exec, data); err != nil {
			return err
		}
	}
	return err
}

func (a *App) loadSchemaInfo(sourceFS *appfs.SourceFs) error {
	// Load the schema info
	schemaInfoData, err := sourceFS.ReadFile(apptype.SCHEMA_FILE_NAME)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil // Ignore absence of schema file
	}

	a.storeInfo, err = apptype.ReadStoreInfo(apptype.SCHEMA_FILE_NAME, schemaInfoData)
	if err != nil {
		return fmt.Errorf("error reading schema info: %w", err)
	}

	return nil

}

func (a *App) loadParamsInfo(sourceFS *appfs.SourceFs) error {
	// Load the params info
	paramsInfoData, err := sourceFS.ReadFile(apptype.PARAMS_FILE_NAME)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil // Ignore absence of params file
	}

	a.paramInfo, err = apptype.ReadParamInfo(apptype.PARAMS_FILE_NAME, paramsInfoData)
	if err != nil {
		return fmt.Errorf("error reading params info: %w", err)
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

		inReload := atomic.Bool{}
		reloadEndTime := atomic.Int64{}
		inReload.Store(false)
		reloadEndTime.Store(0)

		for {
			select {
			case event, ok := <-a.watcher.Events:
				if !ok {
					return
				}

				if inReload.Load() {
					// If a reload is in progress, ignore the event
					a.Trace().Str("event", fmt.Sprint(event)).Msg("Ignoring event since reload is in progress")
					continue
				}
				endTime := reloadEndTime.Load()
				diff := time.Now().UnixMilli() - endTime
				a.Trace().Int64("diff", diff).Msg("Time since last reload")
				if endTime > 0 && (time.Now().UnixMilli()-endTime) < int64(a.systemConfig.FileWatcherDebounceMillis)*5 {
					// If a reload has happened recently, ignore the event
					a.Trace().Str("event", fmt.Sprint(event)).Msg("Ignoring event since reload happened recently")
					continue
				}

				a.Trace().Str("event", fmt.Sprint(event)).Msg("Received event")

				go func() {
					defer func() {
						if r := recover(); r != nil {
							a.Error().Msgf("Recovered from panic in watcher: %s", r)
						}
					}()

					inReload.Store(true)
					defer inReload.Store(false)
					_, err := a.Reload(true, false)
					if err != nil {
						a.Error().Err(err).Msg("Error reloading app")
					}
					a.reloadError = err
					a.Trace().Msg("Reloaded app after file changes")
					reloadEndTime.Store(time.Now().UnixMilli())
				}()
			case err, ok := <-a.watcher.Errors:
				a.Error().Err(err).Msgf("Error in watcher error receiver")
				if !ok {
					return
				}
			}
		}
	}()

	// Add watcher path.
	filepath.WalkDir(a.SourceUrl, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			a.Trace().Str("path", path).Msg("Adding path to watcher")
			return a.watcher.Add(path)
		}
		return nil
	})

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
