// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sync"

	"github.com/claceio/clace/internal/utils"
	"github.com/fsnotify/fsnotify"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	APP_FILE_NAME         = "app.star"
	APP_CONFIG_KEY        = "config"
	DEFAULT_HANDLER       = "handler"
	METHODS_DELIMITER     = ","
	CONFIG_LOCK_FILE_NAME = "config.lock"
)

type App struct {
	*utils.Logger
	*utils.AppEntry
	name, layout string
	config       *AppConfig
	fs           AppFS
	mu           sync.Mutex
	initialized  bool
	globals      starlark.StringDict
	appDef       *starlarkstruct.Struct
	appRouter    *chi.Mux
	template     *template.Template
	watcher      *fsnotify.Watcher
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

	if reloaded && a.FsRefresh && a.FsPath != "" {
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
		if err != os.ErrNotExist {
			return false, err
		}

		// Config lock is not present, use default config
		a.Debug().Msg("No config lock file found, using default config")
		a.config = NewAppConfig()
		a.saveLockFile()
	} else {
		// Config lock file is present, read defaults from that
		a.Debug().Msg("Config lock file found, using config from lock file")
		a.config = NewCompatibleAppConfig()
		json.Unmarshal(configData, a.config)
	}

	// Load Starlark config, AppConfig is updated with the settings contents
	err = a.loadStarlark()
	if err != nil {
		return false, err
	}

	// Parse HTML templates
	a.template, err = a.fs.ParseFS(a.config.Routing.TemplateLocations...)
	if err != nil {
		return false, err
	}
	a.initialized = true
	return true, nil
}

func (a *App) saveLockFile() error {
	var jsonBuf bytes.Buffer
	if err := json.NewEncoder(&jsonBuf).Encode(a.config); err != nil {
		return err
	}
	err := a.fs.Write(CONFIG_LOCK_FILE_NAME, jsonBuf.Bytes())
	return err
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.Info().Str("method", r.Method).Str("url", r.URL.String()).Msg("App Received request")
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
				if event.Has(fsnotify.Write) {
					a.reload(true)
				}
			case err, ok := <-a.watcher.Errors:
				a.Error().Err(err).Msgf("Error in watcher error receiver")
				if !ok {
					return
				}
			}
		}
	}()

	// Add watcher path.
	err = a.watcher.Add(a.FsPath)
	if err != nil {
		return err
	}

	return nil
}
