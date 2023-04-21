// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"embed"

	"github.com/BurntSushi/toml"
)

//go:embed "clace.default.toml"
var f embed.FS

// init reads the embedded toml file and creates a default ServerConfig
func NewServerConfig() *ServerConfig {
	b, err := f.ReadFile("clace.default.toml")
	if err != nil {
		panic(err)
	}

	var config ServerConfig
	_, err = toml.Decode(string(b), &config)
	if err != nil {
		panic(err)
	}

	return &config
}
