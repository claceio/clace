// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"sync"

	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
)

type App struct {
	*utils.Logger
	*utils.AppEntry
	fs          fs.FS
	mu          sync.Mutex
	initialized bool
	globals     starlark.StringDict
}

func NewApp(logger *utils.Logger, app *utils.AppEntry) *App {
	return &App{
		Logger:   logger,
		AppEntry: app,
	}
}

func (a *App) Initialize() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.initialized {
		return nil
	}

	a.fs = os.DirFS(a.CodeUrl)
	err := a.load()
	if err != nil {
		return err
	}
	a.initialized = true
	return nil
}

func (a *App) load() error {
	a.Info().Str("path", a.Path).Str("domain", a.Domain).Msg("Loading app")

	file, err := a.fs.Open("clace.star")
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, file)
	if err != nil {
		return err
	}

	// The Thread defines the behavior of the built-in 'print' function.
	thread := &starlark.Thread{
		Name:  "app",
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
	}

	// This dictionary defines the pre-declared environment.
	predeclared := starlark.StringDict{
		"greeting": starlark.String("hello"),
	}

	// Execute a program.
	a.globals, err = starlark.ExecFile(thread, "clace.star", buf, predeclared)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			log.Fatal(evalErr.Backtrace())
		}
		log.Fatal(err)
	}
	return nil
}

func (a *App) PrintGlobals() {
	fmt.Println("\nGlobals:")
	for _, name := range a.globals.Keys() {
		v := a.globals[name]
		fmt.Printf("%s (%s) = %s\n", name, v.Type(), v.String())
	}
}
