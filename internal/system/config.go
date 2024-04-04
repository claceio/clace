// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"bytes"
	"embed"

	"github.com/BurntSushi/toml"
	"github.com/claceio/clace/internal/utils"
)

const DEFAULT_CONFIG = "clace.default.toml"

//go:embed "clace.default.toml"
var f embed.FS

func getEmbeddedToml() (string, error) {
	file, err := f.Open(DEFAULT_CONFIG)
	if err != nil {
		return "", err
	}

	defer file.Close()
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(file)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// NewServerConfigEmbedded reads the embedded toml file and creates a ServerConfig
func NewServerConfigEmbedded() (*utils.ServerConfig, error) {
	contents, err := getEmbeddedToml()
	if err != nil {
		return nil, err
	}

	var config utils.ServerConfig
	err = LoadServerConfig(contents, &config)
	return &config, err
}

// LoadServerConfig loads a ServerConfig from the given contents
func LoadServerConfig(contents string, config *utils.ServerConfig) error {
	_, err := toml.Decode(contents, &config)
	return err
}

// NewClientConfigEmbedded reads the embedded toml file and creates a ClientConfig
func NewClientConfigEmbedded() (*utils.ClientConfig, error) {
	contents, err := getEmbeddedToml()
	if err != nil {
		return nil, err
	}

	var config utils.ClientConfig
	err = LoadClientConfig(contents, &config)
	return &config, err
}

// LoadClientConfig load a ClientConfig from the given contents
func LoadClientConfig(contents string, config *utils.ClientConfig) error {
	_, err := toml.Decode(contents, &config)
	return err
}

// LoadGlobalConfig load a GlobalConfig from the given contents
func LoadGlobalConfig(contents string, config *utils.GlobalConfig) error {
	_, err := toml.Decode(contents, &config)
	return err
}

func GetDefaultConfigs() (*utils.GlobalConfig, *utils.ClientConfig, *utils.ServerConfig, error) {
	contents, err := getEmbeddedToml()
	if err != nil {
		return nil, nil, nil, err
	}

	var globalConfig utils.GlobalConfig
	var clientConfig utils.ClientConfig
	var serverConfig utils.ServerConfig
	if _, err := toml.Decode(contents, &globalConfig); err != nil {
		return nil, nil, nil, err
	}
	if _, err := toml.Decode(contents, &clientConfig); err != nil {
		return nil, nil, nil, err
	}
	if _, err := toml.Decode(contents, &serverConfig); err != nil {
		return nil, nil, nil, err
	}

	return &globalConfig, &clientConfig, &serverConfig, nil
}
