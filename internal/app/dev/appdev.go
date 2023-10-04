// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package dev

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"slices"

	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
)

//go:embed index_gen.go.html clace_gen.go.html
var embedHtml embed.FS
var indexEmbed, claceGenEmbed []byte

func init() {
	var err error
	if indexEmbed, err = embedHtml.ReadFile(util.INDEX_GEN_FILE); err != nil {
		panic(err)
	}
	if claceGenEmbed, err = embedHtml.ReadFile(util.CLACE_GEN_FILE); err != nil {
		panic(err)
	}
}

// AppDev is the main object that represents a Clace app in dev mode. It is created when the app is loaded with is_dev true
// and handles the styling and js library related functionalities. Access to this is synced through the initMutex in App.
// The reload method in App is the main access point to this object
type AppDev struct {
	*utils.Logger

	CustomLayout bool
	Config       *util.AppConfig
	systemConfig *utils.SystemConfig
	sourceFS     *util.AppFS
	workFS       *util.AppFS
	AppStyle     *AppStyle

	filesDownloaded map[string][]string
	JsLibs          []JSLibrary
	jsCache         map[JSLibrary]string
}

func NewAppDev(logger *utils.Logger, sourceFS, workFS *util.AppFS, systemConfig *utils.SystemConfig) *AppDev {
	dev := &AppDev{
		Logger:          logger,
		sourceFS:        sourceFS,
		workFS:          workFS,
		systemConfig:    systemConfig,
		AppStyle:        &AppStyle{},
		filesDownloaded: make(map[string][]string),
		jsCache:         make(map[JSLibrary]string),
	}
	return dev
}

// downloadFile downloads the files from the url, unless it was already loaded for this app in the current
// server session.
func (a *AppDev) downloadFile(url string, appFS *util.AppFS, path string) error {
	var ok bool
	var alreadyDone []string
	if alreadyDone, ok = a.filesDownloaded[url]; ok {
		if slices.Contains(alreadyDone, path) {
			a.Trace().Msgf("File %s:%s already downloaded", url, path)
			return nil
		}

		a.Trace().Msgf("File %s downloaded to different path", url)
	}

	a.Info().Msgf("Downloading %s into %s", url, path)

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	if _, err = io.Copy(&buf, resp.Body); err != nil {
		return err
	}
	if err = appFS.Write(path, buf.Bytes()); err != nil {
		return err
	}
	alreadyDone = append(alreadyDone, path)
	a.filesDownloaded[url] = alreadyDone
	return nil
}

// SetupJsLibs sets up the js libraries for the app.
func (a *AppDev) SetupJsLibs() error {
	if a.JsLibs != nil {
		for _, jsLib := range a.JsLibs {
			if _, ok := a.jsCache[jsLib]; ok {
				a.Trace().Msgf("JsLib %s already setup, skipping", jsLib)
				continue
			}

			targetFile, err := jsLib.Setup(a, a.sourceFS, a.workFS)
			if err != nil {
				if targetFile == "" {
					// Setup failed and cannot check if file exists, error out
					return err
				}
				_, err2 := a.sourceFS.Stat(targetFile)
				if err2 != nil {
					// Setup failed and file does not exist, error out with original error
					return err
				}
				a.Warn().Err(err).Msgf("Error setting up %s, using existing file", targetFile)
			}
			// Cache that this lib is setup
			a.jsCache[jsLib] = targetFile
		}
	}

	for lib, target := range a.jsCache {
		if target != "" && (a.JsLibs == nil || !slices.Contains(a.JsLibs[:], lib)) {
			// This lib is in the cache, but not in current list of libs. Remove it
			// from the disk.
			a.Trace().Msgf("Removing js lib %s", target)
			if err := a.sourceFS.Remove(target); err != nil {
				a.Warn().Msgf("Error removing js lib %s : %s", target, err)
			}
			delete(a.jsCache, lib)
		}
	}

	return nil
}

// GenerateHTML generates the default HTML template files for the app.
func (a *AppDev) GenerateHTML() error {
	// The header name of contents have changed, recreate it. Since reload creates the header
	// file and updating the file causes the FS watcher to call reload, we have to make sure the
	// file is updated only if there is an actual content change
	if !a.CustomLayout {
		indexData, err := a.sourceFS.ReadFile(util.INDEX_GEN_FILE)
		if err != nil || !bytes.Equal(indexData, indexEmbed) {
			if err := a.sourceFS.Write(util.INDEX_GEN_FILE, indexEmbed); err != nil {
				return err
			}
		}
	} else {
		a.sourceFS.Remove(util.INDEX_GEN_FILE)
	}

	claceGenData, err := a.sourceFS.ReadFile(util.CLACE_GEN_FILE)
	if err != nil || !bytes.Equal(claceGenData, claceGenEmbed) {
		if err := a.sourceFS.Write(util.CLACE_GEN_FILE, claceGenEmbed); err != nil {
			return err
		}
	}

	return nil
}

func (a *AppDev) SaveConfigLockFile() error {
	buf, err := json.MarshalIndent(a.Config, "", "  ")
	if err != nil {
		return err
	}
	err = a.sourceFS.Write(util.CONFIG_LOCK_FILE_NAME, buf)
	return err
}

// Close the app dev session
func (a *AppDev) Close() error {
	if a.AppStyle != nil {
		if err := a.AppStyle.StopWatcher(); err != nil {
			a.Warn().Err(err).Msg("Error stopping watcher")
		}
	}
	return nil
}
