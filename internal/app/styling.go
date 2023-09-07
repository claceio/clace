// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"syscall"

	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	STYLE_FILE_PATH = "static/gen/css/style.css"
)

// LibraryType is the type of style library used by the app
type LibraryType string

const (
	TailwindCSS LibraryType = "tailwindcss"
	DaisyUI     LibraryType = "daisyui"
	Other       LibraryType = "other"
	None        LibraryType = ""
)

// AppStyle is the style related configuration and state for an app. It is created
// when the App is loaded. It keeps track of the watcher process required to rebuild the
// CSS file when the tailwind/daisy config changes. The reload mutex lock in App is used to
// ensure only one call to the watcher is done at a time, no locking is implemented in AppStyle
type AppStyle struct {
	appId          utils.AppId
	library        LibraryType
	libraryUrl     string
	disableWatcher bool
	watcher        *exec.Cmd
	watcherState   *WatcherState
	watcherStdout  *os.File
}

// WatcherState is the state of the watcher process as of when it was started the last time.
type WatcherState struct {
	library           LibraryType
	templateLocations []string
}

// Init initializes the AppStyle object from the app definition
func (s *AppStyle) Init(appId utils.AppId, appDef *starlarkstruct.Struct) error {
	var ok bool
	var err error

	s.appId = appId

	var styleAttr starlark.Value
	if styleAttr, err = appDef.Attr("style"); err != nil {
		// No style defined
		s.library = None
		s.libraryUrl = ""
		s.disableWatcher = true
		return nil
	}

	var styleDef *starlarkstruct.Struct
	if styleDef, ok = styleAttr.(*starlarkstruct.Struct); !ok {
		return fmt.Errorf("style attr is not a struct")
	}
	var library string
	var disableWatcher bool
	if library, err = getStringAttr(styleDef, "library"); err != nil {
		return err
	}
	if disableWatcher, err = getBoolAttr(styleDef, "disable_watcher"); err != nil {
		return err
	}
	s.disableWatcher = disableWatcher

	libType := strings.ToLower(library)
	switch libType {
	case string(None):
		s.library = None
		s.libraryUrl = ""
	case string(TailwindCSS):
		s.library = TailwindCSS
		s.libraryUrl = ""
	case string(DaisyUI):
		s.library = DaisyUI
		s.libraryUrl = ""
	default:
		if strings.HasPrefix(libType, "http://") || strings.HasPrefix(libType, "https://") {
			s.libraryUrl = libType
			s.library = Other
		} else {
			return fmt.Errorf("invalid style library config : %s", libType)
		}
	}

	return nil
}

// Setup sets up the style library for the app. This is called when the app is reloaded.
func (s *AppStyle) Setup(templateLocations []string, sourceFS, workFS *AppFS) error {
	switch s.library {
	case None:
		// Empty out the style.css file
		return sourceFS.Write(STYLE_FILE_PATH, []byte(""))
	case TailwindCSS:
		fallthrough
	case DaisyUI:
		// Generate the tailwind/daisyui config files
		return s.setupTailwindConfig(templateLocations, sourceFS, workFS)
	case Other:
		// Download style.css from url
		return DownloadFile(s.libraryUrl, sourceFS, STYLE_FILE_PATH)
	default:
		return fmt.Errorf("invalid style library type : %s", s.library)
	}
}

const (
	// TODO: CONTENT needs to be configurable based on TemplateLocations
	TAILWIND_CONFIG_FILE     = "tailwind.config.js"
	TAILWIND_CONFIG_CONTENTS = `
	module.exports = {
		content: [%s],
		theme: {
		  extend: {},
		},
	  
		plugins: [
		  %s
		],
	}`

	TAILWIND_INPUT_CONTENTS = `
	@tailwind base;
	@tailwind components;
	@tailwind utilities;
	`
)

func (s *AppStyle) setupTailwindConfig(templateLocations []string, sourceFS, workFS *AppFS) error {
	configPath := fmt.Sprintf("style/%s", TAILWIND_CONFIG_FILE)
	inputPath := fmt.Sprintf("style/%s", "input.css")

	daisyPlugin := ""
	if s.library == DaisyUI {
		daisyPlugin = `require("daisyui")`
	}

	var buf strings.Builder
	for i, loc := range templateLocations {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(fmt.Sprintf("'%s'", path.Join(sourceFS.root, loc)))
	}

	configContents := fmt.Sprintf(TAILWIND_CONFIG_CONTENTS, buf.String(), daisyPlugin)
	if err := workFS.Write(configPath, []byte(configContents)); err != nil {
		return fmt.Errorf("error writing tailwind config file : %s", err)
	}
	if err := workFS.Write(inputPath, []byte(TAILWIND_INPUT_CONTENTS)); err != nil {
		return fmt.Errorf("error writing tailwind input file : %s", err)
	}

	return nil
}

