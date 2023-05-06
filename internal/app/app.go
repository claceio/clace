// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
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
	APP_FILE              = "app.star"
	APP_KEY               = "app"
	DEFAULT_HANDLER       = "handler"
	METHODS_DELIMITER     = ","
	TEMPLATE_FILE_PATTERN = "*.go.html"
)

type AppFS interface {
	Open(dir string) (fs.File, error)
	ReadFile(name string) ([]byte, error)
	ParseFS(patterns ...string) (*template.Template, error)
}

type AppFSImpl struct {
	dir fs.FS
}

var _ AppFS = (*AppFSImpl)(nil)

func (f *AppFSImpl) Open(dir string) (fs.File, error) {
	f.dir = os.DirFS(dir)
	return nil, nil
}

func (f *AppFSImpl) ReadFile(name string) ([]byte, error) {
	file, err := f.dir.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, file)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (f *AppFSImpl) ParseFS(patterns ...string) (*template.Template, error) {
	return template.ParseFS(f.dir, patterns...)
}

type App struct {
	*utils.Logger
	*utils.AppEntry
	fs          AppFS
	mu          sync.Mutex
	initialized bool
	globals     starlark.StringDict
	appDef      *starlarkstruct.Struct
	appRouter   *chi.Mux
	template    *template.Template
	watcher     *fsnotify.Watcher
}

func NewApp(logger *utils.Logger, app *utils.AppEntry) *App {
	return &App{
		Logger:   logger,
		AppEntry: app,
	}
}

func (a *App) Initialize(fs AppFS) error {
	a.fs = fs
	if err := a.reload(false); err != nil {
		return err
	}

	if a.FsRefresh && a.FsPath != "" {
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

func (a *App) reload(force bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized && !force {
		return nil
	}
	var err error
	a.Info().Msg("Reloading app definition")

	// Parse templates
	a.template, err = a.fs.ParseFS(TEMPLATE_FILE_PATTERN)
	if err != nil {
		return err
	}

	err = a.loadStarlark()
	if err != nil {
		return err
	}
	a.initialized = true
	return nil
}

func (a *App) PrintGlobals() {
	fmt.Println("\nGlobals:")
	for _, name := range a.globals.Keys() {
		v := a.globals[name]
		fmt.Printf("%s (%s) = %s\n", name, v.Type(), v.String())
	}
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
	go func() {
		for {
			a.Trace().Msg("Waiting for file changes")
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
