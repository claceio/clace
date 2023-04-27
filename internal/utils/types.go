// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package utils

import "time"

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	Http     HttpConfig     `toml:"http"`
	Metadata MetadataConfig `toml:"metadata"`
	Log      LogConfig      `toml:"logging"`
}

// HttpConfig is the configuration for the HTTP server
type HttpConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

// MetadataConfig is the configuration for the Metadata persistence layer
type MetadataConfig struct {
	DBConnection string `toml:"db_connection"`
	AutoUpgrade  bool   `toml:"auto_upgrade"`
}

// LogConfig is the configuration for the Logger
type LogConfig struct {
	Level          string `toml:"level"`
	MaxBackups     int    `toml:"max_backups"`
	MaxSizeMB      int    `toml:"max_size_mb"`
	ConsoleLogging bool   `toml:"console_logging"`
	FileLogging    bool   `toml:"file_logging"`
}

// App is the application configuration
type App struct {
	*Logger
	Path       string
	Domain     string
	CodeUrl    string
	UserID     string
	CreateTime *time.Time
	UpdateTime *time.Time
	Rules      string
	Metadata   string
}