func DownloadFile(url string, appFS *AppFS, path string) error {
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
	return nil
}

// StartWatcher starts the watcher process for the app. This is called when the app is reloaded.
func (s *AppStyle) StartWatcher(templateLocations []string, sourceFS, workFS *AppFS, systemConfig *utils.SystemConfig) error {
	switch s.library {
	case None:
		fallthrough
	case Other:
		// If config is being switched from tailwind/daisy to other/none, stop any current watcher
		return s.StopWatcher()
	case TailwindCSS:
		fallthrough
	case DaisyUI:
		if s.disableWatcher {
			return s.StopWatcher()
		}
		return s.startTailwindWatcher(templateLocations, sourceFS, workFS, systemConfig)
	default:
		return fmt.Errorf("invalid style library type : %s", s.library)
	}
}

func (s *AppStyle) startTailwindWatcher(templateLocations []string, sourceFS, workFS *AppFS, systemConfig *utils.SystemConfig) error {
	tailwindCmd := strings.TrimSpace(systemConfig.TailwindCSSCommand)
	if tailwindCmd == "" {
		fmt.Println("Warning: tailwindcss command not configured. Skipping tailwindcss watcher") // TODO: log
		return nil
	}

	if s.watcher != nil {
		if s.watcherState != nil && s.watcherState.library == s.library && slices.Equal(s.watcherState.templateLocations, templateLocations) {
			fmt.Println("Warning: tailwindcss watcher already running with current config. Skipping tailwindcss watcher") // TODO: log
			return nil
		}
		fmt.Printf("Warning: tailwindcss watcher already running with older config. Stopping previous watcher %#v %#v %#v", s.watcherState, s.library, templateLocations) // TODO: log
		if err := s.StopWatcher(); err != nil {
			return err
		}
	}
	s.watcherState = &WatcherState{library: s.library, templateLocations: templateLocations}

	split := strings.Split(tailwindCmd, " ")
	args := []string{}
	if len(split) > 1 {
		args = split[1:]
	}

	// Since the watcher process creates the file, the unit test framework (in memory filesystem)
	// can't be used to test the watcher functionality)
	targetFile := path.Join(sourceFS.root, STYLE_FILE_PATH)
	targetDir := path.Dir(targetFile)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("error creating directory %s : %s", targetDir, err)
	}
	args = append(args, "--watch")
	args = append(args, "-c", path.Join(workFS.root, "style", TAILWIND_CONFIG_FILE))
	args = append(args, "-i", path.Join(workFS.root, "style", "input.css"))
	args = append(args, "-o", targetFile)
	fmt.Printf("Running command %s args %#v", split[0], args) // TODO: log

	// Setup stdin/stdout for watcher process
	if s.watcherStdout != nil {
		s.watcherStdout.Close()
	}
	var err error
	s.watcherStdout, err = os.Create(path.Join(workFS.root, "tailwindcss.log"))
	if err != nil {
		return fmt.Errorf("error creating tailwindcss log file : %s", err)
	}

	// Start watcher process, wait async for it to complete
	s.watcher = exec.Command(split[0], args...)
	s.watcher.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // ensure process group

	s.watcher.Stdin = os.Stdin // this seems to be required for the process to start
	s.watcher.Stdout = s.watcherStdout
	s.watcher.Stderr = s.watcherStdout
	if err := s.watcher.Start(); err != nil {
		return fmt.Errorf("error starting tailwind watcher : %s", err)
	}
	go func() {
		if err := s.watcher.Wait(); err != nil {
			fmt.Printf("error waiting for tailwind watcher : %s\n", err) // TODO: log
		}
	}()

	return nil
}

func (s *AppStyle) StopWatcher() error {
	if s.watcher != nil && s.watcher.Process != nil {
		fmt.Println("Stopping watcher")
		if err := syscall.Kill(-s.watcher.Process.Pid, syscall.SIGKILL); err != nil {
			fmt.Printf("error killing previous watcher process : %s\n", err)
		}
		s.watcher = nil
	}
	return nil
}
