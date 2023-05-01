// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"embed"
	"sync"

	"github.com/claceio/clace/internal/utils"
)

type App struct {
	*utils.Logger
	*utils.AppEntry
	fs          embed.FS
	mu          sync.Mutex
	initialized bool
}

func NewApp(logger *utils.Logger, app *utils.AppEntry) *App {
	return &App{
		Logger:   logger,
		AppEntry: app,
	}
}

func (a *App) Intilialize() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized {
		return nil
	}

	/*
		dirFs := os.DirFS(a.Path)
		var ok bool
		a.fs, ok = dirFs.(embed.FS)
		if !ok {
			return os.ErrNotExist
		}
		a.initialized = true
	*/
	return nil
}
