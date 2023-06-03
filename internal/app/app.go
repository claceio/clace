// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/claceio/clace/internal/utils"
	"github.com/fsnotify/fsnotify"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	APP_FILE_NAME         = "app.star"
	APP_CONFIG_KEY        = "app"
	DEFAULT_HANDLER       = "handler"
	METHODS_DELIMITER     = ","
	CONFIG_LOCK_FILE_NAME = "config.lock"
	PLUGIN_SUFFIX         = "plugin"
	INDEX_FILE            = "index.go.html"
	INDEX_GEN_FILE        = "index_gen.go.html"
	CLACE_GEN_FILE        = "clace_gen.go.html"
)

//go:embed index_gen.go.html clace_gen.go.html
var embedHtml embed.FS
var indexEmbed, claceGenEmbed []byte

func init() {
	var err error
	if indexEmbed, err = embedHtml.ReadFile(INDEX_GEN_FILE); err != nil {
		panic(err)
	}
	if claceGenEmbed, err = embedHtml.ReadFile(CLACE_GEN_FILE); err != nil {
		panic(err)
	}
}

type App struct {
	*utils.Logger
	*utils.AppEntry
	Name         string
	customLayout bool
	Config       *AppConfig
	fs           AppFS
	mu           sync.Mutex
	initialized  bool
	reloadError  error
	globals      starlark.StringDict
	appDef       *starlarkstruct.Struct
	appRouter    *chi.Mux
	template     *template.Template
	watcher      *fsnotify.Watcher
	sseListeners []chan SSEMessage
}

type SSEMessage struct {
	event string
	data  string
}

func NewApp(fs AppFS, logger *utils.Logger, app *utils.AppEntry) *App {
	return &App{
		fs:       fs,
		Logger:   logger,
		AppEntry: app,
	}
}

func (a *App) Initialize() error {
	var reloaded bool
	var err error
	if reloaded, err = a.reload(false); err != nil {
		return err
	}

	if reloaded && (a.IsDev || a.AutoSync || a.AutoReload) {
		if err := a.startWatcher(); err != nil {
			a.Info().Msgf("error starting watcher: %s", err)
			return err
		}
	}
	return nil
}

func (a *App) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.watcher != nil {
		if err := a.watcher.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) reload(force bool) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized && !force {
		return false, nil
	}
	var err error
	a.Info().Msg("Reloading app definition")

	configData, err := a.fs.ReadFile(CONFIG_LOCK_FILE_NAME)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, fs.ErrNotExist) && !os.IsNotExist(err) {
			return false, err
		}

		// Config lock is not present, use default config
		a.Debug().Msg("No config lock file found, using default config")
		a.Config = NewAppConfig()
		a.saveLockFile()
	} else {
		// Config lock file is present, read defaults from that
		a.Debug().Msg("Config lock file found, using config from lock file")
		a.Config = NewCompatibleAppConfig()
		json.Unmarshal(configData, a.Config)
	}

	// Load Starlark config, AppConfig is updated with the settings contents
	if err = a.loadStarlarkConfig(); err != nil {
		return false, err
	}

	// Create the generated HTML
	if err = a.generateHTML(); err != nil {
		return false, err
	}

	// Parse HTML templates
	if a.template, err = a.fs.ParseFS(a.Config.Routing.TemplateLocations...); err != nil {
		return false, err
	}
	a.initialized = true

	if a.IsDev || a.AutoReload {
		a.notifyClients()
	}
	return true, nil
}

func (a *App) generateHTML() error {
	// The header name of contents have changed, recreate it. Since reload creates the header
	// file and updating the file causes the FS watcher to call reload, we have to make sure the
	// file is updated only if there is an actual content change
	indexData, err := a.fs.ReadFile(INDEX_GEN_FILE)
	if err != nil || !bytes.Equal(indexData, indexEmbed) {
		if err := a.fs.Write(INDEX_GEN_FILE, indexEmbed); err != nil {
			return err
		}
	}

	claceGenData, err := a.fs.ReadFile(CLACE_GEN_FILE)
	if err != nil || !bytes.Equal(claceGenData, claceGenEmbed) {
		if err := a.fs.Write(CLACE_GEN_FILE, claceGenEmbed); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) saveLockFile() error {
	buf, err := json.MarshalIndent(a.Config, "", "  ")
	if err != nil {
		return err
	}
	err = a.fs.Write(CONFIG_LOCK_FILE_NAME, buf)
	return err
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
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.watcher != nil {
		a.watcher.Close()
	}

	var err error
	a.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Start listening for events.
	a.Trace().Msg("Start waiting for file changes")
	go func() {
		for {
			select {
			case event, ok := <-a.watcher.Events:
				if !ok {
					return
				}
				a.Trace().Str("event", fmt.Sprint(event)).Msg("Received event")
				_, err := a.reload(true)
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
	path := a.FsPath
	if path == "" {
		path = a.SourceUrl
	}

	err = a.watcher.Add(path)
	if err != nil {
		return err
	}

	return nil
}

func (a *App) addSSEClient(newChan chan SSEMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sseListeners = append(a.sseListeners, newChan)
}

func (a *App) removeSSEClient(chanRemove chan SSEMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
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
