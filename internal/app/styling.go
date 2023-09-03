// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/claceio/clace/internal/utils"
)

type LibraryType string

const (
	STYLE_FILE_PATH = "static/gen/css/style.css"
)

const (
	TailwindCSS LibraryType = "tailwindcss"
	DaisyUI     LibraryType = "daisyui"
	Other       LibraryType = "other"
	None        LibraryType = ""
)

type AppStyle struct {
	appId      utils.AppId
	library    LibraryType
	libraryUrl string
	//builder    exec.Cmd
}

func NewAppStyle(appId utils.AppId, stylingConfig StylingConfig) (*AppStyle, error) {
	appStyling := AppStyle{appId: appId, library: None}
	libType := strings.ToLower(stylingConfig.Library)

	if libType == string(None) {
		appStyling.library = None
	} else if libType == string(TailwindCSS) {
		appStyling.library = TailwindCSS
	} else if libType == string(DaisyUI) {
		appStyling.library = DaisyUI
	} else {
		appStyling.library = Other
		if strings.HasPrefix(libType, "http://") || strings.HasPrefix(libType, "https://") {
			appStyling.libraryUrl = libType
		} else {
			return nil, fmt.Errorf("invalid styling library config : %s", libType)
		}
	}
	return &appStyling, nil
}

func (s *AppStyle) Setup(sourceFS *AppFS, workFS *AppFS) error {
	switch s.library {
	case None:
		// Empty out the style.css file
		return sourceFS.Write(STYLE_FILE_PATH, []byte(""))
	case Other:
		// Download style.css from url
		return DownloadFile(s.libraryUrl, sourceFS, STYLE_FILE_PATH)
	case TailwindCSS:
		fallthrough
	case DaisyUI:
		// Generate the tailwind/daisyui config files
		return s.setupTailwindConfig(workFS)
	default:
		return fmt.Errorf("invalid styling library type : %s", s.library)
	}
}

const (
	TAILWIND_CONFIG_FILE     = "tailwind.config.js"
	TAILWIND_CONFIG_CONTENTS = `
	MODULE.EXPORTS = {
		CONTENT: ['*.HTML'],
		THEME: {
		  EXTEND: {},
		},
	  
		PLUGINS: [
		  REQUIRE('@TAILWINDCSS/FORMS'),
		  REQUIRE('@TAILWINDCSS/TYPOGRAPHY')
		  %s
		],
	}`

	TAILWIND_INPUT_CONTENTS = `
	@tailwind base;
	@tailwind components;
	@tailwind utilities;
	`
)

func (s *AppStyle) setupTailwindConfig(workFS *AppFS) error {
	configPath := fmt.Sprintf("style/%s", TAILWIND_CONFIG_FILE)
	inputPath := fmt.Sprintf("style/%s", "input.css")

	daisyPlugin := ""
	if s.library == DaisyUI {
		daisyPlugin = `, REQUIRE("daisyui")`
	}
	configContents := fmt.Sprintf(TAILWIND_CONFIG_CONTENTS, daisyPlugin)
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
